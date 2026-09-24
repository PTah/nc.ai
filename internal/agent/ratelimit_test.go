package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"notcursor.ai/app/internal/llm"
)

// fakeProvider отдаёт подготовленные ошибки, затем — простой ответ.
type fakeProvider struct {
	errs  []error
	calls int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) ChatCompletion(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	f.calls++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return nil, err
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{
		Message:      llm.Message{Role: "assistant", Content: "ok"},
		FinishReason: "stop",
	}}}, nil
}

func rateLimitNotices(events *[]Event) EmitFunc {
	return func(e Event) {
		if e.Type == "notice" {
			*events = append(*events, e)
		}
	}
}

func TestChatWaitsForProviderRateLimit(t *testing.T) {
	p := &fakeProvider{errs: []error{&llm.RateLimitError{
		Provider: "Local API",
		Detail:   "Requests rate limit exceeded",
		Wait:     30 * time.Millisecond,
	}}}
	r := &Runner{Provider: p, RetryBackoff: time.Millisecond}
	var notices []Event
	resp, _, err := r.chat(context.Background(), &llm.ChatRequest{}, rateLimitNotices(&notices))
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp == nil || p.calls != 2 {
		t.Fatalf("calls=%d, want 2 (повтор после лимита)", p.calls)
	}
	if len(notices) != 1 || !strings.Contains(notices[0].Content, "ограничил частоту") {
		t.Fatalf("notices=%v", notices)
	}
	if !strings.Contains(notices[0].Content, "1 сек") {
		t.Fatalf("в уведомлении нет ожидания: %q", notices[0].Content)
	}
}

func TestChatDoesNotWaitForDailyRateLimit(t *testing.T) {
	rl := &llm.RateLimitError{Provider: "Local API", Wait: maxRateLimitWait + time.Minute}
	p := &fakeProvider{errs: []error{rl}}
	r := &Runner{Provider: p, RetryBackoff: time.Millisecond}
	var notices []Event
	_, _, err := r.chat(context.Background(), &llm.ChatRequest{}, rateLimitNotices(&notices))
	if err == nil {
		t.Fatal("ожидали ошибку лимита без повтора")
	}
	var got *llm.RateLimitError
	if !errors.As(err, &got) {
		t.Fatalf("want *llm.RateLimitError, got %T: %v", err, err)
	}
	if p.calls != 1 {
		t.Fatalf("calls=%d, want 1 (суточный лимит не ждём)", p.calls)
	}
	if len(notices) != 0 {
		t.Fatalf("notices=%v", notices)
	}
}
