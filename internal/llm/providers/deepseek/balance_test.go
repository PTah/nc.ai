package deepseek

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
		if r.URL.Path != "/user/balance" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("auth=%q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"is_available": true,
			"balance_infos": []map[string]string{{
				"currency":          "USD",
				"total_balance":     "10.1807",
				"granted_balance":   "2.0000",
				"topped_up_balance": "8.1807",
			}},
		})
	}))
	t.Cleanup(srv.Close)

	bal := fetchBalance(context.Background(), "sk-test", srv.URL)
	if !bal.OK {
		t.Fatalf("ok=false detail=%s", bal.Detail)
	}
	if bal.AvailableUSD != 10.1807 {
		t.Errorf("total=%v", bal.AvailableUSD)
	}
	if bal.GrantedUSD != 2 {
		t.Errorf("granted=%v", bal.GrantedUSD)
	}
	if bal.ToppedUpUSD != 8.1807 {
		t.Errorf("topup=%v", bal.ToppedUpUSD)
	}
	if bal.Currency != "USD" {
		t.Errorf("currency=%s", bal.Currency)
	}
	if !strings.Contains(bal.Detail, "grant") || !strings.Contains(bal.Detail, "topup") {
		t.Errorf("detail=%q", bal.Detail)
	}
}

func TestFetchBalancePrefersUSD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"is_available": true,
			"balance_infos": []map[string]string{
				{"currency": "CNY", "total_balance": "100", "granted_balance": "10", "topped_up_balance": "90"},
				{"currency": "USD", "total_balance": "12.5", "granted_balance": "0", "topped_up_balance": "12.5"},
			},
		})
	}))
	t.Cleanup(srv.Close)
	bal := fetchBalance(context.Background(), "sk-test", srv.URL)
	if bal.Currency != "USD" || bal.AvailableUSD != 12.5 {
		t.Fatalf("got currency=%s total=%v", bal.Currency, bal.AvailableUSD)
	}
}

func TestFetchBalanceEmptyKey(t *testing.T) {
	bal := FetchBalance(context.Background(), "")
	if bal.OK || bal.Source != "none" {
		t.Fatalf("%+v", bal)
	}
}

func TestFormatBalanceDetailCNY(t *testing.T) {
	got := formatBalanceDetail(110, 10, 100, "CNY", true)
	if !strings.Contains(got, "¥110.0000") || !strings.Contains(got, "grant ¥10.0000") {
		t.Fatalf("%q", got)
	}
}
