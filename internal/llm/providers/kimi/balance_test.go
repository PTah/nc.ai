package kimi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchBalanceOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me/balance" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("auth=%q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"available_balance": 49.58894,
				"voucher_balance":   46.58893,
				"cash_balance":      3.00001,
			},
			"scode":  "0x0",
			"status": true,
		})
	}))
	t.Cleanup(srv.Close)

	bal := fetchBalance(context.Background(), "sk-test", srv.URL)
	if !bal.OK {
		t.Fatalf("ok=false detail=%s", bal.Detail)
	}
	if bal.AvailableUSD != 49.58894 {
		t.Errorf("avail=%v", bal.AvailableUSD)
	}
	if bal.VoucherUSD != 46.58893 {
		t.Errorf("voucher=%v", bal.VoucherUSD)
	}
	if bal.CashUSD != 3.00001 {
		t.Errorf("cash=%v", bal.CashUSD)
	}
	if !strings.Contains(bal.Detail, "voucher") || !strings.Contains(bal.Detail, "cash") {
		t.Errorf("detail=%q", bal.Detail)
	}
}

func TestFetchBalanceEmptyKey(t *testing.T) {
	bal := FetchBalance(context.Background(), "")
	if bal.OK || bal.Source != "none" {
		t.Fatalf("%+v", bal)
	}
}

func TestFormatBalanceDetailInsufficient(t *testing.T) {
	got := formatBalanceDetail(0, 0, 0)
	if !strings.Contains(got, "insufficient") {
		t.Fatalf("%q", got)
	}
}
