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
	}
	for in, want := range cases {
		if got := NormalizeModel(in); got != want {
			t.Fatalf("NormalizeModel(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsPeakWindowsAndWeekend(t *testing.T) {
	// Off-peak window = 16:30–00:30 UTC (00:30–08:30 Beijing).
	// Mon 01:30 UTC → Beijing 09:30 → peak
	if !IsPeak(time.Date(2026, 9, 7, 1, 30, 0, 0, time.UTC)) {
		t.Fatal("Mon 01:30 UTC (Beijing 09:30) should be peak")
	}
	// Mon 05:00 UTC → Beijing 13:00 → peak (daytime)
	if !IsPeak(time.Date(2026, 9, 7, 5, 0, 0, 0, time.UTC)) {
		t.Fatal("Mon 05:00 UTC (Beijing 13:00) should be peak")
	}
	// Mon 17:00 UTC → Beijing 01:00 → off-peak night window
	if IsPeak(time.Date(2026, 9, 7, 17, 0, 0, 0, time.UTC)) {
		t.Fatal("Mon 17:00 UTC (Beijing 01:00) should be off-peak")
	}
	// Mon 23:00 UTC → Beijing 07:00 → off-peak night window
	if IsPeak(time.Date(2026, 9, 7, 23, 0, 0, 0, time.UTC)) {
		t.Fatal("Mon 23:00 UTC (Beijing 07:00) should be off-peak")
	}
	// Mon 12:00 UTC → Beijing 20:00 → peak evening
	if !IsPeak(time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("Mon 12:00 UTC (Beijing 20:00) should be peak")
	}
	// Boundary: 16:29 UTC → peak; 16:30 UTC → off-peak
	if !IsPeak(time.Date(2026, 9, 7, 16, 29, 0, 0, time.UTC)) {
		t.Fatal("Mon 16:29 UTC should be peak (just before window)")
	}
	if IsPeak(time.Date(2026, 9, 7, 16, 30, 0, 0, time.UTC)) {
		t.Fatal("Mon 16:30 UTC should be off-peak (window start)")
	}
	// Boundary: 00:29 UTC → off-peak; 00:30 UTC → peak
	if IsPeak(time.Date(2026, 9, 7, 0, 29, 0, 0, time.UTC)) {
		t.Fatal("Mon 00:29 UTC should be off-peak (just before window end)")
	}
	if !IsPeak(time.Date(2026, 9, 7, 0, 30, 0, 0, time.UTC)) {
		t.Fatal("Mon 00:30 UTC should be peak (window end)")
	}
	// Beijing Saturday 00:30 = 2026-08-28T16:30:00Z → off-peak (weekend rule)
	if IsPeak(time.Date(2026, 8, 28, 16, 30, 0, 0, time.UTC)) {
		t.Fatal("Beijing Sat 00:30 should be off-peak")
	}
	// Saturday daytime (Beijing) → off-peak after weekend rule
	if IsPeak(time.Date(2026, 9, 5, 4, 0, 0, 0, time.UTC)) { // Beijing Sat 12:00
		t.Fatal("Beijing Sat 12:00 should be off-peak (weekend)")
	}
	// Before weekend rule: Fri daytime still peak
	if !IsPeak(time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC)) { // Fri before effective
		t.Fatal("pre-rule Fri 07:00 UTC should be peak")
	}
}

func TestCostOffPeakFlashCacheHit(t *testing.T) {
	// Sunday (Beijing) → off-peak: hit $0.007 / 1M
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) // Sun
	u := &llm.Usage{PromptTokens: 1_000_000, PromptCacheHitTokens: 1_000_000}
	got := CostAt("deepseek-v4-flash", u, at)
	if !almost(got, 0.007) {
		t.Fatalf("flash hit off-peak = %v, want 0.007", got)
	}
}

func TestCostPeakFlashCacheMissAndOut(t *testing.T) {
	at := time.Date(2026, 9, 7, 5, 0, 0, 0, time.UTC) // Mon daytime (Beijing 13:00) → peak
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
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	u := &llm.Usage{
		PromptCacheHitTokens:  500_000,
		PromptCacheMissTokens: 500_000,
		CompletionTokens:      100_000,
	}
	got := CostAt("deepseek-v4-pro", u, at)
	// off-peak: hit 0.022, miss 0.66, out 1.98
	want := 0.5*0.022 + 0.5*0.66 + 0.1*1.98
	if !almost(got, want) {
		t.Fatalf("pro mixed off-peak = %v, want %v", got, want)
	}
}

func TestCostNoCacheSplitFallsBackToMiss(t *testing.T) {
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	u := &llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 100_000}
	got := CostAt("deepseek-v4-pro", u, at)
	want := 0.66 + 0.1*1.98
	if !almost(got, want) {
		t.Fatalf("pro no-split = %v, want %v", got, want)
	}
}

func TestCostVisionUsesFlashCard(t *testing.T) {
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
	at := time.Now().UTC()
	u := &llm.Usage{
		PromptTokens:          1_000_000,
		PromptCacheHitTokens:  800_000,
		PromptCacheMissTokens: 200_000,
		CompletionTokens:      100_000,
	}
	// glm-5.3: hit 0.26, miss 1.4, out 4.4 per 1M
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
