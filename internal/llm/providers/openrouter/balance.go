package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AccountBalance is remaining OpenRouter credits (prepaid).
// GET /api/v1/credits — may require a management key; regular keys often work too.
type AccountBalance struct {
	OK           bool    `json:"ok"`
	AvailableUSD float64 `json:"availableUsd"`
	UsedUSD      float64 `json:"usedUsd"`
	TotalUSD     float64 `json:"totalUsd"`
	Source       string  `json:"source"` // credits | none
	Detail       string  `json:"detail"`
}

type creditsResponse struct {
	Data struct {
		TotalCredits float64 `json:"total_credits"`
		TotalUsage   float64 `json:"total_usage"`
	} `json:"data"`
}

// FetchBalance probes GET /credits for remaining prepaid funds.
func FetchBalance(ctx context.Context, apiKey string) AccountBalance {
	if apiKey == "" {
		return AccountBalance{Detail: "API key is empty", Source: "none"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultBaseURL+"/credits", nil)
	if err != nil {
		return AccountBalance{Source: "none", Detail: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("HTTP-Referer", AppReferer)
	req.Header.Set("X-OpenRouter-Title", AppTitle)

	res, err := client.Do(req)
	if err != nil {
		return AccountBalance{Source: "none", Detail: err.Error()}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		detail := "GET /credits недоступен этим ключом"
		if res.StatusCode == 401 || res.StatusCode == 403 {
			detail = "Баланс через API требует management key (Keys → Management). Смотрите openrouter.ai/credits"
		}
		return AccountBalance{
			Source: "none",
			Detail: fmt.Sprintf("%s (HTTP %d)", detail, res.StatusCode),
		}
	}
	var parsed creditsResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return AccountBalance{Source: "none", Detail: "не удалось разобрать ответ /credits"}
	}
	total := parsed.Data.TotalCredits
	used := parsed.Data.TotalUsage
	avail := total - used
	if avail < 0 {
		avail = 0
	}
	return AccountBalance{
		OK:           true,
		AvailableUSD: avail,
		UsedUSD:      used,
		TotalUSD:     total,
		Source:       "credits",
		Detail:       fmt.Sprintf("credits · available $%.4f · used $%.4f / $%.4f", avail, used, total),
	}
}
