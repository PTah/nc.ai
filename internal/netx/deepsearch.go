// Created by nc.ai 0.9.0 on new-admin-pc 2026-10-28 10:26:22
package netx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"notcursor.ai/app/internal/redact"
)

// DeepSeek in-house Search & Fetch API (opened 2026-10-17). Same bearer key as
// the LLM endpoint, billed per successful request ($0.75 / 1000), QPS 100/25.
// It gives what the DuckDuckGo scrape cannot: rerank score, authority tier,
// operator syntax and a model-ready summary, plus server-side Markdown fetch.
const (
	// DeepSeekAPIBase is the public API root; search/fetch live under it.
	DeepSeekAPIBase = "https://api.deepseek.com"
	deepSearchPath  = "/v1/tools/search"
	deepFetchPath   = "/v1/tools/fetch"

	// deepSearchTimeout / deepFetchTimeout keep a dead endpoint from eating a step.
	deepSearchTimeout = 25 * time.Second
	deepFetchTimeout  = 40 * time.Second

	// deepMaxCount is the API ceiling for one search.
	deepMaxCount = 50
	// deepMaxSummary / deepMaxContent trim what goes into the prompt. The API
	// documents summary as 600–800 chars; the cap keeps eight hits under ~7 KB.
	deepMaxSummary = 800
	deepMaxContent = 120 * 1024
	// deepMaxErrorCap keeps an upstream error message readable in a tool result.
	deepMaxErrorCap = 300
)

// DeepHit is one web result returned by the Search API.
type DeepHit struct {
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Snippet   string  `json:"snippet"`
	Summary   string  `json:"summary"`
	Content   string  `json:"content"`
	SiteName  string  `json:"site_name"`
	Date      string  `json:"date"`
	Rerank    float64 `json:"rerank_score"`
	Authority int     `json:"authority"`
}

// DeepSearchResult is the whole Search API response.
type DeepSearchResult struct {
	Results []DeepHit `json:"results"`
}

// DeepPage is the Fetch API response: page content as Markdown.
type DeepPage struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// DeepAPIError is a non-2xx answer. Status lets the caller decide whether to
// fall back (402 no balance, 403 blocked, 404 no content, 429 QPS, 5xx).
type DeepAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *DeepAPIError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code != "" && !strings.Contains(msg, e.Code) {
		msg = e.Code + ": " + msg
	}
	return fmt.Sprintf("deepseek %d: %s", e.Status, msg)
}

// DeepSearchOptions tunes one search request.
type DeepSearchOptions struct {
	// Count is the number of results, 1..50 (0 = 8).
	Count int
	// NeedContent also returns page content in DeepHit.Content.
	NeedContent bool
	// Original is the user's question behind the query; improves understanding.
	Original string
}

// DeepSearch runs the in-house search and returns structured results.
func DeepSearch(ctx context.Context, apiKey, query string, opt DeepSearchOptions) (DeepSearchResult, error) {
	return deepSearchWithBase(ctx, DeepSeekAPIBase, apiKey, query, opt)
}

func deepSearchWithBase(ctx context.Context, base, apiKey, query string, opt DeepSearchOptions) (DeepSearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return DeepSearchResult{}, fmt.Errorf("query is empty")
	}
	count := opt.Count
	if count <= 0 {
		count = 8
	}
	if count > deepMaxCount {
		count = deepMaxCount
	}
	body := map[string]any{
		"query": q,
		"count": count,
	}
	if opt.NeedContent {
		body["need_content"] = true
	}
	if orig := strings.TrimSpace(opt.Original); orig != "" {
		body["ori_question"] = orig
	}
	var out DeepSearchResult
	if err := deepPost(ctx, base, apiKey, deepSearchPath, body, &out, deepSearchTimeout); err != nil {
		return DeepSearchResult{}, err
	}
	return out, nil
}

// DeepFetch retrieves one page and returns its title and Markdown content.
// The URL still goes through the SSRF check: a server-side fetch must not let
// the agent touch private networks through DeepSeek either.
func DeepFetch(ctx context.Context, apiKey, rawURL string) (DeepPage, error) {
	return deepFetchWithBase(ctx, DeepSeekAPIBase, apiKey, rawURL)
}

