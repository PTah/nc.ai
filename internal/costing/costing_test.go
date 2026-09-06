package costing

import (
	"math"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func almost(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestCostCacheHit(t *testing.T) {
	u := &llm.Usage{
		PromptTokens:          1_000_000,
		PromptCacheHitTokens:  1_000_000,
		PromptCacheMissTokens: 0,
	}
	got := Cost("deepseek-v4-flash", u)
	if !almost(got, 0.07) {
		t.Fatalf("hit-only flash cost = %v, want 0.07", got)
	}
}

func TestCostCacheMiss(t *testing.T) {
	u := &llm.Usage{
		PromptTokens:          1_000_000,
		PromptCacheHitTokens:  0,
		PromptCacheMissTokens: 1_000_000,
	}
	got := Cost("deepseek-v4-flash", u)
	if !almost(got, 0.27) {
		t.Fatalf("miss-only flash cost = %v, want 0.27", got)
	}
}

func TestCostCompletion(t *testing.T) {
	u := &llm.Usage{PromptTokens: 0, CompletionTokens: 1_000_000}
	got := Cost("deepseek-v4-flash", u)
	if !almost(got, 1.10) {
		t.Fatalf("completion-only flash cost = %v, want 1.10", got)
	}
}

func TestCostNoCacheSplitFallsBackToMiss(t *testing.T) {
	u := &llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 100_000}
	got := Cost("deepseek-v4-pro", u)
	if !almost(got, 0.55+0.219) {
		t.Fatalf("pro cost = %v, want 0.769", got)
	}
}

func TestCostUnknownModelUsesDefault(t *testing.T) {
	u := &llm.Usage{PromptTokens: 1_000_000}
	got := Cost("totally-unknown-model", u)
	if !almost(got, 0.27) {
		t.Fatalf("unknown-model cost = %v, want default 0.27", got)
	}
}

func TestCostNilUsage(t *testing.T) {
	if got := Cost("deepseek-v4-flash", nil); got != 0 {
		t.Fatalf("nil usage cost = %v, want 0", got)
	}
}
