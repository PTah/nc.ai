// Edited by nc.ai 0.9.0 on new-admin-pc 2026-10-28 10:26:44
package netx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeepSearchParsesResults(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"DeepSeek V4 released","url":"https://example.com/v4","snippet":"V4 is out",
			 "summary":"DeepSeek released V4 with tool calling.","site_name":"Example News",
			 "date":"2026-06-01T00:00:00+00:00","rerank_score":2.4,"authority":3},
			{"title":"Second","url":"https://example.com/2","snippet":"two","rerank_score":0.4,"authority":1}
		]}`))
	}))
	defer srv.Close()

	res, err := deepSearchWithBase(context.Background(), srv.URL, "sk-test", "DeepSeek V4", DeepSearchOptions{Count: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotPath != "/v1/tools/search" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotBody, `"count":5`) || !strings.Contains(gotBody, `"query":"DeepSeek V4"`) {
		t.Fatalf("request body = %s", gotBody)
	}
	if len(res.Results) != 2 {
		t.Fatalf("results = %d", len(res.Results))
	}
	if res.Results[0].SiteName != "Example News" || res.Results[0].Rerank != 2.4 || res.Results[0].Authority != 3 {
		t.Fatalf("first hit parsed wrong: %+v", res.Results[0])
	}

	out := FormatDeepSearch("DeepSeek V4", res)
	for _, want := range []string{
		"2 hits · deepseek",
		"https://example.com/v4",
		"Example News · 2026-06-01 · score 2.4 · authoritative",
		"tool calling",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("format misses %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "authoritative") != 1 {
		t.Fatalf("authority marker must appear only for authority >= 3:\n%s", out)
	}
}

func TestDeepSearchEmptyAndErrors(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer empty.Close()

	res, err := deepSearchWithBase(context.Background(), empty.URL, "k", "nothing here", DeepSearchOptions{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if out := FormatDeepSearch("nothing here", res); !strings.Contains(out, "no results") {
		t.Fatalf("empty format = %q", out)
	}

	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"Insufficient Balance","type":"invalid_request_error","code":"402"}}`))
	}))
	defer denied.Close()

	_, err = deepSearchWithBase(context.Background(), denied.URL, "k", "q", DeepSearchOptions{})
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := err.(*DeepAPIError)
	if !ok {
		t.Fatalf("error type = %T (%v)", err, err)
	}
	if apiErr.Status != http.StatusPaymentRequired {
		t.Fatalf("status = %d", apiErr.Status)
	}
	if !strings.Contains(apiErr.Error(), "Insufficient Balance") {
		t.Fatalf("error text = %q", apiErr.Error())
	}
}

func TestDeepFetchReturnsMarkdown(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"title":"Example article","content":"# Example article\n\nBody text."}`))
	}))
	defer srv.Close()

	page, err := deepFetchWithBase(context.Background(), srv.URL, "k", "https://example.com/a")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if page.Title != "Example article" || !strings.HasPrefix(page.Content, "# Example article") {
		t.Fatalf("page = %+v", page)
	}
	if !strings.Contains(gotBody, `"url":"https://example.com/a"`) {
		t.Fatalf("request body = %s", gotBody)
	}
	if out := FormatDeepPage("https://example.com/a", page); !strings.Contains(out, "— Example article") {
		t.Fatalf("format = %q", out)
	}
}

func TestUnescapeMarkdown(t *testing.T) {
	in := `\#\# Date\: 2026\-10\-17` + "\n" + `price 1\.5 \\ end` + "\n" + `keep \n real newline escape`
	out := unescapeMarkdown(in)
	if !strings.HasPrefix(out, "## Date: 2026-10-17") {
		t.Fatalf("escapes not removed: %q", out)
	}
	if !strings.Contains(out, `price 1.5 \ end`) {
		t.Fatalf("double backslash must collapse to one: %q", out)
	}
	if !strings.Contains(out, `\n`) {
		t.Fatalf("\\n must stay untouched: %q", out)
	}
}

func TestDeepFetchEmptyContentAndSSRF(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"title":"","content":"   "}`))
	}))
	defer empty.Close()

	if _, err := deepFetchWithBase(context.Background(), empty.URL, "k", "https://example.com/a"); err == nil {
		t.Fatal("empty content must be an error so the caller can fall back")
	}

	// Private targets and non-http schemes are rejected before any request leaves the machine.
	if _, err := DeepFetch(context.Background(), "k", "http://192.168.1.1/"); err == nil {
		t.Fatal("SSRF check must reject private addresses")
	}
	if _, err := DeepFetch(context.Background(), "k", "file:///etc/passwd"); err == nil {
		t.Fatal("non-http schemes must be rejected")
	}
}

func TestDeepSearchRequiresKeyAndQuery(t *testing.T) {
	if _, err := DeepSearch(context.Background(), "", "q", DeepSearchOptions{}); err == nil {
		t.Fatal("empty key must fail")
	}
	if _, err := DeepSearch(context.Background(), "k", "   ", DeepSearchOptions{}); err == nil {
		t.Fatal("empty query must fail")
	}
}
