package netx

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"notcursor.ai/app/internal/redact"
)

const maxBodyBytes = 200 * 1024

var (
	hrefRe    = regexp.MustCompile(`(?i)<a[^>]+class="[^"]*result__a[^"]*"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	snippetRe = regexp.MustCompile(`(?i)<a[^>]+class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
	tagRe     = regexp.MustCompile(`(?s)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<[^>]+>`)
	spaceRe   = regexp.MustCompile(`[ \t\r\n]{2,}`)
	uddgRe    = regexp.MustCompile(`(?i)[?&]uddg=([^&]+)`)
)

// SearchDuckDuckGo runs a web search and returns a compact snippet list.
func SearchDuckDuckGo(query string) (string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", fmt.Errorf("query is empty")
	}
	raw := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(q)
	body, ct, err := fetchBytes(raw)
	if err != nil {
		return "", err
	}
	_ = ct
	return formatSearch(q, body), nil
}

func formatSearch(q, body string) string {
	type hit struct {
		title, href, snippet string
	}
	var hits []hit
	locs := hrefRe.FindAllStringSubmatchIndex(body, 12)
	for _, loc := range locs {
		href := html.UnescapeString(body[loc[2]:loc[3]])
		title := stripTags(body[loc[4]:loc[5]])
		href = unwrapDDG(href)
		snippet := ""
		rest := body[loc[1]:]
		if m := snippetRe.FindStringSubmatch(rest); m != nil {
			snippet = stripTags(m[1])
		}
		if href == "" {
			continue
		}
		hits = append(hits, hit{title: title, href: href, snippet: snippet})
		if len(hits) >= 8 {
			break
		}
	}
	if len(hits) == 0 {
		text := stripTags(body)
		if len(text) > 1500 {
			text = text[:1500] + "…"
		}
		return redact.String(fmt.Sprintf("web_search %q: no structured results\n%s", q, text))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "web_search %q (%d hits)\n", q, len(hits))
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, empty(h.title, "(no title)"), h.href)
		if h.snippet != "" {
			fmt.Fprintf(&b, "   %s\n", h.snippet)
		}
	}
	return redact.String(strings.TrimRight(b.String(), "\n"))
}

func unwrapDDG(href string) string {
	href = strings.TrimSpace(href)
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	if m := uddgRe.FindStringSubmatch(href); m != nil {
		if dec, err := url.QueryUnescape(m[1]); err == nil && dec != "" {
			return dec
		}
	}
	return href
}

// FetchURL GET-s a public http(s) URL after an SSRF check. HTML is stripped to text.
func FetchURL(raw string) (string, error) {
	body, ct, err := fetchBytes(raw)
	if err != nil {
		return "", err
	}
	text := body
	head := text
	if len(head) > 200 {
		head = head[:200]
	}
	if strings.Contains(strings.ToLower(ct), "html") || strings.Contains(strings.ToLower(head), "<html") {
		text = stripTags(text)
	}
	if len([]byte(body)) > maxBodyBytes {
		text += "\n\n/* truncated */"
	}
	return redact.String(text), nil
}

func fetchBytes(raw string) (string, string, error) {
	u, err := CheckURL(raw)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "NotCursor.ai/0.6 (desktop agent)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml,text/plain;q=0.9,*/*;q=0.8")
	resp, err := SafeClient(0).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		if _, err := CheckURL(resp.Request.URL.String()); err != nil {
			return "", "", err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("http %d from %s", resp.StatusCode, u.Host)
	}
	limited := io.LimitReader(resp.Body, maxBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", "", err
	}
	if len(data) > maxBodyBytes {
		data = data[:maxBodyBytes]
	}
	return string(data), resp.Header.Get("Content-Type"), nil
}

func stripTags(s string) string {
	s = html.UnescapeString(tagRe.ReplaceAllString(s, " "))
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func empty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
