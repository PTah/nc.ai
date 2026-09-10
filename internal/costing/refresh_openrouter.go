package costing

import (
	"context"
	"encoding/json"
	"strings"
)

const openRouterModelsURL = "https://openrouter.ai/api/v1/models"

type openRouterModelsResp struct {
	Data []struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt         string `json:"prompt"`
			Completion     string `json:"completion"`
			InputCacheRead string `json:"input_cache_read"`
		} `json:"pricing"`
	} `json:"data"`
}

// openRouterWantedModels returns builtin OpenRouter keys plus the currently
// selected model (both full id and its base, e.g. without ":floor").
func openRouterWantedModels(selected string) []string {
	want := map[string]struct{}{}
	for k := range builtinOpenRouterSheet {
		want[k] = struct{}{}
	}
	if selected != "" {
		want[selected] = struct{}{}
		if i := strings.Index(selected, ":"); i > 0 {
			want[selected[:i]] = struct{}{}
		}
	}
	out := make([]string, 0, len(want))
	for k := range want {
		out = append(out, k)
	}
	return out
}

// FetchOpenRouterPricing downloads current per-token pricing for wanted models
// from openrouter.ai/api/v1/models and converts it to USD / 1M tokens.
func FetchOpenRouterPricing(ctx context.Context, wanted []string) (map[string]Prices, error) {
	want := map[string]struct{}{}
	for _, w := range wanted {
		want[w] = struct{}{}
		if i := strings.Index(w, ":"); i > 0 {
			want[w[:i]] = struct{}{}
		}
	}
	if len(want) == 0 {
		for k := range builtinOpenRouterSheet {
			want[k] = struct{}{}
		}
	}

	data, err := httpGet(ctx, openRouterModelsURL)
	if err != nil {
		return nil, err
	}
	var resp openRouterModelsResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	out := map[string]Prices{}
	for _, m := range resp.Data {
		if _, ok := want[m.ID]; !ok {
			continue
		}
		hit := parseFloat(m.Pricing.InputCacheRead)
		if hit <= 0 {
			hit = parseFloat(m.Pricing.Prompt)
		}
		p := Prices{
			InputMiss:  parseFloat(m.Pricing.Prompt) * 1e6,
			InputHit:   hit * 1e6,
			Completion: parseFloat(m.Pricing.Completion) * 1e6,
		}
		if p.InputMiss <= 0 && p.Completion <= 0 {
			continue
		}
		out[m.ID] = p
	}
	return out, nil
}
