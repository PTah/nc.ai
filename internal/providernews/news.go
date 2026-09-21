// Package providernews keeps a small local digest of what changed at the
// providers we talk to (DeepSeek, Z.AI, OpenRouter, Qwen): new models, pricing
// and API changes. The pricing refreshers in internal/costing only update the
// numbers; this package answers "what happened there lately".
package providernews

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"notcursor.ai/app/internal/fsx"
)

// Item is one entry from a provider feed.
type Item struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Title    string `json:"title"`
	Date     string `json:"date,omitempty"` // YYYY-MM-DD when the source gives one
	URL      string `json:"url"`
}

// SourceError reports a feed that could not be read this time.
type SourceError struct {
	Provider string `json:"provider"`
	Message  string `json:"message"`
}

// Snapshot is the cached digest plus service fields.
type Snapshot struct {
	UpdatedAt time.Time     `json:"updatedAt"`
	ReadAt    time.Time     `json:"readAt,omitempty"`
	Items     []Item        `json:"items"`
	Errors    []SourceError `json:"errors,omitempty"`
	// ModelLists is the last seen model catalog per provider, used to notice a
	// model appearing or disappearing (that is how the V4 Pro retirement was
	// mis-announced earlier).
	ModelLists map[string][]string `json:"modelLists,omitempty"`
	// PrevIDs is the id list of the previous digest, used to count what is new.
	PrevIDs []string `json:"prevIds,omitempty"`
}

// Payload is what the UI receives.
type Payload struct {
	Items     []Item        `json:"items"`
	UpdatedAt string        `json:"updatedAt,omitempty"`
	NewCount  int           `json:"newCount"`
	Stale     bool          `json:"stale"`
	Errors    []SourceError `json:"errors,omitempty"`
}

// MaxItems caps the digest so the cache file and the UI stay small.
const MaxItems = 60

// StaleAfter is how long a digest stays "fresh" before the UI offers a refresh.
const StaleAfter = 24 * time.Hour

// FileName is the cache file inside the NotCursor config directory.
const FileName = "provider-news.json"

// Path returns the cache location (os.UserConfigDir()/NotCursor/provider-news.json).
func Path() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "NotCursor", FileName), nil
}

// Load reads the cached digest. A missing file is not an error.
func Load() (Snapshot, error) {
	p, err := Path()
	if err != nil {
		return Snapshot{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, nil
		}
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return Snapshot{}, nil // a broken cache is simply ignored
	}
	return s, nil
}

// Save writes the digest atomically.
func Save(s Snapshot) error {
	p, err := Path()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(p, append(raw, '\n'), 0o600)
}

// PayloadOf converts a snapshot for the UI, counting entries that were not in
// the previous digest.
func PayloadOf(s Snapshot, now time.Time) Payload {
	seen := make(map[string]bool, len(s.PrevIDs))
	for _, id := range s.PrevIDs {
		seen[id] = true
	}
	newCount := 0
	for _, it := range s.Items {
		if !seen[it.ID] {
			newCount++
		}
	}
	p := Payload{Items: s.Items, NewCount: newCount, Errors: s.Errors, Stale: true}
	if !s.UpdatedAt.IsZero() {
		p.UpdatedAt = s.UpdatedAt.Format(time.RFC3339)
		p.Stale = now.Sub(s.UpdatedAt) >= StaleAfter
	}
	return p
}

// MarkRead remembers that the user looked at the digest.
func MarkRead() error {
	s, err := Load()
	if err != nil {
		return err
	}
	s.ReadAt = time.Now()
	return Save(s)
}

