// Created by nc.ai 0.9.0 on new-admin-pc 2026-10-28 10:28:44
package tools

import (
	"context"
	"strings"
	"testing"
)

func TestWebProviderSelection(t *testing.T) {
	cases := []struct {
		name    string
		setting string
		key     string
		want    string
	}{
		{"unset is duckduckgo even with a key", "", "sk-x", WebProviderDuckDuckGo},
		{"auto, key present", "auto", "sk-x", WebProviderDeepSeek},
		{"auto, no key", "auto", "", WebProviderDuckDuckGo},
		{"forced deepseek, no key", "deepseek", "", WebProviderDeepSeek},
		{"forced duckduckgo, key present", "duckduckgo", "sk-x", WebProviderDuckDuckGo},
		{"case and spaces", "  DuckDuckGo ", "sk-x", WebProviderDuckDuckGo},
		{"unknown value with key", "bing", "sk-x", WebProviderDuckDuckGo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Registry{WebTools: c.setting, DeepSeekKey: c.key}
			if got := r.webProvider(); got != c.want {
				t.Fatalf("provider = %q, want %q", got, c.want)
			}
		})
	}
}

// A forced DeepSeek mode without a key must explain the fix instead of issuing a
// doomed request (and instead of silently searching somewhere else).
func TestWebToolsWithoutKeyGiveAHint(t *testing.T) {
	r := &Registry{WebTools: WebProviderDeepSeek}

	_, err := r.webSearch(context.Background(), "ноутбук до 1000 долларов")
	if err == nil || !strings.Contains(err.Error(), "ключ DeepSeek") {
		t.Fatalf("web_search error = %v", err)
	}
	if !strings.Contains(err.Error(), "DuckDuckGo") {
		t.Fatalf("hint must mention the alternative: %v", err)
	}

	_, err = r.webFetch(context.Background(), "https://example.com/a")
	if err == nil || !strings.Contains(err.Error(), "ключ DeepSeek") {
		t.Fatalf("fetch_url error = %v", err)
	}
}

func TestWebToolsValidateArguments(t *testing.T) {
	r := &Registry{WebTools: WebProviderDuckDuckGo}
	if _, err := r.webSearch(context.Background(), "   "); err == nil {
		t.Fatal("empty query must fail")
	}
	if _, err := r.webFetch(context.Background(), "  "); err == nil {
		t.Fatal("empty url must fail")
	}
}
