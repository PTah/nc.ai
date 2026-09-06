// Package costing converts DeepSeek chat/completions usage into USD spend.
//
// Billable formula (official rate card, USD per 1M tokens):
//
//	cost = prompt_cache_hit_tokens  * input_hit  +
//	       prompt_cache_miss_tokens * input_miss +
//	       completion_tokens        * output
//
// Rates depend on model and peak/off-peak. Peak hours (Beijing calendar):
// Mon–Fri 09:00–12:00 and 14:00–18:00 (= 01:00–04:00 and 06:00–10:00 UTC).
// Since 2026-08-23 weekends are entirely off-peak (Beijing weekday).
// Off-peak = half of peak for every token type.
//
// Source: https://api-docs.deepseek.com/ + DeepSeek Models & Pricing.
package costing

import (
	"strings"
	"time"

	"notcursor.ai/app/internal/llm"
)

// Prices is a USD-per-1M-tokens price sheet for one model at one period.
type Prices struct {
	InputMiss  float64 // prompt tokens, cache miss
	InputHit   float64 // prompt tokens, cache hit
	Completion float64 // completion tokens (incl. reasoning)
}

// Peak rates (USD / 1M). Off-peak = half.
var peakSheet = map[string]Prices{
	"deepseek-v4-flash": {
		InputHit: 0.014, InputMiss: 0.44, Completion: 1.32,
	},
	"deepseek-v4-pro": {
		InputHit: 0.044, InputMiss: 1.32, Completion: 3.96,
	},
}

// weekendOffpeakEffective is when DeepSeek began weekend-wide off-peak
// (00:00 Beijing 2026-08-23 = 2026-08-22T16:00:00Z).
var weekendOffpeakEffective = time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

// beijingOffsetHours: CST = UTC+8 (no DST).
const beijingOffsetHours = 8

// NormalizeModel maps response/request model ids onto the rate-card keys.
func NormalizeModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimPrefix(m, "deepseek/")
	switch {
	case strings.HasPrefix(m, "deepseek-v4-pro"):
		return "deepseek-v4-pro"
	case m == "" || strings.HasPrefix(m, "deepseek-v4-flash"):
		// flash, flash-0731, flash-vision-exp → flash rate card
		return "deepseek-v4-flash"
	default:
		return "deepseek-v4-flash"
	}
}

// IsPeak reports whether DeepSeek bills peak rates at the given UTC instant.
// Weekday is read from Beijing local time (official Chinese wording).
func IsPeak(at time.Time) bool {
	at = at.UTC()
	beijing := at.Add(beijingOffsetHours * time.Hour)
	// After the weekend rule: Sat/Sun (Beijing) are always off-peak.
	if !at.Before(weekendOffpeakEffective) {
		wd := beijing.Weekday() // Sunday=0 … Saturday=6
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
	}
	minute := at.Hour()*60 + at.Minute()
	// Peak windows in UTC: [01:00, 04:00) and [06:00, 10:00).
	if (minute >= 60 && minute < 240) || (minute >= 360 && minute < 600) {
		return true
	}
	return false
}

// Price returns the rate card for model at the given instant.
func Price(model string, at time.Time) Prices {
	key := NormalizeModel(model)
	peak, ok := peakSheet[key]
	if !ok {
		peak = peakSheet["deepseek-v4-flash"]
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

// Cost returns USD for one API response using usage from DeepSeek and "now".
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
		// No cache breakdown: bill whole prompt as miss (conservative).
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
