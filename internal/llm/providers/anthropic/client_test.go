package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"notcursor.ai/app/internal/llm"
)

func TestChatCompletionHTTP(t *testing.T) {
	var seenPath, seenKey, seenVersion string
	var seenBody apiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenKey = r.Header.Get("x-api-key")
		seenVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "id": "msg_2", "model": "claude-sonnet-4",
		  "content": [{"type":"text","text":"привет"},
		              {"type":"tool_use","id":"toolu_2","name":"read_file","input":{"path":"c.go"}}],
		  "stop_reason": "tool_use",
		  "usage": {"input_tokens": 3, "output_tokens": 4}
		}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk-test", "claude-sonnet-4")
	resp, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "привет"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if seenPath != "/v1/messages" {
		t.Errorf("path=%q", seenPath)
	}
	if seenKey != "sk-test" || seenVersion != APIVersion {
		t.Errorf("ключ=%q версия=%q", seenKey, seenVersion)
	}
	if seenBody.Model != "claude-sonnet-4" || seenBody.MaxTokens != defaultMaxTokens {
		t.Errorf("body=%+v", seenBody)
	}
	if resp.Choices[0].Message.Content != "привет" || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("ответ=%+v", resp.Choices[0].Message)
	}
	if resp.Usage.PromptTokens != 3 || resp.Usage.CompletionTokens != 4 {
		t.Errorf("usage=%+v", resp.Usage)
	}
}

func TestChatCompletionRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.Header().Set("x-ratelimit-limit-requests", "50")
		w.Header().Set("x-ratelimit-remaining-requests", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "sk", "claude-sonnet-4")
	_, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "x"}},
	})
	var rl *llm.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("want *llm.RateLimitError, got %T: %v", err, err)
	}
	if rl.Wait != 42*time.Second {
		t.Errorf("wait=%s", rl.Wait)
	}
	if !strings.Contains(rl.Detail, "осталось 0") {
		t.Errorf("detail=%q", rl.Detail)
	}
	// агент считает лимит временной ошибкой по этой фразе
	if !strings.Contains(err.Error(), "лимит запросов") {
		t.Errorf("error=%q", err.Error())
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path=%q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-4"},{"id":"claude-sonnet-4"}]}`))
	}))
	defer srv.Close()

	ids, err := New(srv.URL, "sk", "claude-sonnet-4").ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(ids) != 2 || ids[0] != "claude-opus-4" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestListModelsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "bad", "m").ListModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "неверный ключ") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("потеряли сообщение провайдера: %v", err)
	}
}

func TestNameAndDefaults(t *testing.T) {
	c := New("", "", "")
	if c.Name() != "Anthropic" {
		t.Errorf("Name=%q", c.Name())
	}
	if c.BaseURL() != DefaultBaseURL {
		t.Errorf("BaseURL=%q", c.BaseURL())
	}
	c.SetModel(" claude-haiku ")
	if c.Model() != "claude-haiku" {
		t.Errorf("Model=%q", c.Model())
	}
}