// Refresh fetches every feed, merges the result with what we already had and
// stores the new digest. Feed failures do not fail the whole call: they are
// reported in Snapshot.Errors so the UI can show "Qwen: не отвечает".
func Refresh(ctx context.Context, prev Snapshot) (Snapshot, error) {
	type result struct {
		provider string
		items    []Item
		err      error
	}
	sources := []struct {
		provider string
		url      string
		parse    func(body []byte) []Item
	}{
		{"DeepSeek", deepSeekUpdatesURL, parseDeepSeekUpdates},
		{"Z.AI", zaiReleaseNotesURL, parseZaiReleaseNotes},
		{"OpenRouter", openRouterBlogURL, parseOpenRouterBlog},
		{"Qwen", qwenBlogRSSURL, parseQwenRSS},
	}

	results := make([]result, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, provider, url string, parse func([]byte) []Item) {
			defer wg.Done()
			body, err := fetch(ctx, url)
			if err != nil {
				results[i] = result{provider: provider, err: err}
				return
			}
			results[i] = result{provider: provider, items: parse(body)}
		}(i, src.provider, src.url, src.parse)
	}
	wg.Wait()

	snapshot := Snapshot{UpdatedAt: time.Now(), ReadAt: prev.ReadAt, ModelLists: map[string][]string{}}
	snapshot.PrevIDs = ids(prev.Items)
	for _, res := range results {
		if res.err != nil {
			snapshot.Errors = append(snapshot.Errors, SourceError{Provider: res.provider, Message: shortErr(res.err)})
			continue
		}
		snapshot.Items = append(snapshot.Items, res.items...)
	}

	// Model catalogs: a cheaper early warning than reading release notes.
	today := snapshot.UpdatedAt.Format("2006-01-02")
	for name, prevList := range prev.ModelLists {
		snapshot.ModelLists[name] = prevList
	}
	for _, cat := range []struct {
		provider string
		url      string
		parse    func([]byte) []string
	}{
		{"DeepSeek", deepSeekPricingURL, parseDeepSeekCatalog},
		{"Z.AI", zaiPricingURL, parseZaiCatalog},
		{"OpenRouter", openRouterModelsURL, parseOpenRouterCatalog},
	} {
		body, err := fetch(ctx, cat.url)
		if err != nil {
			continue // catalog problems must not spam the digest
		}
		cur := cat.parse(body)
		if len(cur) == 0 {
			continue
		}
		snapshot.Items = append(snapshot.Items, catalogDiff(cat.provider, cat.url, snapshot.ModelLists[cat.provider], cur, today)...)
		snapshot.ModelLists[cat.provider] = cur
	}
	// Keep previously known entries when a feed temporarily fails, so the list
	// does not blink empty in the UI.
	if len(snapshot.Errors) > 0 {
		have := make(map[string]bool, len(snapshot.Items))
		for _, it := range snapshot.Items {
			have[it.ID] = true
		}
		for _, it := range prev.Items {
			if !have[it.ID] {
				snapshot.Items = append(snapshot.Items, it)
			}
		}
	}
	snapshot.Items = dedupeSort(snapshot.Items)
	if err := Save(snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func ids(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// catalogDiff generates news entries for models that appeared in or disappeared
// from a provider catalog. The first snapshot is silent: it only records the
// baseline, otherwise the very first refresh would list every model as new.
func catalogDiff(provider, url string, prev, cur []string, today string) []Item {
	if len(prev) == 0 || len(cur) == 0 {
		return nil
	}
	before := make(map[string]bool, len(prev))
	for _, m := range prev {
		before[m] = true
	}
	now := make(map[string]bool, len(cur))
	for _, m := range cur {
		now[m] = true
	}
	var out []Item
	for _, m := range cur {
		if before[m] {
			continue
		}
		out = append(out, Item{
			ID:       "catalog|" + provider + "|added|" + m,
			Provider: provider,
			Title:    "В списке моделей появилась " + m,
			Date:     today,
			URL:      url,
		})
	}
	for _, m := range prev {
		if now[m] {
			continue
		}
		out = append(out, Item{
			ID:       "catalog|" + provider + "|removed|" + m,
			Provider: provider,
			Title:    "Ушла из списка моделей: " + m,
			Date:     today,
			URL:      url,
		})
	}
	return out
}

// ---- model catalogs -------------------------------------------------------

const (
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	zaiPricingURL       = "https://docs.z.ai/guides/overview/pricing.md"
)

// deepSeekModelRe pulls the model column out of the pricing table.
var (
	deepSeekTableRe = regexp.MustCompile(`(?s)MODEL\s+(.{0,800}?)(?:PRICING|CONTEXT LENGTH)`)
	deepSeekNameRe  = regexp.MustCompile(`\bdeepseek-[a-z0-9.\-]+\b`)
	// Retired names still accepted by the API: they are not the catalog.
	legacyDeepSeekNames = map[string]bool{
		"deepseek-v4-flash":            true,
		"deepseek-v4-flash-vision-exp": true,
		"deepseek-chat":                true,
		"deepseek-reasoner":            true,
	}
	glmModelRe = regexp.MustCompile(`\b(?:glm|autoglm|chatglm|cogvideo)[a-z]*-?\d[a-z0-9.\-]*`)
)

// parseDeepSeekCatalog lists the models DeepSeek currently sells. The pricing
// page names them in the table and repeats retired names in the footnote, so the
// retired ones are filtered out explicitly.
func parseDeepSeekCatalog(body []byte) []string {
	text := htmlToText(body)
	var out []string
	for _, name := range deepSeekNameRe.FindAllString(text, -1) {
		if legacyDeepSeekNames[name] {
			continue
		}
		out = append(out, name)
	}
	return uniqueSorted(out)
}

// parseZaiCatalog lists GLM-family models mentioned in docs.z.ai pricing.
func parseZaiCatalog(body []byte) []string {
	text := htmlToText(body)
	var out []string
	for _, name := range glmModelRe.FindAllString(strings.ToLower(text), -1) {
		out = append(out, name)
	}
	return uniqueSorted(out)
}

// parseOpenRouterCatalog keeps only the families we actually route to.
func parseOpenRouterCatalog(body []byte) []string {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	var out []string
	for _, m := range payload.Data {
		id := strings.ToLower(strings.TrimSpace(m.ID))
		for _, prefix := range []string{"deepseek/", "z-ai/", "qwen/"} {
			if strings.HasPrefix(id, prefix) {
				out = append(out, id)
				break
			}
		}
	}
	return uniqueSorted(out)
}

func uniqueSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func dedupeSort(items []Item) []Item {
	seen := make(map[string]bool, len(items))
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.ID == "" || seen[it.ID] {
			continue
		}
		// The same entry can arrive from two feeds with different titles: keep
		// the first, it comes from the provider with the more specific source.
		seen[it.ID] = true
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].Provider < out[j].Provider
	})
	if len(out) > MaxItems {
		out = out[:MaxItems]
	}
	return out
}

