package costing

import (
	"fmt"
	"sort"
	"time"
)

// ModelPrice is one rate-card row for the Model prices UI (USD per 1M tokens).
type ModelPrice struct {
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	InputUSD       float64 `json:"inputUsd"` // prompt / cache-miss (DeepSeek: peak miss)
	OutputUSD      float64 `json:"outputUsd"`
	CacheHitUSD    float64 `json:"cacheHitUsd,omitempty"`
	InputOffUSD    float64 `json:"inputOffUsd,omitempty"`
	OutputOffUSD   float64 `json:"outputOffUsd,omitempty"`
	CacheHitOffUSD float64 `json:"cacheHitOffUsd,omitempty"`
	Strength       int     `json:"strength"` // lower = weaker / cheaper tier
	Note           string  `json:"note,omitempty"`
	Free           bool    `json:"free,omitempty"`
}

// Catalog returns curated models ordered weakest → strongest (by Strength).
func Catalog() []ModelPrice {
	ds := currentDeepSeekPeak()
	flash := flashPeakRatesAt(time.Now().UTC(), ds["deepseek-v4-flash"])
	pro := ds["deepseek-v4-pro"]
	if pro.InputMiss <= 0 {
		pro = BuiltinDeepSeekPeak()["deepseek-v4-pro"]
	}
	wins := FormatPeakWindowsLocal(time.Local)
	flashNote := fmt.Sprintf(
		"peak miss $%.2f / out $%.2f · off-peak miss $%.2f / out $%.2f · peak %s",
		flash.InputMiss, flash.Completion, flash.InputMiss/2, flash.Completion/2, wins,
	)
	proNote := fmt.Sprintf(
		"peak miss $%.2f / out $%.2f · off-peak ≈ ½ · peak %s",
		pro.InputMiss, pro.Completion, wins,
	)

	rows := []ModelPrice{
		{
			Provider: "DeepSeek", Model: "deepseek-v4-flash",
			InputUSD: flash.InputMiss, OutputUSD: flash.Completion, CacheHitUSD: flash.InputHit,
			InputOffUSD: flash.InputMiss / 2, OutputOffUSD: flash.Completion / 2, CacheHitOffUSD: flash.InputHit / 2,
			Strength: 10, Note: flashNote,
		},
		{
			Provider: "DeepSeek", Model: "deepseek-v4-flash-vision-exp",
			InputUSD: flash.InputMiss, OutputUSD: flash.Completion, CacheHitUSD: flash.InputHit,
			InputOffUSD: flash.InputMiss / 2, OutputOffUSD: flash.Completion / 2, CacheHitOffUSD: flash.InputHit / 2,
			Strength: 12, Note: "vision · same Flash card · " + wins,
		},
		{
			Provider: "DeepSeek", Model: "deepseek-v4-pro",
			InputUSD: pro.InputMiss, OutputUSD: pro.Completion, CacheHitUSD: pro.InputHit,
			InputOffUSD: pro.InputMiss / 2, OutputOffUSD: pro.Completion / 2, CacheHitOffUSD: pro.InputHit / 2,
			Strength: 40, Note: proNote,
		},

		{Provider: "Z.ai", Model: "glm-4.7-flash", InputUSD: 0, OutputUSD: 0, Strength: 1, Free: true, Note: "free PAYG"},
		{Provider: "Z.ai", Model: "glm-4.5-flash", InputUSD: 0, OutputUSD: 0, Strength: 2, Free: true, Note: "free PAYG"},
		{Provider: "Z.ai", Model: "glm-4.5-air", InputUSD: 0.2, OutputUSD: 1.1, CacheHitUSD: 0.03, Strength: 15},
		{Provider: "Z.ai", Model: "glm-5.3-flash", InputUSD: 0.15, OutputUSD: 0.50, CacheHitUSD: 0.03, Strength: 18, Note: "vision-capable"},
		{Provider: "Z.ai", Model: "glm-4.7", InputUSD: 0.6, OutputUSD: 2.2, CacheHitUSD: 0.11, Strength: 25},
		{Provider: "Z.ai", Model: "glm-5", InputUSD: 1.0, OutputUSD: 3.2, CacheHitUSD: 0.2, Strength: 32},
		{Provider: "Z.ai", Model: "glm-5.3", InputUSD: 1.4, OutputUSD: 4.4, CacheHitUSD: 0.26, Strength: 45, Note: "flagship"},

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
