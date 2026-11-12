// Created by nc.ai 1.1.18 on new-admin-pc 2026-11-12 10:26:29
package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

// Подпись thinking-блока должна вернуться в запрос байт-в-байт, а сам запрос —
// включить extended thinking и потерять temperature (API иначе отвечает 400).
func TestThinkingSignatureRoundTrip(t *testing.T) {
	temp := 0.2
	maxTok := 2048
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "посмотри файл"},
			{
				Role:             "assistant",
				Content:          "зову инструмент",
				ReasoningContent: "сначала прочитаю файл",
				Thinking: []llm.ThinkingBlock{{
					Thinking:  "сначала прочитаю файл",
					Signature: "EqQBCkgIBRABGAIiQL+signature==",
				}},
				ToolCalls: []llm.ToolCall{{
					ID: "toolu_1", Type: "function",
					Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`},
				}},
			},
			{Role: "tool", ToolCallID: "toolu_1", Content: "package a"},
		},
		Thinking:    map[string]any{"type": "enabled"},
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}
	out, err := buildRequest(req, "claude-sonnet-4")
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if out.Temperature != nil {
		t.Errorf("temperature должна быть снята при thinking: %v", *out.Temperature)
	}
	if out.Thinking == nil || out.Thinking["type"] != "enabled" {
		t.Fatalf("thinking не включён: %+v", out.Thinking)
	}
	if budget, _ := out.Thinking["budget_tokens"].(int); budget <= 0 || budget >= out.MaxTokens {
		t.Fatalf("budget_tokens=%v при max_tokens=%d", out.Thinking["budget_tokens"], out.MaxTokens)
	}
	var assistant *apiMessage
	for i := range out.Messages {
		if out.Messages[i].Role == "assistant" {
			assistant = &out.Messages[i]
		}
	}
	if assistant == nil || len(assistant.Content) == 0 {
		t.Fatalf("assistant-turn не собран: %+v", out.Messages)
	}
	first := assistant.Content[0]
	if first.Type != "thinking" || first.Signature != "EqQBCkgIBRABGAIiQL+signature==" || first.Thinking == "" {
		t.Fatalf("thinking-блок не переслан: %+v", first)
	}
	if assistant.Content[1].Type != "text" {
		t.Fatalf("порядок блоков: %+v", assistant.Content)
	}
}

// Без запрошенного thinking блоки не отправляем: API их не примет.
func TestThinkingBlocksDroppedWhenThinkingDisabled(t *testing.T) {
	maxTok := 512
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "раз"},
			{Role: "assistant", Content: "два", Thinking: []llm.ThinkingBlock{{
				Thinking: "думал", Signature: "sig",
			}}},
			{Role: "user", Content: "три"},
		},
		Thinking:  map[string]any{"type": "disabled"},
		MaxTokens: &maxTok,
	}
	out, err := buildRequest(req, "claude-sonnet-4")
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if out.Thinking != nil {
		t.Fatalf("thinking должен быть выключен: %+v", out.Thinking)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "signature") {
		t.Fatalf("подпись утекла в запрос: %s", raw)
	}
}

// Модель без extended thinking и чужие anthropic-совместимые эндпоинты не
// получают thinking, даже если агент его просит, — иначе 400.
func TestThinkingGatedByModelSupport(t *testing.T) {
	maxTok := 4096
	req := &llm.ChatRequest{
		Messages:  []llm.Message{{Role: "user", Content: "привет"}},
		Thinking:  map[string]any{"type": "enabled"},
		MaxTokens: &maxTok,
	}
	for _, m := range []string{"claude-3-opus-20240229", "claude-2.1", "deepseek-chat"} {
		out, err := buildRequest(req, m)
		if err != nil {
			t.Fatalf("buildRequest(%s): %v", m, err)
		}
		if out.Thinking != nil {
			t.Fatalf("модель %s не умеет thinking, а он ушёл: %+v", m, out.Thinking)
		}
	}
	out, err := buildRequest(req, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatal(err)
	}
	if out.Thinking == nil {
		t.Fatal("sonnet-4 должен получить thinking")
	}
}

// Бюджет меньше 1024 запрещён API — зажимаем до минимума.
func TestThinkingBudgetClamped(t *testing.T) {
	maxTok := 3000
	req := &llm.ChatRequest{
		Messages:  []llm.Message{{Role: "user", Content: "привет"}},
		Thinking:  map[string]any{"type": "enabled", "budget_tokens": 128},
		MaxTokens: &maxTok,
	}
	out, err := buildRequest(req, "claude-sonnet-4")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := out.Thinking["budget_tokens"].(int)
	if !ok || b < 1024 {
		t.Fatalf("budget_tokens=%v, want >=1024", out.Thinking["budget_tokens"])
	}
	if out.MaxTokens <= b {
		t.Fatalf("max_tokens=%d не больше бюджета %d", out.MaxTokens, b)
	}
}

func TestParseResponseKeepsSignature(t *testing.T) {
	resp := &apiResponse{
		ID:    "msg_1",
		Model: "claude-sonnet-4",
		Content: []apiBlock{
			{Type: "thinking", Thinking: "думаю", Signature: "sig-1"},
			{Type: "text", Text: "готово"},
		},
		StopReason: "end_turn",
	}
	out := parseResponse(resp)
	msg := out.Choices[0].Message
	if msg.ReasoningContent != "думаю" {
		t.Fatalf("reasoning_content=%q", msg.ReasoningContent)
	}
	if len(msg.Thinking) != 1 || msg.Thinking[0].Signature != "sig-1" || msg.Thinking[0].Thinking != "думаю" {
		t.Fatalf("thinking=%+v", msg.Thinking)
	}
	if msg.Content != "готово" {
		t.Fatalf("content=%q", msg.Content)
	}
}

// Потоковый ответ: thinking_delta + signature_delta → подпись в сообщении,
// иначе следующий шаг tool-loop отвалится с 400.
func TestStreamKeepsSignature(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"думаю"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-9"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"готово"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk", "claude-sonnet-4")
	resp, err := c.ChatCompletionStream(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "привет"}},
	}, nil)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	msg := resp.Choices[0].Message
	if len(msg.Thinking) != 1 || msg.Thinking[0].Signature != "sig-9" || msg.Thinking[0].Thinking != "думаю" {
		t.Fatalf("thinking=%+v", msg.Thinking)
	}
	if msg.Content != "готово" {
		t.Fatalf("content=%q", msg.Content)
	}
}