func shortErr(err error) string {
	msg := err.Error()
	if len(msg) > 160 {
		msg = msg[:160] + "…"
	}
	return msg
}

const (
	deepSeekUpdatesURL  = "https://api-docs.deepseek.com/updates"
	deepSeekPricingURL  = "https://api-docs.deepseek.com/quick_start/pricing"
	zaiReleaseNotesURL  = "https://docs.z.ai/release-notes"
	openRouterBlogURL   = "https://openrouter.ai/blog"
	qwenBlogRSSURL      = "https://qwenlm.github.io/blog/index.xml"
	userAgent           = "NotCursor/" + "news"
	requestTimeout      = 20 * time.Second
	maxBodyBytes        = 3 << 20
	titleLimit          = 160
	deepSeekDefaultDate = "Change Log"
)

func fetch(ctx context.Context, url string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("http " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
}

// ---- source parsers -------------------------------------------------------

var (
	scriptRe   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	noscriptRe = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`)
	tagRe      = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRe    = regexp.MustCompile(`[ \t\xa0]+`)
	blankRe    = regexp.MustCompile(`\n{2,}`)

	deepSeekDateRe = regexp.MustCompile(`(?m)^Date:\s*(\d{4}-\d{2}-\d{2})\s*$`)
	zaiDateRe      = regexp.MustCompile(`(?m)^(\d{4}-\d{2}-\d{2})(?:\s+(\S.{0,140}))?$`)
	monthYearRe    = regexp.MustCompile(`^(January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{1,2},\s+\d{4}$`)
	zeroWidthRe    = regexp.MustCompile(`[\x{200b}\x{200e}\x{200f}\x{feff}]`)
	// Z.AI entries are always a model name: GLM-*, AutoGLM-*, CogVideoX-*.
	zaiTitleRe = regexp.MustCompile(`(?i)^(glm|autoglm|chatglm|cogvideo)`)
	// OpenRouter's blog lists a title, an excerpt and then the date, so the text
	// pass picks the excerpt. Read the heading + date pair straight from markup.
	openRouterEntryRe = regexp.MustCompile(`(?is)<h[23][^>]*>\s*(?:<[^>]+>\s*)*([^<]{8,200}?)\s*(?:<[^>]+>\s*)*</h[23]>.{0,900}?((?:January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{1,2},\s+20\d\d)`)
)

// zaiStopTitles are page chrome and navigation labels that sit next to dates.
var zaiStopTitles = map[string]bool{
	"new released":            true,
	"new released - overview": true,
	"models":                  true,
	"overview":                true,
	"on this page":            true,
	"guides":                  true,
	"api reference":           true,
	"released notes":          true,
	"coding plan":             true,
	"terms and policy":        true,
	"help center":             true,
	"learn more":              true,
}

// htmlToText flattens markup into plain lines so the parsers can use anchors
// that survive a redesign better than tag-specific selectors.
func htmlToText(body []byte) string {
	s := scriptRe.ReplaceAllString(string(body), " ")
	s = styleRe.ReplaceAllString(s, " ")
	s = noscriptRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "<br", "\n<br")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "</h1>", "\n")
	s = strings.ReplaceAll(s, "</h2>", "\n")
	s = strings.ReplaceAll(s, "</h3>", "\n")
	s = strings.ReplaceAll(s, "</h4>", "\n")
	s = strings.ReplaceAll(s, "</li>", "\n")
	s = strings.ReplaceAll(s, "</div>", "\n")
	// Table cells keep values glued to labels otherwise: "MODELdeepseek-flash".
	s = strings.ReplaceAll(s, "</td>", "\n")
	s = strings.ReplaceAll(s, "</th>", "\n")
	s = strings.ReplaceAll(s, "</tr>", "\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = zeroWidthRe.ReplaceAllString(s, "")
	s = spaceRe.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(ln)
	}
	s = strings.Join(lines, "\n")
	s = blankRe.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}

func firstLine(s string, limit int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > limit {
		s = strings.TrimSpace(s[:limit]) + "…"
	}
	return s
}

func makeID(provider, url, title string) string {
	return provider + "|" + url + "|" + title
}

// parseDeepSeekUpdates reads the DeepSeek change log page: entries look like
// "Date: 2026-09-10" followed by "<Model> Release".
func parseDeepSeekUpdates(body []byte) []Item {
	text := htmlToText(body)
	lines := strings.Split(text, "\n")
	var out []Item
	for i, ln := range lines {
		m := deepSeekDateRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		title := ""
		for j := i + 1; j < len(lines) && j <= i+4; j++ {
			cand := strings.TrimSpace(lines[j])
			if cand == "" || cand == deepSeekDefaultDate || deepSeekDateRe.MatchString(cand) {
				continue
			}
			title = firstLine(cand, titleLimit)
			break
		}
		if title == "" {
			title = "Change Log " + m[1]
		}
		out = append(out, Item{
			ID:       makeID("DeepSeek", deepSeekUpdatesURL, title),
			Provider: "DeepSeek",
			Title:    title,
			Date:     m[1],
			URL:      deepSeekUpdatesURL,
		})
	}
	return out
}

// parseZaiReleaseNotes reads docs.z.ai/release-notes, where entries start with a
// "YYYY-MM-DD" line and the model name follows it (same line or the next one).
func parseZaiReleaseNotes(body []byte) []Item {
	text := htmlToText(body)
	lines := strings.Split(text, "\n")
	var out []Item
	seen := map[string]bool{}
	for i, ln := range lines {
		m := zaiDateRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		title := firstLine(m[2], titleLimit)
		if title == "" || zaiStopTitles[strings.ToLower(title)] {
			title = ""
			for j := i + 1; j < len(lines) && j <= i+3; j++ {
				cand := strings.TrimSpace(lines[j])
				if cand == "" || zaiDateRe.MatchString(cand) || zaiStopTitles[strings.ToLower(cand)] {
					continue
				}
				title = firstLine(cand, titleLimit)
				break
			}
		}
		if title == "" || !zaiTitleRe.MatchString(title) || seen[m[1]+title] {
			continue
		}
		seen[m[1]+title] = true
		out = append(out, Item{
			ID:       makeID("Z.AI", zaiReleaseNotesURL, title),
			Provider: "Z.AI",
			Title:    title,
			Date:     m[1],
			URL:      zaiReleaseNotesURL,
		})
	}
	return out
}

// parseOpenRouterBlog reads the blog index from markup: a heading followed by the
// publish date. Text flattening is useless here because the excerpt sits between
// the title and the date.
func parseOpenRouterBlog(body []byte) []Item {
	raw := scriptRe.ReplaceAllString(string(body), " ")
	matches := openRouterEntryRe.FindAllStringSubmatch(raw, 60)
	var out []Item
	seen := map[string]bool{}
	for _, m := range matches {
		title := firstLine(cleanText(m[1]), titleLimit)
		date := humanDateToISO(cleanText(m[2]))
		if title == "" || date == "" {
			continue
		}
		id := makeID("OpenRouter", openRouterBlogURL, title)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Item{
			ID:       id,
			Provider: "OpenRouter",
			Title:    title,
			Date:     date,
			URL:      openRouterBlogURL,
		})
	}
	return out
}

// cleanText removes leftover markup and collapses whitespace in a fragment.
func cleanText(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = zeroWidthRe.ReplaceAllString(s, "")
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// parseQwenRSS reads the Qwen blog RSS (Hugo feed, RSS 2.0).
func parseQwenRSS(body []byte) []Item {
	var feed struct {
		Channel struct {
			Items []struct {
				Title   string `xml:"title"`
				Link    string `xml:"link"`
				PubDate string `xml:"pubDate"`
				Date    string `xml:"date"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil
	}
	var out []Item
	for _, it := range feed.Channel.Items {
		title := firstLine(it.Title, titleLimit)
		link := strings.TrimSpace(it.Link)
		if title == "" || link == "" {
			continue
		}
		out = append(out, Item{
			ID:       makeID("Qwen", link, title),
			Provider: "Qwen",
			Title:    title,
			Date:     rssDate(it.PubDate, it.Date),
			URL:      link,
		})
	}
	return out
}

func rssDate(candidates ...string) string {
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
			if t, err := time.Parse(layout, c); err == nil {
				return t.Format("2006-01-02")
			}
		}
		if len(c) >= 10 {
			return c[:10]
		}
	}
	return ""
}

func humanDateToISO(s string) string {
	for _, layout := range []string{"January 2, 2006", "Jan 2, 2006"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}
