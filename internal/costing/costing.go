package costing

import (
	"strings"
	"time"

	"notcursor.ai/app/internal/appmeta"
	"notcursor.ai/app/internal/llm"
)

// Package costing converts provider usage into USD spend.
//
// DeepSeek (peak/off-peak UTC weekdays):
//
//	cost = hit * input_hit + miss * input_miss + completion * output
//	off-peak rates = half of peak; peak windows from official docs (refreshable).
//
// Z.ai (no peak schedule in public pricing; cache ≈ cached input when listed):
//
//	cost = hit * cached_input + miss * input + completion * output
//
// OpenRouter: prefer usage.cost from the API response when present; else sheet.
//
// Free models (glm-4.7-flash, glm-4.5-flash) bill $0.
//
// Live DeepSeek/Z.ai sheets can be overridden by doc refresh (DeepSeek daily, Z.ai weekly).

// Prices is a USD-per-1M-tokens price sheet for one model at one period.
type Prices struct {
	InputMiss  float64 `json:"inputMiss"`  // prompt tokens, cache miss / new content
	InputHit   float64 `json:"inputHit"`   // prompt tokens, cache hit
	Completion float64 `json:"completion"` // completion tokens (incl. reasoning)
}

// builtinOpenRouterSheet is approximate USD / 1M (mid-market). Prefer Usage.CostUSD from API.
// Source snapshot: openrouter.ai/api/v1/models (per-token * 1e6).
var builtinOpenRouterSheet = map[string]Prices{
	"qwen/qwen3-coder-flash": {
		InputMiss: 0.195, InputHit: 0.195, Completion: 0.975,
	},
	"qwen/qwen3-coder-30b-a3b-instruct": {
		InputMiss: 0.07, InputHit: 0.07, Completion: 0.28,
	},
	"qwen/qwen3-coder": {
		InputMiss: 0.30, InputHit: 0.30, Completion: 1.00,
	},
	"qwen/qwen3-coder-plus": {
		InputMiss: 0.65, InputHit: 0.65, Completion: 3.25,
	},
	"qwen/qwen3-coder-next": {
		InputMiss: 0.12, InputHit: 0.12, Completion: 0.80,
	},
	"qwen/qwen3-vl-8b-instruct": {
		InputMiss: 0.117, InputHit: 0.117, Completion: 0.455,
	},
	"qwen/qwen3-vl-32b-instruct": {
		InputMiss: 0.104, InputHit: 0.104, Completion: 0.416,
	},
}

// NormalizeModel maps response/request model ids onto rate-card keys.
// Unknown models return "".
func NormalizeModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimPrefix(m, "deepseek/")
	m = strings.TrimPrefix(m, "zai/")
	// OpenRouter routing suffixes (:floor, :nitro, :exacto, …).
	if i := strings.IndexByte(m, ':'); i > 0 {
		m = m[:i]
	}
	switch {
	case strings.HasPrefix(m, "deepseek-v4-pro"):
		return "deepseek-v4-pro"
	case strings.HasPrefix(m, "deepseek-v4-flash"), strings.HasPrefix(m, "deepseek"):
		return "deepseek-v4-flash"
	case strings.HasPrefix(m, "glm-5.3-flash"):
		return "glm-5.3-flash"
	case strings.HasPrefix(m, "glm-5.3"):
		return "glm-5.3"
	case strings.HasPrefix(m, "glm-5.2"):
		return "glm-5.2"
	case strings.HasPrefix(m, "glm-5.1"):
		return "glm-5.1"
	case strings.HasPrefix(m, "glm-5-turbo"):
		return "glm-5-turbo"
	case m == "glm-5":
		return "glm-5"
	case strings.HasPrefix(m, "glm-4.7-flashx"):
		return "glm-4.7-flashx"
	case strings.HasPrefix(m, "glm-4.7-flash"):
		return "glm-4.7-flash"
	case strings.HasPrefix(m, "glm-4.7"):
		return "glm-4.7"
	case strings.HasPrefix(m, "glm-4.6"):
		return "glm-4.6"
	case strings.HasPrefix(m, "glm-4.5-flash"):
		return "glm-4.5-flash"
	case strings.HasPrefix(m, "glm-4.5-air"):
		return "glm-4.5-air"
	case strings.HasPrefix(m, "glm-4.5"):
		return "glm-4.5"
	case strings.HasPrefix(m, "qwen/qwen3-coder-flash"):
		return "qwen/qwen3-coder-flash"
	case strings.HasPrefix(m, "qwen/qwen3-coder-30b"):
		return "qwen/qwen3-coder-30b-a3b-instruct"
	case strings.HasPrefix(m, "qwen/qwen3-coder-plus"):
		return "qwen/qwen3-coder-plus"
	case strings.HasPrefix(m, "qwen/qwen3-coder-next"):
		return "qwen/qwen3-coder-next"
	case m == "qwen/qwen3-coder" || strings.HasPrefix(m, "qwen/qwen3-coder-"):
		return "qwen/qwen3-coder"
	case strings.HasPrefix(m, "qwen/qwen3-vl-32b"):
		return "qwen/qwen3-vl-32b-instruct"
	case strings.HasPrefix(m, "qwen/qwen3-vl"):
		return "qwen/qwen3-vl-8b-instruct"
	default:
		return ""
	}
}

