package local

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestNormalizeBaseURL(t *testing.T) {
	if got := NormalizeBaseURL(""); got != DefaultBaseURL {
		t.Fatalf("empty: got %q", got)
	}
	if got := NormalizeBaseURL("127.0.0.1:1234/v1/"); got != "http://127.0.0.1:1234/v1" {
		t.Fatalf("no scheme: got %q", got)
	}
	if got := NormalizeBaseURL("http://host:11434/v1/"); got != "http://host:11434/v1" {
		t.Fatalf("trim slash: got %q", got)
	}
}

func TestOllamaOrigin(t *testing.T) {
	got, err := ollamaOrigin("http://127.0.0.1:11434/v1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:11434" {
		t.Fatalf("got %q", got)
	}
}

func TestChatCompletionOptionalKey(t *testing.T) {
	var sawAuth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "m1")
	c.http = srv.Client()
	out, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
		Thinking:        map[string]any{"type": "enabled"},
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Choices[0].Message.Content != "hi" {
		t.Fatalf("content=%q", out.Choices[0].Message.Content)
	}
	if sawAuth != "" {
		t.Fatalf("expected no Authorization, got %q", sawAuth)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatal("thinking must not be sent")
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort must not be sent")
	}
}

func TestChatCompletionWithBearer(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "tok-secret", "m1")
	c.http = srv.Client()
	if _, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "x"}},
	}); err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer tok-secret" {
		t.Fatalf("auth=%q", sawAuth)
	}
}

func TestListModelsOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"b-model"},{"id":"a-model"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "")
	c.http = srv.Client()
	ids, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "a-model" || ids[1] != "b-model" {
		t.Fatalf("got %#v", ids)
	}
}

func TestListModelsOllamaFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			http.Error(w, "nope", http.StatusNotFound)
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5:7b"},{"name":"llama3"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "")
	c.http = srv.Client()
	ids, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, "llama3") || !strings.Contains(joined, "qwen2.5:7b") {
		t.Fatalf("got %#v", ids)
	}
}

func TestChatRequiresModel(t *testing.T) {
	c := New("http://127.0.0.1:9/v1", "", "")
	_, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "model is empty") {
		t.Fatalf("got %v", err)
	}
}
