package costing

import "sort"

// ModelPrice is one rate-card row for the Model prices UI (USD per 1M tokens).
type ModelPrice struct {
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	InputUSD   float64 `json:"inputUsd"`   // prompt / cache-miss (DeepSeek: peak miss)
	OutputUSD  float64 `json:"outputUsd"`  // completion (DeepSeek: peak)
	CacheHitUSD float64 `json:"cacheHitUsd,omitempty"`
	Strength   int     `json:"strength"` // lower = weaker / cheaper tier
	Note       string  `json:"note,omitempty"`
	Free       bool    `json:"free,omitempty"`
}

// Catalog returns curated models ordered weakest → strongest (by Strength).
func Catalog() []ModelPrice {
	rows := []ModelPrice{
		// DeepSeek (peak rates; off-peak ≈ half)
		{Provider: "DeepSeek", Model: "deepseek-v4-flash", InputUSD: 0.44, OutputUSD: 1.32, CacheHitUSD: 0.014, Strength: 10, Note: "peak; off-peak ≈ ½"},
		{Provider: "DeepSeek", Model: "deepseek-v4-flash-vision-exp", InputUSD: 0.44, OutputUSD: 1.32, CacheHitUSD: 0.014, Strength: 12, Note: "vision · peak rates like flash"},
		{Provider: "DeepSeek", Model: "deepseek-v4-pro", InputUSD: 1.32, OutputUSD: 3.96, CacheHitUSD: 0.044, Strength: 40, Note: "peak; off-peak ≈ ½"},

		// Z.ai (PAYG)
		{Provider: "Z.ai", Model: "glm-4.7-flash", InputUSD: 0, OutputUSD: 0, Strength: 1, Free: true, Note: "free PAYG"},
		{Provider: "Z.ai", Model: "glm-4.5-flash", InputUSD: 0, OutputUSD: 0, Strength: 2, Free: true, Note: "free PAYG"},
		{Provider: "Z.ai", Model: "glm-4.5-air", InputUSD: 0.2, OutputUSD: 1.1, CacheHitUSD: 0.03, Strength: 15},
		{Provider: "Z.ai", Model: "glm-5.3-flash", InputUSD: 0.15, OutputUSD: 0.50, CacheHitUSD: 0.03, Strength: 18, Note: "vision-capable"},
		{Provider: "Z.ai", Model: "glm-4.7", InputUSD: 0.6, OutputUSD: 2.2, CacheHitUSD: 0.11, Strength: 25},
		{Provider: "Z.ai", Model: "glm-5", InputUSD: 1.0, OutputUSD: 3.2, CacheHitUSD: 0.2, Strength: 32},
		{Provider: "Z.ai", Model: "glm-5.3", InputUSD: 1.4, OutputUSD: 4.4, CacheHitUSD: 0.26, Strength: 45, Note: "flagship"},

		// OpenRouter mid-market (prefer usage.cost when billed)
		{Provider: "OpenRouter", Model: "qwen/qwen3-coder-next", InputUSD: 0.12, OutputUSD: 0.80, Strength: 14, Note: "mid-market; :floor may be cheaper"},
		{Provider: "OpenRouter", Model: "qwen/qwen3-coder-flash", InputUSD: 0.195, OutputUSD: 0.975, Strength: 16, Note: "Auto default"},
		{Provider: "OpenRouter", Model: "qwen/qwen3-coder-30b-a3b-instruct", InputUSD: 0.07, OutputUSD: 0.28, Strength: 17},
		{Provider: "OpenRouter", Model: "qwen/qwen3-vl-8b-instruct", InputUSD: 0.117, OutputUSD: 0.455, Strength: 19, Note: "vision"},
		{Provider: "OpenRouter", Model: "qwen/qwen3-coder", InputUSD: 0.30, OutputUSD: 1.00, Strength: 28, Note: "Auto complex"},
		{Provider: "OpenRouter", Model: "qwen/qwen3-coder-plus", InputUSD: 0.65, OutputUSD: 3.25, Strength: 42, Note: "strongest curated coder"},
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Strength != rows[j].Strength {
			return rows[i].Strength < rows[j].Strength
		}
		return rows[i].Model < rows[j].Model
	})
	return rows
}
