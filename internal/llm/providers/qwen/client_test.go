package qwen

import (
	"strings"
	"testing"
)

func TestPreferModel(t *testing.T) {
	list := []string{"qwen-max", "qwen-turbo"}
	if got := PreferModel(list, "qwen-max"); got != "qwen-max" {
		t.Fatalf("keep current: got %q", got)
	}
	if got := PreferModel(list, "missing"); got != "qwen-plus" && got != list[0] {
		t.Fatalf("prefer default: got %q", got)
	}
	if got := PreferModel(nil, ""); got != DefaultModel {
		t.Fatalf("empty: got %q", got)
	}
}

func TestOrderAndMerge(t *testing.T) {
	merged := MergeCurated([]string{"text-embedding-v3", "qwen-max"})
	ordered := OrderModels(merged)
	if len(ordered) < len(CuratedModels) {
		t.Fatalf("expected curated present, got %d", len(ordered))
	}
	if ordered[0] != CuratedModels[0] {
		t.Fatalf("first should be curated[0]=%q got %q", CuratedModels[0], ordered[0])
	}
}

func TestFilterModels(t *testing.T) {
	items := []ModelInfo{
		{ID: "text-embedding-v3"},
		{ID: "qwen3-coder-plus"},
		{ID: "wanx-v1"},
		{ID: "qwen-max-latest"},
		{ID: "qwen-audio-turbo"},
	}
	out := FilterModels(items, 10)
	joined := strings.Join(out, ",")
	if !strings.Contains(joined, "qwen-max-latest") {
		t.Fatal("expected qwen-max-latest")
	}
	if strings.Contains(joined, "embedding") {
		t.Fatal("embeddings should be filtered out")
	}
	if strings.Contains(joined, "audio") {
		t.Fatal("audio models should be filtered out")
	}
	if strings.Contains(joined, "wanx-v1") {
		t.Fatal("non-qwen models should be filtered out")
	}
}

func TestBaseURLFor(t *testing.T) {
	if got := BaseURLFor("cn"); got != CnBaseURL {
		t.Fatalf("cn: got %q", got)
	}
	if got := BaseURLFor("intl"); got != IntlBaseURL {
		t.Fatalf("intl: got %q", got)
	}
	if got := BaseURLFor(""); got != IntlBaseURL {
		t.Fatalf("default: got %q", got)
	}
}

func TestMapAPIError(t *testing.T) {
	err := mapAPIError(401, []byte(`{"error":{"message":"Invalid API key"}}`))
	if err == nil || !strings.Contains(err.Error(), "ключ") {
		t.Fatalf("401: got %v", err)
	}
	err = mapAPIError(429, []byte(`{"error":{"message":"Requests rate limit exceeded"}}`))
	if err == nil || !strings.Contains(err.Error(), "лимит") {
		t.Fatalf("429: got %v", err)
	}
}
