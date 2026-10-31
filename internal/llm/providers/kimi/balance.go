package kimi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"notcursor.ai/app/internal/llm"
)

// AccountBalance is the snapshot from GET /v1/users/me/balance.
// Docs: https://platform.kimi.ai/docs/api/balance
type AccountBalance struct {
	OK           bool    `json:"ok"`
	AvailableUSD float64 `json:"availableUsd"`
	VoucherUSD   float64 `json:"voucherUsd"`
	CashUSD      float64 `json:"cashUsd"`
	Currency     string  `json:"currency"` // always USD on global platform
	Source       string  `json:"source"`   // balance | none
	Detail       string  `json:"detail"`
}

type balanceData struct {
	AvailableBalance float64 `json:"available_balance"`
	VoucherBalance   float64 `json:"voucher_balance"`
	CashBalance      float64 `json:"cash_balance"`
}

type balanceResponse struct {
	Code   int         `json:"code"`
	Data   balanceData `json:"data"`
	Status bool        `json:"status"`
	SCode  string      `json:"scode"`
}

// FetchBalance calls GET /users/me/balance with the API key (global endpoint).
func FetchBalance(ctx context.Context, apiKey string) AccountBalance {
	return fetchBalance(ctx, apiKey, DefaultBaseURL)
}

func fetchBalance(ctx context.Context, apiKey, baseURL string) AccountBalance {
	if apiKey == "" {
		return AccountBalance{Detail: "API key is empty", Source: "none"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	client := &http.Client{
		Timeout:   12 * time.Second,
		Transport: llm.Transport(),
	}
	url := strings.TrimRight(baseURL, "/") + "/users/me/balance"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return AccountBalance{Source: "none", Detail: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return AccountBalance{Source: "none", Detail: err.Error()}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		detail := "GET /users/me/balance недоступен"
		if res.StatusCode == 401 || res.StatusCode == 403 {
			detail = fmt.Sprintf("Баланс: ключ отклонён (HTTP %d) — проверьте регион (global vs CN)", res.StatusCode)
		} else {
			detail = fmt.Sprintf("%s (HTTP %d)", detail, res.StatusCode)
		}
		return AccountBalance{Source: "none", Detail: detail}
	}
	var parsed balanceResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return AccountBalance{Source: "none", Detail: "не удалось разобрать ответ /users/me/balance"}
	}
	if parsed.Code != 0 && !parsed.Status {
		return AccountBalance{
			Source: "none",
			Detail: fmt.Sprintf("баланс: code=%d scode=%s", parsed.Code, parsed.SCode),
		}
	}
	avail := parsed.Data.AvailableBalance
	voucher := parsed.Data.VoucherBalance
	cash := parsed.Data.CashBalance
	return AccountBalance{
		OK:           true,
		AvailableUSD: avail,
		VoucherUSD:   voucher,
		CashUSD:      cash,
		Currency:     "USD",
		Source:       "balance",
		Detail:       formatBalanceDetail(avail, voucher, cash),
	}
}

func formatBalanceDetail(avail, voucher, cash float64) string {
	line := fmt.Sprintf("bal $%.4f · voucher $%.4f · cash $%.4f", avail, voucher, cash)
	if avail <= 0 {
		line += " · insufficient"
	}
	return line
}
