package openrouter

import (
	"strings"
	"testing"
)

func TestPreferModel(t *testing.T) {
	list := []string{"qwen/qwen3-coder", "qwen/qwen3-coder-flash:floor"}
	if got := PreferModel(list, "qwen/qwen3-coder"); got != "qwen/qwen3-coder" {
		t.Fatalf("keep current: got %q", got)
	}
	if got := PreferModel(list, "missing"); got != DefaultModel && got != "qwen/qwen3-coder-flash:floor" {
		t.Fatalf("prefer default: got %q", got)
	}
	if got := PreferModel(nil, ""); got != DefaultModel {
		t.Fatalf("empty: got %q", got)
	}
}

func TestOrderAndMerge(t *testing.T) {
	merged := MergeCurated([]string{"openai/gpt-4o", "qwen/qwen3-coder"})
	ordered := OrderModels(merged)
	if len(ordered) < len(CuratedModels) {
		t.Fatalf("expected curated present, got %d", len(ordered))
	}
	if ordered[0] != CuratedModels[0] {
		t.Fatalf("first should be curated[0]=%q got %q", CuratedModels[0], ordered[0])
	}
}

func TestFilterCodingModels(t *testing.T) {
	items := []ModelInfo{
		{ID: "anthropic/claude-sonnet-4"},
		{ID: "qwen/qwen3-coder-extra"},
		{ID: "deepseek/deepseek-chat"},
		{ID: "meta-llama/llama-3"},
	}
	out := FilterCodingModels(items, 10)
	joined := strings.Join(out, ",")
	if !strings.Contains(joined, "qwen/qwen3-coder-extra") {
		t.Fatal("expected qwen coder extra")
	}
	if !strings.Contains(joined, "deepseek/deepseek-chat") {
		t.Fatal("expected deepseek")
	}
	if strings.Contains(joined, "claude-sonnet") {
		t.Fatal("claude should be filtered out of extras")
	}
}

func TestMapAPIError402(t *testing.T) {
	err := mapAPIError(402, []byte(`{"error":{"message":"Insufficient credits"}}`))
	if err == nil || !strings.Contains(err.Error(), "кредитов") {
		t.Fatalf("got %v", err)
	}
}
