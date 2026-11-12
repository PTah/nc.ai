// Created by nc.ai 1.1.17 on new-admin-pc 2026-11-12 09:52:10
//go:build live

// Живые проверки резервного провайдера (roadmap п.2). Запуск:
//
//	go test -tags live ./internal/agent -run Live -v -timeout 180s
//
// Ключи берутся из настоящих настроек приложения; чего нет — тест пропускается.
package agent

import (
	"context"
	"testing"
	"time"

	"notcursor.ai/app/internal/config"
	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/llm/providers/deepseek"
	"notcursor.ai/app/internal/llm/providers/openrouter"
)

func liveStore(t *testing.T) *config.Store {
	t.Helper()
	st := config.NewStore()
	if err := st.Load(); err != nil {
		t.Fatalf("Load настроек: %v", err)
	}
	return st
}

// Оба провайдера живы: free-tier OpenRouter и DeepSeek отвечают на короткий запрос.
func TestLiveProvidersAnswer(t *testing.T) {
	st := liveStore(t)
	cases := []struct {
		name     string
		key      string
		provider llm.Provider
	}{
		{"openrouter", st.APIKey(config.ProviderOpenRouter), openrouter.New(st.APIKey(config.ProviderOpenRouter), st.Get().OpenRouterModel)},
		{"deepseek", st.APIKey(config.ProviderDeepSeek), deepseek.New(st.APIKey(config.ProviderDeepSeek), st.Get().DeepSeekModel)},
	}
	for _, c := range cases {
		if c.key == "" {
			t.Logf("%s: ключа нет в настройках — пропуск", c.name)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		maxTokens := 16
		resp, err := c.provider.ChatCompletion(ctx, &llm.ChatRequest{
			Messages:  []llm.Message{{Role: "user", Content: "Ответь одним словом: ok"}},
			MaxTokens: &maxTokens,
			Thinking:  map[string]any{"type": "disabled"},
		})
		cancel()
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		got := ""
		if len(resp.Choices) > 0 {
			got = resp.Choices[0].Message.Content
		}
		t.Logf("%s: model=%s ответ=%q", c.name, resp.Model, got)
	}
}

// Длинный лимит у основного провайдера уводит прогон на реальный DeepSeek из
// настроек и там приходит настоящий ответ.
func TestLiveFallbackSwitchToDeepSeek(t *testing.T) {
	st := liveStore(t)
	key := st.APIKey(config.ProviderDeepSeek)
	if key == "" {
		t.Skip("ключ DeepSeek не задан — проверять нечего")
	}
	fbID := st.FallbackProvider()
	if fbID == "" {
		t.Log("в настройках резерв не включён — проверяем DeepSeek напрямую как резерв")
		fbID = config.ProviderDeepSeek
	}
	fbModel := st.FallbackModel()
	if fbID != config.ProviderDeepSeek {
		t.Skipf("резерв в настройках — %q, а живая проверка описана для deepseek", fbID)
	}

	primary := &fakeProvider{errs: []error{&llm.RateLimitError{
		Provider: "OpenRouter", Detail: "free-models-per-day exceeded",
		Wait: 6 * time.Hour, LimitSource: "requests",
	}}}
	r := &Runner{
		Provider:         primary,
		ProviderID:       config.ProviderOpenRouter,
		FallbackProvider: deepseek.New(key, fbModel),
		FallbackID:       config.ProviderDeepSeek,
		FallbackModel:    fbModel,
		RetryBackoff:     time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	maxTokens := 24
	req := &llm.ChatRequest{
		Messages:  []llm.Message{{Role: "user", Content: "Ответь одним словом: ok"}},
		MaxTokens: &maxTokens,
		Thinking:  map[string]any{"type": "disabled"},
	}
	var notices []Event
	resp, _, err := r.chat(ctx, req, rateLimitNotices(&notices))
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if primary.calls != 1 {
		t.Fatalf("основной вызван %d раз, want 1", primary.calls)
	}
	if r.ProviderID != config.ProviderDeepSeek {
		t.Fatalf("резерв не включился: ProviderID=%q", r.ProviderID)
	}
	got := ""
	if len(resp.Choices) > 0 {
		got = resp.Choices[0].Message.Content
	}
	if got == "" {
		t.Fatalf("пустой ответ с резерва: %+v", resp)
	}
	t.Logf("резерв DeepSeek (%s) ответил: %q, notices=%d", resp.Model, got, len(notices))
}
