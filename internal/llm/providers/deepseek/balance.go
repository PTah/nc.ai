package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"notcursor.ai/app/internal/llm"
)

// AccountBalance is the prepaid / granted snapshot from GET /user/balance.
// Docs: https://api-docs.deepseek.com/api/get-user-balance/
type AccountBalance struct {
	OK           bool    `json:"ok"`
	AvailableUSD float64 `json:"availableUsd"` // total available (units = Currency)
	GrantedUSD   float64 `json:"grantedUsd"`
	ToppedUpUSD  float64 `json:"toppedUpUsd"`
	Currency     string  `json:"currency"` // CNY | USD
	IsAvailable  bool    `json:"isAvailable"`
	Source       string  `json:"source"` // balance | none
	Detail       string  `json:"detail"`
}

type balanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

type balanceResponse struct {
	IsAvailable  bool          `json:"is_available"`
	BalanceInfos []balanceInfo `json:"balance_infos"`
}

// FetchBalance calls GET /user/balance with the API key.
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/user/balance", nil)
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
		detail := "GET /user/balance недоступен"
		if res.StatusCode == 401 || res.StatusCode == 403 {
			detail = fmt.Sprintf("Баланс: ключ отклонён (HTTP %d)", res.StatusCode)
		} else {
			detail = fmt.Sprintf("%s (HTTP %d)", detail, res.StatusCode)
		}
		return AccountBalance{Source: "none", Detail: detail}
	}
	var parsed balanceResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return AccountBalance{Source: "none", Detail: "не удалось разобрать ответ /user/balance"}
	}
	info, ok := pickBalanceInfo(parsed.BalanceInfos)
	if !ok {
		return AccountBalance{
			OK:          true,
			IsAvailable: parsed.IsAvailable,
			Source:      "balance",
			Detail:      "balance · пустой balance_infos",
		}
	}
	total := parseAmount(info.TotalBalance)
	granted := parseAmount(info.GrantedBalance)
	topped := parseAmount(info.ToppedUpBalance)
	currency := strings.TrimSpace(info.Currency)
	if currency == "" {
		currency = "USD"
	}
	return AccountBalance{
		OK:           true,
		AvailableUSD: total,
		GrantedUSD:   granted,
		ToppedUpUSD:  topped,
		Currency:     currency,
		IsAvailable:  parsed.IsAvailable,
		Source:       "balance",
		Detail:       formatBalanceDetail(total, granted, topped, currency, parsed.IsAvailable),
	}
}

func pickBalanceInfo(infos []balanceInfo) (balanceInfo, bool) {
	if len(infos) == 0 {
		return balanceInfo{}, false
	}
	for _, info := range infos {
		if strings.EqualFold(strings.TrimSpace(info.Currency), "USD") {
			return info, true
		}
	}
	return infos[0], true
}

func parseAmount(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func formatMoney(amount float64, currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "CNY", "RMB":
		return fmt.Sprintf("¥%.4f", amount)
	case "USD", "":
		return fmt.Sprintf("$%.4f", amount)
	default:
		return fmt.Sprintf("%.4f %s", amount, currency)
	}
}

func formatBalanceDetail(total, granted, topped float64, currency string, available bool) string {
	line := fmt.Sprintf("bal %s · grant %s · topup %s",
		formatMoney(total, currency),
		formatMoney(granted, currency),
		formatMoney(topped, currency),
	)
	if !available {
		line += " · insufficient"
	}
	return line
}
