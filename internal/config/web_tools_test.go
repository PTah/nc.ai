// Created by nc.ai 0.9.0 on new-admin-pc 2026-10-28 10:28:50
package config

import "testing"

func TestNormalizeWebTools(t *testing.T) {
	cases := map[string]string{
		"":            "duckduckgo",
		"   ":         "duckduckgo",
		"auto":        "auto",
		"deepseek":    "deepseek",
		"DeepSeek":    "deepseek",
		" DDG ":       "duckduckgo",
		"duckduckgo":  "duckduckgo",
		"DuckDuckGo ": "duckduckgo",
		"bing":        "duckduckgo",
	}
	for in, want := range cases {
		if got := normalizeWebTools(in); got != want {
			t.Fatalf("normalizeWebTools(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetWebToolsProviderPersistsChoice(t *testing.T) {
	st := NewStoreForTest(t.TempDir())
	if got := st.WebToolsProviderName(); got != "duckduckgo" {
		t.Fatalf("default = %q, want duckduckgo", got)
	}
	if err := st.SetWebToolsProvider("deepseek"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := st.WebToolsProviderName(); got != "deepseek" {
		t.Fatalf("after set = %q, want deepseek", got)
	}
	if err := st.SetWebToolsProvider("nonsense"); err != nil {
		t.Fatalf("set unknown: %v", err)
	}
	if got := st.WebToolsProviderName(); got != "duckduckgo" {
		t.Fatalf("unknown value must normalize to duckduckgo, got %q", got)
	}
}
