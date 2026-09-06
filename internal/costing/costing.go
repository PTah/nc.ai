// Package costing converts LLM token usage into USD spend.
//
// The spend counter shown in the top bar (CLine-style "total cost") is backed
// by the usage object DeepSeek returns in every chat/completions response:
//
//	cost = prompt_cache_hit_tokens * input_hit   +
//	       prompt_cache_miss_tokens * input_miss +
//	       completion_tokens * output
//
// Prices are USD per 1M tokens. They are approximate per-model defaults and
// are centralised here so they can be adjusted when a provider publishes
// new pricing (see docs/exchange-protocols/deepseek.md).
package costing

import "notcursor.ai/app/internal/llm"

// Prices is a USD-per-1M-tokens price sheet for one model.
type Prices struct {
	InputMiss  float64 // prompt tokens, cache miss
	InputHit   float64 // prompt tokens, cache hit
	Completion float64 // completion tokens (incl. reasoning)
}

// priceSheet mirrors the models exposed by DeepSeek.
var priceSheet = map[string]Prices{
	// Fast/cheap coding model.
	"deepseek-v4-flash": {InputMiss: 0.27, InputHit: 0.07, Completion: 1.10},
	// Max-quality reasoning model.
	"deepseek-v4-pro": {InputMiss: 0.55, InputHit: 0.14, Completion: 2.19},
}

// defaultPrices is used for unknown/aliased model ids so the counter always works.
var defaultPrices = priceSheet["deepseek-v4-flash"]

// Price returns the price sheet for model, falling back to a default when the
// model id is not recognised.
func Price(model string) Prices {
	if p, ok := priceSheet[model]; ok {
		return p
	}
	return defaultPrices
}

// Cost returns the USD cost of a single API response based on its token usage.
// It returns 0 when usage is nil (some providers omit it for short/error calls).
func Cost(model string, u *llm.Usage) float64 {
	if u == nil {
		return 0
	}
	p := Price(model)
	hit := max(u.PromptCacheHitTokens, 0)
	miss := max(u.PromptCacheMissTokens, 0)
	if hit+miss == 0 {
		// Provider did not report the cache split: treat the whole prompt
		// as a cache miss (conservative upper bound).
		miss = max(u.PromptTokens, 0)
	}
	return usd(hit, p.InputHit) + usd(miss, p.InputMiss) + usd(max(u.CompletionTokens, 0), p.Completion)
}

func usd(tokens int, per1M float64) float64 {
	if tokens <= 0 || per1M <= 0 {
		return 0
	}
	return float64(tokens) / 1_000_000 * per1M
}
