package costing

import (
	"math"
	"testing"
	"time"

	"notcursor.ai/app/internal/llm"
)

func almost(a, b float64) bool {
	return math.Abs(a-b) < 1e-12
}

func TestNormalizeModel(t *testing.T) {
	cases := map[string]string{
		"":                             "",
		"deepseek-v4-flash":            "deepseek-v4-flash",
		"deepseek-v4-flash-0731":       "deepseek-v4-flash",
		"deepseek-v4-flash-vision-exp": "deepseek-v4-flash",
		"deepseek/deepseek-v4-flash":   "deepseek-v4-flash",
		"deepseek-v4-pro":              "deepseek-v4-pro",
		"deepseek-v4-pro-0813":         "deepseek-v4-pro",
		"DeepSeek-V4-Pro":              "deepseek-v4-pro",
		"glm-4.5":                      "glm-4.5",
		"zai/glm-5.1":                  "glm-5.1",
		"glm-4.7-flash":                "glm-4.7-flash",
		"glm-5.3-flash":                "glm-5.3-flash",
		"qwen/qwen3-coder-flash:floor": "qwen/qwen3-coder-flash",
		"qwen/qwen3-coder:floor":       "qwen/qwen3-coder",
		"qwen/qwen3-coder-plus":        "qwen/qwen3-coder-plus",
	}
	for in, want := range cases {
		if got := NormalizeModel(in); got != want {
			t.Fatalf("NormalizeModel(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCostOffPeakFlashCacheHit(t *testing.T) {
	SetDeepSeekPeakSheet(BuiltinDeepSeekPeak())
	SetPeakWindows(DefaultPeakWindows())
	// Sunday → off-peak: hit $0.007 / 1M
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	u := &llm.Usage{PromptTokens: 1_000_000, PromptCacheHitTokens: 1_000_000}
	got := CostAt("deepseek-v4-flash", u, at)
	if !almost(got, 0.007) {
		t.Fatalf("flash hit off-peak = %v, want 0.007", got)
	}
}

func TestCostPeakFlashCacheMissAndOut(t *testing.T) {
	SetDeepSeekPeakSheet(BuiltinDeepSeekPeak())
	SetPeakWindows(DefaultPeakWindows())
	at := time.Date(2026, 9, 7, 7, 0, 0, 0, time.UTC) // Mon 07:00 UTC → peak
	u := &llm.Usage{
		PromptTokens:          1_000_000,
		PromptCacheMissTokens: 1_000_000,
		CompletionTokens:      1_000_000,
	}
	got := CostAt("deepseek-v4-flash", u, at)
	want := 0.44 + 1.32
	if !almost(got, want) {
		t.Fatalf("flash miss+out peak = %v, want %v", got, want)
	}
}

func TestCostProOffPeak(t *testing.T) {
	SetDeepSeekPeakSheet(BuiltinDeepSeekPeak())
	SetPeakWindows(DefaultPeakWindows())
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	u := &llm.Usage{
		PromptCacheHitTokens:  500_000,
		PromptCacheMissTokens: 500_000,
		CompletionTokens:      100_000,
	}
	got := CostAt("deepseek-v4-pro", u, at)
	want := 0.5*0.022 + 0.5*0.66 + 0.1*1.98
	if !almost(got, want) {
		t.Fatalf("pro mixed off-peak = %v, want %v", got, want)
	}
}

func TestCostNoCacheSplitFallsBackToMiss(t *testing.T) {
	SetDeepSeekPeakSheet(BuiltinDeepSeekPeak())
	SetPeakWindows(DefaultPeakWindows())
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	u := &llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 100_000}
	got := CostAt("deepseek-v4-pro", u, at)
	want := 0.66 + 0.1*1.98
	if !almost(got, want) {
		t.Fatalf("pro no-split = %v, want %v", got, want)
	}
}

func TestCostVisionUsesFlashCard(t *testing.T) {
	SetDeepSeekPeakSheet(BuiltinDeepSeekPeak())
	SetPeakWindows(DefaultPeakWindows())
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	u := &llm.Usage{PromptCacheMissTokens: 1_000_000}
	got := CostAt("deepseek-v4-flash-vision-exp", u, at)
	if !almost(got, 0.22) {
		t.Fatalf("vision miss off-peak = %v, want 0.22", got)
	}
}

func TestCostNilUsage(t *testing.T) {
	if got := Cost("deepseek-v4-flash", nil); got != 0 {
		t.Fatalf("nil usage cost = %v, want 0", got)
	}
}

func TestCostZaiCacheAndFree(t *testing.T) {
	SetZaiSheet(BuiltinZaiSheet())
	at := time.Now().UTC()
	u := &llm.Usage{
		PromptTokens:          1_000_000,
		PromptCacheHitTokens:  800_000,
		PromptCacheMissTokens: 200_000,
		CompletionTokens:      100_000,
	}
	got := CostAt("glm-5.3", u, at)
	want := 0.8*0.26 + 0.2*1.4 + 0.1*4.4
	if !almost(got, want) {
		t.Fatalf("zai cost=%v want %v", got, want)
	}
	if got := CostAt("glm-4.7-flash", u, at); got != 0 {
		t.Fatalf("free model should be 0, got %v", got)
	}
}

func TestInputTokens(t *testing.T) {
	u := &llm.Usage{PromptCacheHitTokens: 10, PromptCacheMissTokens: 20}
	if got := InputTokens(u); got != 30 {
		t.Fatalf("InputTokens=%d want 30", got)
	}
	u2 := &llm.Usage{PromptTokens: 99, PromptCacheHitTokens: 10}
	if got := InputTokens(u2); got != 99 {
		t.Fatalf("InputTokens prefers PromptTokens=%d want 99", got)
	}
}

func TestCostOpenRouterNativeCostWins(t *testing.T) {
	u := &llm.Usage{
		PromptTokens:     1_000_000,
		CompletionTokens: 1_000_000,
		CostUSD:          0.0042,
	}
	got := CostAt("qwen/qwen3-coder-flash:floor", u, time.Now().UTC())
	if !almost(got, 0.0042) {
		t.Fatalf("native cost should win: got %v", got)
	}
}

func TestCostOpenRouterSheetFallback(t *testing.T) {
	u := &llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	got := CostAt("qwen/qwen3-coder-flash", u, time.Now().UTC())
	want := 0.195 + 0.975
	if !almost(got, want) {
		t.Fatalf("sheet fallback = %v want %v", got, want)
	}
}
