// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:46:14
// Created by nc.ai 1.1.16 on macbook 2026-11-11
package llm

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Anthropic отдаёт момент сброса окна в RFC3339, а не секундами: без этого
// разбора Wait=0 и агент не ждёт вовсе.
func TestRetryAfterAnthropicReset(t *testing.T) {
	h := http.Header{}
	reset := time.Now().Add(90 * time.Second).UTC().Format(time.RFC3339)
	h.Set("anthropic-ratelimit-requests-reset", reset)

	got := RetryAfter(h)
	if got < 80*time.Second || got > 95*time.Second {
		t.Fatalf("RetryAfter = %s, want ~90s", got)
	}
}

// Часть прокси отдаёт X-RateLimit-Reset как unix-время сброса.
func TestRetryAfterEpochReset(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(30*time.Second).Unix(), 10))

	got := RetryAfter(h)
	if got < 20*time.Second || got > 35*time.Second {
		t.Fatalf("RetryAfter = %s, want ~30s", got)
	}
}

func TestRetryAfterTakesLatest(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "10")
	h.Set("X-RateLimit-Reset-Tokens", "45s")

	if got := RetryAfter(h); got != 45*time.Second {
		t.Fatalf("RetryAfter = %s, want 45s", got)
	}
}

// OpenRouter кладёт в тело limit_source — по нему видно, что именно кончилось.
func TestRateLimitSourceFromBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"openrouter metadata", `{"error":{"code":429,"message":"rate limit","metadata":{"limit_source":"tokens"}}}`, "tokens"},
		{"error limit_source", `{"error":{"limit_source":"requests"}}`, "requests"},
		{"root limit_source", `{"limit_source":"credits"}`, "credits"},
		{"нет поля", `{"error":{"message":"rate limit"}}`, ""},
		{"не json", `<html>429</html>`, ""},
	}
	for _, c := range cases {
		if got := RateLimitSource([]byte(c.body)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNewRateLimitErrorText(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "42")
	h.Set("X-RateLimit-Limit-Requests", "5")
	h.Set("X-RateLimit-Remaining-Requests", "0")
	body := []byte(`{"error":{"code":429,"message":"Rate limit reached","metadata":{"limit_source":"tokens"}}}`)

	err := NewRateLimitError("OpenRouter", h, body)
	if err.Wait != 42*time.Second {
		t.Errorf("Wait = %s, want 42s", err.Wait)
	}
	if err.LimitSource != "tokens" {
		t.Errorf("LimitSource = %q, want tokens", err.LimitSource)
	}
	msg := err.Error()
	for _, want := range []string{"OpenRouter", "лимит запросов", "42 сек", "по токенам", "Rate limit reached", "осталось 0"} {
		if !strings.Contains(msg, want) {
			t.Errorf("в тексте нет %q: %s", want, msg)
		}
	}
}

func TestNewRateLimitErrorWithoutHeaders(t *testing.T) {
	err := NewRateLimitError("DeepSeek", nil, []byte(`{"error":{"message":"slow down"}}`))
	if err.Wait != 0 {
		t.Errorf("Wait = %s, want 0", err.Wait)
	}
	if !strings.Contains(err.Error(), "slow down") {
		t.Errorf("текст = %q", err.Error())
	}
}
