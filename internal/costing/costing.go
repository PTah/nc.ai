package costing

import (
	"strings"
	"time"

	"notcursor.ai/app/internal/llm"
)

// Package costing converts provider usage into USD spend.
//
// DeepSeek (peak/off-peak Beijing):
//
//	cost = hit * input_hit + miss * input_miss + completion * output
//
// Z.ai (no peak schedule in public pricing; cache ≈ 1/5 of input when listed):
//
//	cost = hit * cached_input + miss * input + completion * output
//
// Free models (glm-4.7-flash, glm-4.5-flash) bill $0.

// Prices is a USD-per-1M-tokens price sheet for one model at one period.
type Prices struct {
	InputMiss  float64 // prompt tokens, cache miss / new content
	InputHit   float64 // prompt tokens, cache hit
	Completion float64 // completion tokens (incl. reasoning)
}

// Peak rates (USD / 1M). Off-peak = half. DeepSeek only.
var peakSheet = map[string]Prices{
	"deepseek-v4-flash": {
		InputHit: 0.014, InputMiss: 0.44, Completion: 1.32,
	},
	"deepseek-v4-pro": {
		InputHit: 0.044, InputMiss: 1.32, Completion: 3.96,
	},
}

// zaiSheet is USD / 1M from https://docs.z.ai/guides/overview/pricing (pay-as-you-go).
// Cached Input Storage is billed separately by Z.ai (currently limited-time free) — not modeled here.
var zaiSheet = map[string]Prices{
	"glm-5.3": {
		InputMiss: 1.4, InputHit: 0.26, Completion: 4.4,
	},
	"glm-5.3-flash": {
		InputMiss: 0.15, InputHit: 0.03, Completion: 0.50,
	},
	"glm-5.2": {
		InputMiss: 1.4, InputHit: 0.26, Completion: 4.4,
	},
	"glm-5.1": {
		InputMiss: 1.4, InputHit: 0.26, Completion: 4.4,
	},
	"glm-5": {
		InputMiss: 1.0, InputHit: 0.2, Completion: 3.2,
	},
	"glm-5-turbo": {
		InputMiss: 1.2, InputHit: 0.24, Completion: 4.0,
	},
	"glm-4.7": {
		InputMiss: 0.6, InputHit: 0.11, Completion: 2.2,
	},
	"glm-4.7-flash": {}, // free
	"glm-4.6": {
		InputMiss: 0.6, InputHit: 0.11, Completion: 2.2,
	},
	"glm-4.5": {
		InputMiss: 0.6, InputHit: 0.11, Completion: 2.2,
	},
	"glm-4.5-air": {
		InputMiss: 0.2, InputHit: 0.03, Completion: 1.1,
	},
	"glm-4.5-flash": {}, // free
}

// weekendOffpeakEffective is when DeepSeek began weekend-wide off-peak
// (00:00 Beijing 2026-08-23 = 2026-08-22T16:00:00Z).
var weekendOffpeakEffective = time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

const beijingOffsetHours = 8

// NormalizeModel maps response/request model ids onto rate-card keys.
// Unknown models return "".
func NormalizeModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimPrefix(m, "deepseek/")
	m = strings.TrimPrefix(m, "zai/")
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
	default:
		return ""
	}
}

func isDeepSeekKey(key string) bool {
	return strings.HasPrefix(key, "deepseek")
}

// IsPeak reports whether DeepSeek bills peak rates at the given UTC instant.
func IsPeak(at time.Time) bool {
	at = at.UTC()
	beijing := at.Add(beijingOffsetHours * time.Hour)
	if !at.Before(weekendOffpeakEffective) {
		wd := beijing.Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
	}
	minute := at.Hour()*60 + at.Minute()
	if (minute >= 60 && minute < 240) || (minute >= 360 && minute < 600) {
		return true
	}
	return false
}

// Price returns the rate card for model at the given instant.
func Price(model string, at time.Time) Prices {
	key := NormalizeModel(model)
	if key == "" {
		return Prices{}
	}
	if p, ok := zaiSheet[key]; ok {
		return p
	}
	peak, ok := peakSheet[key]
	if !ok {
		return Prices{}
	}
	if !isDeepSeekKey(key) {
		return peak
	}
	if IsPeak(at) {
		return peak
	}
	return Prices{
		InputHit:   peak.InputHit / 2,
		InputMiss:  peak.InputMiss / 2,
		Completion: peak.Completion / 2,
	}
}

// Cost returns USD for one API response using usage and "now".
func Cost(model string, u *llm.Usage) float64 {
	return CostAt(model, u, time.Now().UTC())
}

// CostAt is Cost with an explicit billing timestamp (for tests / replay).
func CostAt(model string, u *llm.Usage, at time.Time) float64 {
	if u == nil {
		return 0
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
