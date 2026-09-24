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

const sseStreamBody = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4","usage":{"input_tokens":12,"cache_read_input_tokens":3}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"думаю"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Го"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"тово"}}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file"}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"a.go\"}"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}

event: message_stop
data: {"type":"message_stop"}

`

func TestChatCompletionStream(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path=%q", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept=%q", r.Header.Get("Accept"))
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseStreamBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk-test", "claude-sonnet-4")
	var deltas []llm.StreamDelta
	resp, err := c.ChatCompletionStream(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "system", Content: "ты агент"}, {Role: "user", Content: "привет"}},
	}, func(d llm.StreamDelta) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if gotBody["stream"] != true {
		t.Errorf("stream=%v", gotBody["stream"])
	}
	if gotBody["system"] != "ты агент" {
		t.Errorf("system=%v", gotBody["system"])
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
		t.Errorf("сообщение: %q / %q", msg.Content, msg.ReasoningContent)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "toolu_1" ||
		msg.ToolCalls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("tool_calls=%+v", msg.ToolCalls)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish=%q", resp.Choices[0].FinishReason)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 15 || resp.Usage.CompletionTokens != 7 {
		t.Errorf("usage=%+v", resp.Usage)
	}
}

func TestChatCompletionStreamErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "sk", "m").ChatCompletionStream(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "x"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("err=%v", err)
	}
}
