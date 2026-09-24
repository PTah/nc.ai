package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

const sseStreamBody = `data: {"id":"c1","model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"думаю"},"finish_reason":""}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"content":"Го"},"finish_reason":""}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"content":"тово"},"finish_reason":""}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":"}}]},"finish_reason":""}]}

data: {"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}

data: {"id":"c1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]

`

func TestChatCompletionStream(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path=%q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseStreamBody))
	}))
	defer srv.Close()

	c := New("sk-test", "deepseek-v4-flash")
	c.baseURL = srv.URL

	var deltas []llm.StreamDelta
	resp, err := c.ChatCompletionStream(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "привет"}},
	}, func(d llm.StreamDelta) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if gotBody["stream"] != true {
		t.Errorf("stream=%v", gotBody["stream"])
	}
	opts, _ := gotBody["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Errorf("stream_options=%v — usage не запросили", gotBody["stream_options"])
	}
	var text, think strings.Builder
	for _, d := range deltas {
		text.WriteString(d.Content)
		think.WriteString(d.ReasoningContent)
	}
	if text.String() != "Готово" || think.String() != "думаю" {
		t.Fatalf("дельты: text=%q think=%q", text.String(), think.String())
	}
	msg := resp.Choices[0].Message
	if msg.Content != "Готово" || msg.ReasoningContent != "думаю" {
		t.Errorf("собранное сообщение: %q / %q", msg.Content, msg.ReasoningContent)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "read_file" ||
		msg.ToolCalls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("tool_calls=%+v", msg.ToolCalls)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish=%q", resp.Choices[0].FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Errorf("usage=%+v", resp.Usage)
	}
}

// Если эндпоинт не понял stream_options, откатываемся на обычный запрос.
func TestChatCompletionStreamFallsBack(t *testing.T) {
	var streamCalls, plainCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			streamCalls++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"stream_options is not supported"}}`))
			return
		}
		plainCalls++
		_, _ = w.Write([]byte(`{"id":"c2","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := New("sk-test", "deepseek-v4-flash")
	c.baseURL = srv.URL

	deltas := 0
	resp, err := c.ChatCompletionStream(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "привет"}},
	}, func(llm.StreamDelta) { deltas++ })
	if err != nil {
		t.Fatalf("stream с откатом: %v", err)
	}
	if streamCalls != 1 || plainCalls != 1 {
		t.Fatalf("stream=%d plain=%d", streamCalls, plainCalls)
	}
	if deltas != 0 {
		t.Errorf("дельты при откате не ожидались: %d", deltas)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("ответ=%q", resp.Choices[0].Message.Content)
	}
}

// Ошибка потока маппится так же, как у обычного запроса (лимит — человеческим текстом).
func TestChatCompletionStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	defer srv.Close()

	c := New("sk-test", "deepseek-v4-flash")
	c.baseURL = srv.URL
	_, err := c.ChatCompletionStream(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "привет"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "лимит запросов") {
		t.Fatalf("err=%v", err)
	}
}
