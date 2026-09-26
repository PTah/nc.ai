package yandex

import (
	"strings"
	"testing"
)

func TestModelURI(t *testing.T) {
	got := ModelURI("b1gfolder", "yandexgpt")
	if got != "gpt://b1gfolder/yandexgpt/latest" {
		t.Fatalf("got %q", got)
	}
	full := "gpt://b1gfolder/yandexgpt-lite/latest"
	if ModelURI("other", full) != full {
		t.Fatalf("full URI must stay: %q", ModelURI("other", full))
	}
	if ModelURI("", "yandexgpt") != "yandexgpt" {
		t.Fatalf("no folder should keep short id")
	}
}

func TestShortModelID(t *testing.T) {
	if got := ShortModelID("gpt://b1g/yandexgpt/latest"); got != "yandexgpt" {
		t.Fatalf("got %q", got)
	}
	if got := ShortModelID("yandexgpt-lite"); got != "yandexgpt-lite" {
		t.Fatalf("got %q", got)
	}
}

func TestPreferModel(t *testing.T) {
	list := []string{"yandexgpt", "yandexgpt-lite"}
	if got := PreferModel(list, "yandexgpt-lite"); got != "yandexgpt-lite" {
		t.Fatalf("keep current: got %q", got)
	}
	if got := PreferModel(list, "missing"); got != DefaultModel {
		t.Fatalf("prefer default: got %q", got)
	}
	if got := PreferModel(nil, ""); got != DefaultModel {
		t.Fatalf("empty: got %q", got)
	}
}

func TestOrderAndMerge(t *testing.T) {
	merged := MergeCurated([]string{"emb://x", "gpt://f/yandexgpt/latest"})
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
		{ID: "emb://folder/text-search-doc/latest"},
		{ID: "gpt://folder/yandexgpt/latest"},
		{ID: "gpt://folder/gpt-oss-120b/latest"},
	}
	out := FilterModels(items, 10)
	joined := strings.Join(out, ",")
	if !strings.Contains(joined, "yandexgpt") {
		t.Fatal("expected yandexgpt")
	}
	if strings.Contains(joined, "emb://") || strings.Contains(joined, "text-search") {
		t.Fatal("embeddings should be filtered out")
	}
}

func TestMapAPIError(t *testing.T) {
	err := mapAPIError(401, []byte(`{"error":{"message":"Unauthorized"}}`))
	if err == nil || !strings.Contains(err.Error(), "ключ") {
		t.Fatalf("401: got %v", err)
	}
	err = mapAPIError(429, []byte(`{"error":{"message":"rate limit"}}`))
	if err == nil || !strings.Contains(err.Error(), "лимит") {
		t.Fatalf("429: got %v", err)
	}
}
