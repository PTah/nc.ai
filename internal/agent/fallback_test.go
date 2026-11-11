// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:50:50
// Created by nc.ai 1.1.17 on macbook 2026-11-11
package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"notcursor.ai/app/internal/llm"
)

// Основной провайдер исчерпал суточный лимит — прогон должен один раз уйти на
// резервный и там ответить, а не падать.
func TestChatFallsBackOnDailyRateLimit(t *testing.T) {
	primary := &fakeProvider{errs: []error{&llm.RateLimitError{
		Provider:    "OpenRouter",
		Detail:      "free-models-per-day exceeded",
		Wait:        6 * time.Hour,
		LimitSource: "requests",
	}}}
	backup := &fakeProvider{}
	r := &Runner{
		Provider:         primary,
		ProviderID:       "openrouter",
		FallbackProvider: backup,
		FallbackID:       "deepseek",
		FallbackModel:    "deepseek-v4-flash",
		RetryBackoff:     time.Millisecond,
	}
	var notices []Event
	var req = &llm.ChatRequest{}
	resp, _, err := r.chat(context.Background(), req, rateLimitNotices(&notices))
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("ответ не пришёл с резерва: %+v", resp)
	}
	if primary.calls != 1 {
		t.Fatalf("основной вызван %d раз, want 1", primary.calls)
	}
	if backup.calls != 1 {
		t.Fatalf("резервный вызван %d раз, want 1", backup.calls)
	}
	if r.Provider != llm.Provider(backup) {
		t.Fatal("Runner.Provider должен переключиться на резерв")
	}
	if r.ProviderID != "deepseek" {
		t.Fatalf("ProviderID = %q, want deepseek", r.ProviderID)
	}
	if req.Model != "deepseek-v4-flash" {
		t.Fatalf("Model = %q, want deepseek-v4-flash", req.Model)
	}
	if len(notices) != 1 || !strings.Contains(notices[0].Content, "резервном") {
		t.Fatalf("notices=%v", notices)
	}
}

// Короткий лимит выгоднее переждать: провайдера не меняем.
func TestChatKeepsPrimaryOnShortRateLimit(t *testing.T) {
	primary := &fakeProvider{errs: []error{&llm.RateLimitError{
		Provider: "DeepSeek", Wait: 30 * time.Millisecond,
	}}}
	backup := &fakeProvider{}
	r := &Runner{
		Provider:         primary,
		ProviderID:       "deepseek",
		FallbackProvider: backup,
		FallbackID:       "openrouter",
		RetryBackoff:     time.Millisecond,
	}
	if _, _, err := r.chat(context.Background(), &llm.ChatRequest{}, func(Event) {}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if backup.calls != 0 {
		t.Fatalf("резерв вызван %d раз, want 0", backup.calls)
	}
	if r.ProviderID != "deepseek" {
		t.Fatalf("ProviderID = %q, want deepseek", r.ProviderID)
	}
}

// Перегрузка провайдера — тоже повод перейти на резерв.
func TestChatFallsBackOnOverload(t *testing.T) {
	primary := &fakeProvider{errs: []error{errors.New("Anthropic: 529 Overloaded")}}
	backup := &fakeProvider{}
	r := &Runner{
		Provider:         primary,
		ProviderID:       "anthropic",
		FallbackProvider: backup,
		FallbackID:       "deepseek",
		RetryBackoff:     time.Millisecond,
	}
	if _, _, err := r.chat(context.Background(), &llm.ChatRequest{}, func(Event) {}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if backup.calls != 1 {
		t.Fatalf("резерв вызван %d раз, want 1", backup.calls)
	}
	if !r.fallbackUsed {
		t.Fatal("флаг fallbackUsed должен быть выставлен")
	}
}

// Резерв тоже может отказать: второй фолбэк не делаем, отдаём ошибку.
func TestChatFallbackOnlyOnce(t *testing.T) {
	primary := &fakeProvider{errs: []error{&llm.RateLimitError{Provider: "OpenRouter", Wait: 24 * time.Hour}}}
	backup := &fakeProvider{errs: []error{&llm.RateLimitError{Provider: "DeepSeek", Wait: 24 * time.Hour}}}
	r := &Runner{
		Provider:         primary,
		ProviderID:       "openrouter",
		FallbackProvider: backup,
		FallbackID:       "deepseek",
		RetryBackoff:     time.Millisecond,
	}
	_, _, err := r.chat(context.Background(), &llm.ChatRequest{}, func(Event) {})
	if err == nil {
		t.Fatal("ожидали ошибку после отказа резерва")
	}
	if backup.calls != 1 {
		t.Fatalf("резерв вызван %d раз, want 1 (без цикла)", backup.calls)
	}
}

// Без настроенного резерва поведение прежнее: ошибка сразу.
func TestChatWithoutFallbackReturnsRateLimit(t *testing.T) {
	primary := &fakeProvider{errs: []error{&llm.RateLimitError{Provider: "DeepSeek", Wait: 24 * time.Hour}}}
	r := &Runner{Provider: primary, ProviderID: "deepseek", RetryBackoff: time.Millisecond}
	_, _, err := r.chat(context.Background(), &llm.ChatRequest{}, func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "лимит") {
		t.Fatalf("err = %v", err)
	}
}
