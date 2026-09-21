package providernews

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseDeepSeekUpdates(t *testing.T) {
	body := []byte(`<html><body>
	<h2>Date: 2026-09-10</h2>
	<h3>DeepSeek-V4.1-Flash Release</h3>
	<p>Today, we officially release the DeepSeek-V4.1-Flash model.</p>
	<h2>Date: 2026-08-21</h2>
	<h3>DeepSeek-V4-Flash-Vision-Exp Release</h3>
	</body></html>`)
	items := parseDeepSeekUpdates(body)
	if len(items) != 2 {
		t.Fatalf("ожидалось 2 записи, получено %d: %+v", len(items), items)
	}
	if items[0].Date != "2026-09-10" || items[0].Title != "DeepSeek-V4.1-Flash Release" {
		t.Errorf("первая запись: %+v", items[0])
	}
	if items[0].Provider != "DeepSeek" || items[0].URL == "" || items[0].ID == "" {
		t.Errorf("нет обязательных полей: %+v", items[0])
	}
}

func TestParseZaiReleaseNotes(t *testing.T) {
	body := []byte(`<html><body>
	<h3>2026-08-26</h3><p>GLM-5.3-Flash</p><p>Native visual capabilities…</p>
	<h3>2026-08-18</h3><p>GLM-5.3</p>
	<p>Страница содержит и другие даты: 2025-07-15 CogVideoX-3</p>
	</body></html>`)
	items := parseZaiReleaseNotes(body)
	if len(items) < 2 {
		t.Fatalf("ожидалось не меньше 2 записей, получено %d: %+v", len(items), items)
	}
	if items[0].Date != "2026-08-26" || items[0].Title != "GLM-5.3-Flash" {
		t.Errorf("первая запись: %+v", items[0])
	}
	if items[1].Title != "GLM-5.3" {
		t.Errorf("вторая запись: %+v", items[1])
	}
}

func TestParseZaiSameLineTitle(t *testing.T) {
	body := []byte(`<html><body><p>2026-06-16 GLM-5.2</p></body></html>`)
	items := parseZaiReleaseNotes(body)
	if len(items) != 1 || items[0].Title != "GLM-5.2" {
		t.Fatalf("не разобрана однострочная запись: %+v", items)
	}
}

func TestParseOpenRouterBlog(t *testing.T) {
	body := []byte(`<html><body>
	<article><h2>Build a Reliable Tool-Calling Agent Loop on OpenRouter</h2>
	<time>September 17, 2026</time></article>
	<article><h2>Does DeepSeek V4 Have Vision?</h2>
	<time>September 16, 2026</time></article>
	</body></html>`)
	items := parseOpenRouterBlog(body)
	if len(items) != 2 {
		t.Fatalf("ожидалось 2 записи, получено %d: %+v", len(items), items)
	}
	if items[0].Title == "" || items[0].Date != "2026-09-17" {
		t.Errorf("первая запись: %+v", items[0])
	}
}

func TestParseQwenRSS(t *testing.T) {
	body := []byte(`<?xml version="1.0"?><rss version="2.0"><channel>
	<item><title>Qwen3Guard: Real-time Safety</title>
	<link>https://qwenlm.github.io/blog/qwen3guard/</link>
	<pubDate>Tue, 23 Sep 2025 04:00:00 +0800</pubDate></item>
	<item><title>Qwen-Image-Edit</title>
	<link>https://qwenlm.github.io/blog/qwen-image-edit/</link>
	<pubDate>Tue, 19 Aug 2025 01:30:00 +0800</pubDate></item>
	</channel></rss>`)
	items := parseQwenRSS(body)
	if len(items) != 2 {
		t.Fatalf("ожидалось 2 записи, получено %d", len(items))
	}
	if items[0].Date != "2025-09-23" || items[0].URL == "" {
		t.Errorf("первая запись: %+v", items[0])
	}
}

func TestParseQwenRSSToleratesBrokenXML(t *testing.T) {
	if items := parseQwenRSS([]byte("<not xml")); items != nil {
		t.Errorf("ожидался nil на мусоре, получено %+v", items)
	}
}

func TestDedupeSortKeepsNewestAndDropsDuplicates(t *testing.T) {
	items := []Item{
		{ID: "a", Provider: "Qwen", Title: "old", Date: "2025-01-01"},
		{ID: "a", Provider: "Qwen", Title: "old", Date: "2025-01-01"},
		{ID: "b", Provider: "DeepSeek", Title: "new", Date: "2026-09-10"},
		{ID: "", Provider: "X", Title: "no id"},
	}
	out := dedupeSort(items)
	if len(out) != 2 {
		t.Fatalf("ожидалось 2 записи, получено %d: %+v", len(out), out)
	}
	if out[0].ID != "b" {
		t.Errorf("самая свежая запись должна быть первой: %+v", out)
	}
}

func TestDedupeSortCapsItems(t *testing.T) {
	var items []Item
	for i := 0; i < MaxItems+10; i++ {
		items = append(items, Item{ID: string(rune('a'+i%26)) + string(rune('0'+i/26)), Title: "t", Date: "2026-01-01"})
	}
	if got := len(dedupeSort(items)); got > MaxItems {
		t.Errorf("лимит не соблюдён: %d", got)
	}
}

func TestPayloadOfCountsNew(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	prev := Snapshot{UpdatedAt: now.Add(-time.Minute), PrevIDs: []string{"old"}}
	prev.Items = []Item{{ID: "old"}, {ID: "fresh"}}
	p := PayloadOf(prev, now)
	if p.NewCount != 1 {
		t.Errorf("новых должно быть 1: %+v", p)
	}
	if p.Stale {
		t.Errorf("свежий снимок не должен считаться устаревшим")
	}
	if !PayloadOf(Snapshot{}, now).Stale {
		t.Errorf("пустой снимок должен считаться устаревшим")
	}
	if !PayloadOf(Snapshot{UpdatedAt: now.Add(-25 * time.Hour)}, now).Stale {
		t.Errorf("снимок старше суток должен считаться устаревшим")
	}
}

