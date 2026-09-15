package local

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestConsumeOpenAISSEContent(t *testing.T) {
	raw := "" +
		"data: {\"id\":\"1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"При\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"вет\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	var parts []string
	out, err := consumeOpenAISSE(strings.NewReader(raw), func(d llm.StreamDelta) {
		if d.Content != "" {
			parts = append(parts, d.Content)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Choices[0].Message.Content != "Привет" {
		t.Fatalf("content=%q", out.Choices[0].Message.Content)
	}
	if out.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish=%q", out.Choices[0].FinishReason)
	}
	if strings.Join(parts, "") != "Привет" {
		t.Fatalf("parts=%v", parts)
	}
}

func TestConsumeOpenAISSEToolCalls(t *testing.T) {
	raw := "" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"git_status\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	out, err := consumeOpenAISSE(strings.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := out.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].Function.Name != "git_status" {
		t.Fatalf("calls=%+v", calls)
	}
	if calls[0].Function.Arguments != "{}" {
		t.Fatalf("args=%q", calls[0].Function.Arguments)
	}
	if out.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish=%q", out.Choices[0].FinishReason)
	}
}

func TestChatCompletionStreamHTTP(t *testing.T) {
	var sawStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sawStream = strings.Contains(string(raw), `"stream":true`)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"model\":\"m1\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "m1")
	c.http = srv.Client()
	var got string
	out, err := c.ChatCompletionStream(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	}, func(d llm.StreamDelta) { got += d.Content })
	if err != nil {
		t.Fatal(err)
	}
	if !sawStream {
		t.Fatal("expected stream:true in request body")
	}
	if got != "ok" || out.Choices[0].Message.Content != "ok" {
		t.Fatalf("got=%q out=%q", got, out.Choices[0].Message.Content)
	}
}