func deepFetchWithBase(ctx context.Context, base, apiKey, rawURL string) (DeepPage, error) {
	u, err := CheckURL(rawURL)
	if err != nil {
		return DeepPage{}, err
	}
	body := map[string]any{"url": u.String()}
	var page DeepPage
	if err := deepPost(ctx, base, apiKey, deepFetchPath, body, &page, deepFetchTimeout); err != nil {
		return DeepPage{}, err
	}
	if strings.TrimSpace(page.Content) == "" {
		return page, &DeepAPIError{Status: http.StatusNotFound, Code: "empty_content", Message: "fetch returned no readable content"}
	}
	page.Content = trimText(unescapeMarkdown(page.Content), deepMaxContent)
	return page, nil
}

// deepEscapeRe matches a backslash before any non-alphanumeric character — the
// Fetch API escapes Markdown punctuation ("\#", "\:", "\/"), which makes the
// page noisy for the model, while real sequences like "\n" stay intact.
var deepEscapeRe = regexp.MustCompile(`\\([^A-Za-z0-9\s])`)

// unescapeMarkdown removes DeepSeek's punctuation escaping, keeping real
// sequences like \n or a literal "\\" intact.
func unescapeMarkdown(s string) string {
	return deepEscapeRe.ReplaceAllString(s, "$1")
}

// deepPost sends one authenticated JSON request and decodes the answer.
func deepPost(ctx context.Context, base, apiKey, path string, body any, out any, timeout time.Duration) error {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return fmt.Errorf("deepseek web tools: API key is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NotCursor.ai (desktop agent)")

	resp, err := SafeClient(timeout).Do(req)
	if err != nil {
		return fmt.Errorf("deepseek %s: %w", strings.TrimPrefix(path, "/v1/tools/"), err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseDeepError(resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response (http %d): %w", resp.StatusCode, err)
	}
	return nil
}

// parseDeepError turns an OpenAI-compatible error body into *DeepAPIError.
func parseDeepError(status int, data []byte) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	e := &DeepAPIError{Status: status}
	if json.Unmarshal(data, &envelope) == nil {
		e.Message = strings.TrimSpace(envelope.Error.Message)
		e.Code = strings.TrimSpace(envelope.Error.Code)
		if e.Code == "" {
			e.Code = strings.TrimSpace(envelope.Error.Type)
		}
	}
	if e.Message == "" {
		e.Message = trimText(strings.Join(strings.Fields(string(data)), " "), deepMaxErrorCap)
	}
	e.Message = trimText(e.Message, deepMaxErrorCap)
	return e
}

// FormatDeepSearch renders results for the tool result: title, URL, provenance
// and the model-ready summary (snippet as fallback).
func FormatDeepSearch(query string, res DeepSearchResult) string {
	if len(res.Results) == 0 {
		return redact.String(fmt.Sprintf("web_search %q (deepseek): no results", strings.TrimSpace(query)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "web_search %q (%d hits · deepseek)\n", strings.TrimSpace(query), len(res.Results))
	for i, h := range res.Results {
		title := oneLine(h.Title)
		if title == "" {
			title = "(no title)"
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, title)
		if u := strings.TrimSpace(h.URL); u != "" {
			fmt.Fprintf(&b, "   %s\n", u)
		}
		var meta []string
		if s := oneLine(h.SiteName); s != "" {
			meta = append(meta, s)
		}
		if d := dateOnly(h.Date); d != "" {
			meta = append(meta, d)
		}
		if h.Rerank > 0 {
			meta = append(meta, fmt.Sprintf("score %.1f", h.Rerank))
		}
		if h.Authority >= 3 {
			meta = append(meta, "authoritative")
		}
		if len(meta) > 0 {
			fmt.Fprintf(&b, "   %s\n", strings.Join(meta, " · "))
		}
		text := oneLine(h.Summary)
		if text == "" {
			text = oneLine(h.Snippet)
		}
		if text = trimText(text, deepMaxSummary); text != "" {
			fmt.Fprintf(&b, "   %s\n", text)
		}
	}
	return redact.String(strings.TrimRight(b.String(), "\n"))
}

// FormatDeepPage renders a fetched page for the tool result.
func FormatDeepPage(rawURL string, page DeepPage) string {
	head := "fetch " + strings.TrimSpace(rawURL)
	if title := oneLine(page.Title); title != "" {
		head += " — " + title
	}
	body := strings.TrimSpace(page.Content)
	return redact.String(head + "\n" + body)
}

// dateOnly keeps the calendar date of an RFC3339 timestamp.
func dateOnly(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return ""
}

// oneLine collapses whitespace in a title or summary.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// trimText cuts a string to limit bytes on a rune boundary.
func trimText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// isRuneStart reports whether b starts a UTF-8 rune.
func isRuneStart(b byte) bool {
	return b&0xC0 != 0x80
}
