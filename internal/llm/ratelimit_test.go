package llm

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRetryAfter(t *testing.T) {
	cases := []struct {
		name   string
		header map[string]string
		want   time.Duration
	}{
		{"нет заголовков", nil, 0},
		{"Retry-After в секундах", map[string]string{"Retry-After": "42"}, 42 * time.Second},
		{"Retry-After мусор", map[string]string{"Retry-After": "later"}, 0},
		{"x-ratelimit-reset-requests (OpenAI)", map[string]string{"X-RateLimit-Reset-Requests": "6m0s"}, 6 * time.Minute},
		{"берём самое позднее", map[string]string{"Retry-After": "5", "X-RateLimit-Reset-Requests": "20s"}, 20 * time.Second},
		{"регистр заголовка не важен", map[string]string{"x-rpm-limit-reset": "1m30s"}, 90 * time.Second},
	}
	for _, c := range cases {
		h := http.Header{}
		for k, v := range c.header {
			h.Set(k, v)
		}
		if got := RetryAfter(h); got != c.want {
			t.Errorf("%s: RetryAfter=%s, want %s", c.name, got, c.want)
		}
	}
}

func TestRateLimitErrorText(t *testing.T) {
	err := &RateLimitError{Provider: "Local API", Detail: "Rate limit exceeded", Wait: 42 * time.Second}
	want := "Local API: лимит запросов (429), повтор через 42 сек: Rate limit exceeded."
	if err.Error() != want {
		t.Fatalf("Error()=%q, want %q", err.Error(), want)
	}
	var target *RateLimitError
	if !errors.As(error(err), &target) {
		t.Fatal("RateLimitError не распознаётся через errors.As")
	}
}

func TestFormatWait(t *testing.T) {
	cases := map[time.Duration]string{
		0:                      "0 сек",
		300 * time.Millisecond: "1 сек",
		42 * time.Second:       "42 сек",
		2 * time.Minute:        "2 мин",
		125 * time.Second:      "2 мин 5 сек",
	}
	for d, want := range cases {
		if got := FormatWait(d); got != want {
			t.Errorf("FormatWait(%s)=%q, want %q", d, got, want)
		}
	}
}

func TestRateLimitQuota(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Limit-Requests", "5")
	h.Set("X-RateLimit-Remaining-Requests", "0")
	if got := RateLimitQuota(h); got != "запросы: 5, осталось 0" {
		t.Fatalf("quota=%q", got)
	}
	if got := RateLimitQuota(http.Header{}); got != "" {
		t.Fatalf("пустые заголовки: %q", got)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"error":{"message":"Requests rate limit exceeded"}}`, "Requests rate limit exceeded"},
		{`{"error":"  oversubscribed  "}`, "oversubscribed"},
		{`{"message":"quota"}`, "quota"},
		{`{"detail":"nope"}`, ""},
	}
	for _, c := range cases {
		if got := APIErrorMessage([]byte(c.body)); got != c.want {
			t.Errorf("APIErrorMessage(%s)=%q, want %q", c.body, got, c.want)
		}
	}
}