func isDeepSeekKey(key string) bool {
	return strings.HasPrefix(key, "deepseek")
}

// IsOffPeak reports whether DeepSeek bills off-peak (half) rates at the given UTC instant.
// Official schedule: peak only Mon–Fri 01:00–04:00 and 06:00–10:00 UTC; all other times off-peak.
// https://api-docs.deepseek.com/quick_start/pricing/
func IsOffPeak(at time.Time) bool {
	return !IsPeak(at)
}

// IsPeak reports DeepSeek peak billing at the given UTC instant.
func IsPeak(at time.Time) bool {
	at = at.UTC()
	wd := at.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	minute := at.Hour()*60 + at.Minute()
	for _, w := range currentPeakWindows() {
		if minute >= w.StartMin && minute < w.EndMin {
			return true
		}
	}
	return false
}

// zaiFallback is used for unknown Z.ai models so new models are not billed $0.
var zaiFallbackKey = "glm-4.7"

// Price returns the rate card for model at the given instant.
func Price(model string, at time.Time) Prices {
	key := NormalizeModel(model)
	if key == "deepseek-v4-pro" && appmeta.DeepSeekProRetired(at) {
		// V4 Pro is retired: provider routes it to V4.1 Flash and bills as Flash.
		key = "deepseek-v4-flash"
	}
	zai := currentZaiSheet()
	dsPeak := currentDeepSeekPeak()

	if key == "" {
		m := strings.ToLower(strings.TrimSpace(model))
		m = strings.TrimPrefix(m, "zai/")
		if i := strings.IndexByte(m, ':'); i > 0 {
			m = m[:i]
		}
		if strings.HasPrefix(m, "glm") {
			return zai[zaiFallbackKey]
		}
		if strings.HasPrefix(m, "deepseek") {
			key = "deepseek-v4-flash"
		} else if strings.HasPrefix(m, "qwen/") {
			return liveOpenRouterPrices()["qwen/qwen3-coder"]
		} else {
			return Prices{}
		}
	}
	if p, ok := liveOpenRouterPrices()[key]; ok {
		return p
	}
	if p, ok := zai[key]; ok {
		return p
	}
	peak, ok := dsPeak[key]
	if !ok {
		return Prices{}
	}
	if !isDeepSeekKey(key) {
		return peak
	}
	if key == "deepseek-v4-flash" {
		peak = flashPeakRatesAt(at, peak)
	}
	if IsOffPeak(at) {
		return Prices{
			InputHit:   peak.InputHit / 2,
			InputMiss:  peak.InputMiss / 2,
			Completion: peak.Completion / 2,
		}
	}
	return peak
}

// flashPeakRatesAt picks legacy vs new Flash peak card around the 2026-09-10 cutover.
// After the cutover, prefers a live/refreshed sheet unless it still looks like the old card.
func flashPeakRatesAt(at time.Time, live Prices) Prices {
	if at.Before(flashNewRatesFrom) {
		return flashPeakLegacy
	}
	if live.InputMiss > 0 && live.InputMiss < 0.40 {
		return live
	}
	return flashPeakNew
}

// sanitizeFlashPeak upgrades a persisted/fetched Flash peak row after the 2026-09-10 cutover
// when docs or cache still carry the previous card (miss ≥ $0.40).
func sanitizeFlashPeak(sheet map[string]Prices, at time.Time) {
	if at.Before(flashNewRatesFrom) || sheet == nil {
		return
	}
	if f, ok := sheet["deepseek-v4-flash"]; ok && f.InputMiss >= 0.40 {
		sheet["deepseek-v4-flash"] = flashPeakNew
	}
}

// Cost returns USD for one API response using usage and "now".
func Cost(model string, u *llm.Usage) float64 {
	return CostAt(model, u, time.Now().UTC())
}

// CostAt is Cost with an explicit billing timestamp (for tests / replay).
// When Usage.CostUSD > 0 (OpenRouter native cost), that value wins.
func CostAt(model string, u *llm.Usage, at time.Time) float64 {
	if u == nil {
		return 0
	}
	if u.CostUSD > 0 {
		return u.CostUSD
	}
	p := Price(model, at)
	hit, miss := cacheSplit(u)
	return usd(hit, p.InputHit) + usd(miss, p.InputMiss) + usd(max(u.CompletionTokens, 0), p.Completion)
}

// InputTokens returns the prompt token count for UI counters.
func InputTokens(u *llm.Usage) int {
	if u == nil {
		return 0
	}
	if u.PromptTokens > 0 {
		return u.PromptTokens
	}
	hit, miss := cacheSplit(u)
	return hit + miss
}

func cacheSplit(u *llm.Usage) (hit, miss int) {
	hit = max(u.PromptCacheHitTokens, 0)
	miss = max(u.PromptCacheMissTokens, 0)
	if hit+miss == 0 {
		miss = max(u.PromptTokens, 0)
	}
	return hit, miss
}

func usd(tokens int, per1M float64) float64 {
	if tokens <= 0 || per1M <= 0 {
		return 0
	}
	return float64(tokens) / 1_000_000 * per1M
}