func TestHTMLToTextStripsNoise(t *testing.T) {
	body := []byte("<style>p{color:red}</style><script>x()</script><h2>Дата</h2>\u200b<p>Текст&nbsp;и&amp;ещё</p>")
	text := htmlToText(body)
	if text == "" || contains(text, "color:red") || contains(text, "x()") {
		t.Fatalf("мусор не вычищен: %q", text)
	}
	if !contains(text, "Текст и&ещё") {
		t.Errorf("не раскрыты entity: %q", text)
	}
}

func TestCatalogDiff(t *testing.T) {
	prev := []string{"deepseek-flash", "deepseek-v4-pro"}
	cur := []string{"deepseek-flash", "deepseek-v4-pro", "deepseek-v5"}
	items := catalogDiff("DeepSeek", "https://example.test/pricing", prev, cur, "2026-09-20")
	if len(items) != 1 {
		t.Fatalf("ожидалось одно событие, получено %+v", items)
	}
	if items[0].Title != "В списке моделей появилась deepseek-v5" || items[0].Date != "2026-09-20" {
		t.Errorf("событие о появлении: %+v", items[0])
	}

	gone := catalogDiff("DeepSeek", "u", []string{"deepseek-flash", "deepseek-v4-pro"}, []string{"deepseek-flash"}, "2026-09-20")
	if len(gone) != 1 || gone[0].Title != "Ушла из списка моделей: deepseek-v4-pro" {
		t.Fatalf("событие об уходе: %+v", gone)
	}

	if got := catalogDiff("DeepSeek", "u", nil, cur, "2026-09-20"); got != nil {
		t.Errorf("первый снимок должен молчать, получено %+v", got)
	}
	if got := catalogDiff("DeepSeek", "u", prev, prev, "2026-09-20"); got != nil {
		t.Errorf("без изменений событий быть не должно: %+v", got)
	}
}

func TestParseDeepSeekCatalog(t *testing.T) {
	body := []byte(`<table><tr><td>MODEL</td><td>deepseek-flash (1)</td><td>deepseek-v4-pro</td>
	<td>BASE URL</td><td>https://api.deepseek.com</td></tr>
	<tr><td>PRICING</td><td>$0.003</td></tr></table>
	<p>(1) legacy names deepseek-v4-flash and deepseek-v4-flash-vision-exp are still accepted</p>`)
	got := parseDeepSeekCatalog(body)
	if len(got) != 2 || got[0] != "deepseek-flash" || got[1] != "deepseek-v4-pro" {
		t.Fatalf("каталог DeepSeek разобран неверно: %+v (текст: %q)", got, htmlToText(body))
	}
}

func TestParseOpenRouterCatalogKeepsOurFamilies(t *testing.T) {
	body := []byte(`{"data":[{"id":"deepseek/deepseek-v4-pro"},{"id":"z-ai/glm-5.3"},
	{"id":"qwen/qwen3-coder"},{"id":"openai/gpt-5"},{"id":"anthropic/claude-opus"}]}`)
	got := parseOpenRouterCatalog(body)
	if len(got) != 3 {
		t.Fatalf("ожидались только наши семейства: %+v", got)
	}
	if got[0] != "deepseek/deepseek-v4-pro" || got[2] != "z-ai/glm-5.3" {
		t.Errorf("порядок/состав каталога: %+v", got)
	}
	if got := parseOpenRouterCatalog([]byte("<html>")); got != nil {
		t.Errorf("на не-JSON должен быть nil: %+v", got)
	}
}

func TestParseZaiCatalog(t *testing.T) {
	body := []byte("<p>glm-5.3-flash, glm-5.3 and GLM-4.7-Flash are priced here</p>")
	got := parseZaiCatalog(body)
	if len(got) != 3 || got[0] != "glm-4.7-flash" {
		t.Fatalf("каталог Z.AI разобран неверно: %+v", got)
	}
}

func TestLiveFeeds(t *testing.T) {
	if os.Getenv("NC_NEWS_LIVE") == "" {
		t.Skip("set NC_NEWS_LIVE=1 to hit the real provider pages")
	}
	ctx := context.Background()
	for _, src := range []struct {
		provider string
		url      string
		parse    func([]byte) []Item
	}{
		{"DeepSeek", deepSeekUpdatesURL, parseDeepSeekUpdates},
		{"Z.AI", zaiReleaseNotesURL, parseZaiReleaseNotes},
		{"OpenRouter", openRouterBlogURL, parseOpenRouterBlog},
		{"Qwen", qwenBlogRSSURL, parseQwenRSS},
	} {
		body, err := fetch(ctx, src.url)
		if err != nil {
			t.Errorf("%s: %v", src.provider, err)
			continue
		}
		items := dedupeSort(src.parse(body))
		if len(items) == 0 {
			t.Errorf("%s: 0 записей (страница изменилась?)", src.provider)
			continue
		}
		t.Logf("%s: %d записей, свежая: %s %s — %s", src.provider, len(items), items[0].Date, items[0].Provider, items[0].Title)
	}

	// Model catalogs: the thing that should have warned us about V4 Pro.
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
			t.Errorf("%s catalog: %v", cat.provider, err)
			continue
		}
		models := cat.parse(body)
		if len(models) == 0 {
			t.Errorf("%s catalog: пусто", cat.provider)
			continue
		}
		t.Logf("каталог %s: %d моделей, например %v", cat.provider, len(models), firstN(models, 4))
	}
}

func firstN(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
