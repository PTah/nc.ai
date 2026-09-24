package local

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"notcursor.ai/app/internal/llm"
)

func TestMapAPIErrorRateLimit(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "42")
	h.Set("X-RateLimit-Limit-Requests", "5")
	h.Set("X-RateLimit-Remaining-Requests", "0")

	err := mapAPIError(429, []byte(`{"error":{"message":"Requests rate limit exceeded"}}`), h)
	var rl *llm.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("want *llm.RateLimitError, got %T: %v", err, err)
	}
	if rl.Wait != 42*time.Second {
		t.Errorf("Wait=%s, want 42s", rl.Wait)
	}
	if !strings.Contains(rl.Detail, "Requests rate limit exceeded") || !strings.Contains(rl.Detail, "осталось 0") {
		t.Errorf("Detail=%q", rl.Detail)
	}
	// isTransientError агента ищет эту фразу, чтобы повторить запрос.
	if !strings.Contains(err.Error(), "лимит запросов") {
		t.Errorf("Error()=%q — агент не распознает лимит как временную ошибку", err.Error())
	}
}

func TestMapAPIErrorWithoutHeaders(t *testing.T) {
	err := mapAPIError(429, nil, http.Header{})
	var rl *llm.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("want *llm.RateLimitError, got %T: %v", err, err)
	}
	if rl.Wait != 0 {
		t.Errorf("Wait=%s, want 0", rl.Wait)
	}
	if !strings.Contains(err.Error(), "лимит запросов") {
		t.Errorf("Error()=%q", err.Error())
	}
}

func TestMapAPIErrorOtherStatusesUnchanged(t *testing.T) {
	if got := mapAPIError(401, nil, http.Header{}).Error(); !strings.Contains(got, "неверный токен") {
		t.Errorf("401: %q", got)
	}
	if got := mapAPIError(503, nil, http.Header{}).Error(); !strings.Contains(got, "временно недоступен") {
		t.Errorf("503: %q", got)
	}
}
