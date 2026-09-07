package zai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AccountBalance is a best-effort snapshot of remaining Z.ai credits / quota.
// Pay-as-you-go cash balance has no fully documented public API; we try
// credit_grants and Coding Plan monitor endpoints when available.
type AccountBalance struct {
	OK           bool    `json:"ok"`
	AvailableUSD float64 `json:"availableUsd"`
	UsedUSD      float64 `json:"usedUsd"`
	Source       string  `json:"source"` // credit_grants | quota | none
	Detail       string  `json:"detail"`
}

type creditGrantsResponse struct {
	Data []struct {
		Amount    float64 `json:"amount"`
		UsedAmount float64 `json:"used_amount"`
		Available float64 `json:"available"`
		ExpireAt  string  `json:"expire_at"`
	} `json:"data"`
	// Some envelopes nest differently.
	Object string `json:"object"`
	TotalGranted  float64 `json:"total_granted"`
	TotalUsed     float64 `json:"total_used"`
	TotalAvailable float64 `json:"total_available"`
}

// FetchBalance probes undocumented/community endpoints for remaining funds.
func FetchBalance(ctx context.Context, apiKey, endpoint string) AccountBalance {
	if apiKey == "" {
		return AccountBalance{Detail: "API key is empty"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 12 * time.Second}

	// 1) PAYG-ish credit grants (OpenUsage / community).
	if bal, ok := fetchCreditGrants(ctx, client, apiKey); ok {
		return bal
	}
	// 2) Coding Plan quota monitor.
	if bal, ok := fetchQuotaLimit(ctx, client, apiKey); ok {
		return bal
	}
	return AccountBalance{
		Source: "none",
		Detail: "Баланс pay-as-you-go через публичный API недоступен. Смотрите кабинет z.ai / Billing. Для Coding Plan остаток квоты тоже может не отдаваться этим ключом.",
	}
}

func fetchCreditGrants(ctx context.Context, client *http.Client, apiKey string) (AccountBalance, bool) {
	urls := []string{
		"https://api.z.ai/api/paas/v4/user/credit_grants",
		"https://api.z.ai/api/paas/v4/dashboard/billing/credit_grants",
	}
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")
		res, err := client.Do(req)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode >= 300 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		avail, used, ok := parseCreditGrants(raw)
		if !ok {
			continue
		}
		return AccountBalance{
			OK:           true,
			AvailableUSD: avail,
			UsedUSD:      used,
			Source:       "credit_grants",
			Detail:       fmt.Sprintf("credit grants · available $%.4f · used $%.4f", avail, used),
		}, true
	}
	return AccountBalance{}, false
}

func parseCreditGrants(raw map[string]any) (avail, used float64, ok bool) {
	if v, has := asFloat(raw["total_available"]); has {
		avail = v
		used, _ = asFloat(raw["total_used"])
		return avail, used, true
	}
	// Nested data array of grants.
	data, _ := raw["data"].([]any)
	if len(data) == 0 {
		if grants, _ := raw["grants"].([]any); len(grants) > 0 {
			data = grants
		}
	}
	if len(data) == 0 {
		return 0, 0, false
	}
	for _, item := range data {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if a, has := asFloat(m["available"]); has {
			avail += a
		} else if a, has := asFloat(m["amount"]); has {
			u, _ := asFloat(m["used_amount"])
			avail += a - u
			used += u
			continue
		}
		if u, has := asFloat(m["used_amount"]); has {
			used += u
		} else if u, has := asFloat(m["used"]); has {
			used += u
		}
	}
	return avail, used, true
}

func fetchQuotaLimit(ctx context.Context, client *http.Client, apiKey string) (AccountBalance, bool) {
	urls := []string{
		"https://api.z.ai/api/monitor/usage/quota/limit",
		"https://api.z.ai/api/monitor/usage",
	}
	for _, u := range urls {
		for _, auth := range []string{"Bearer " + apiKey, apiKey} {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				continue
			}
			req.Header.Set("Authorization", auth)
			req.Header.Set("Accept", "application/json")
			res, err := client.Do(req)
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode >= 300 {
				continue
			}
			detail := strings.TrimSpace(truncate(string(data), 240))
			// Try to extract remaining percentage / tokens loosely.
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				continue
			}
			if msg, _ := raw["msg"].(string); strings.Contains(strings.ToLower(msg), "coding") {
				continue
			}
			return AccountBalance{
				OK:     true,
				Source: "quota",
				Detail: "Coding Plan / monitor: " + detail,
			}, true
		}
	}
	return AccountBalance{}, false
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		var f float64
		_, err := fmt.Sscanf(x, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}
