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

// Ollama в OpenAI-совместимом режиме отдаёт рассуждения как delta.reasoning,
// DeepSeek — как reasoning_content. Ранее поле reasoning игнорировалось, и ответ
// thinking-модели выглядел пустым.
func TestConsumeOpenAISSEThinkingField(t *testing.T) {
	raw := "" +
		"data: {\"choices\":[{\"delta\":{\"reasoning\":\"думаю\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"ответ\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	var deltas []string
	out, err := consumeOpenAISSE(strings.NewReader(raw), func(d llm.StreamDelta) {
		if d.ReasoningContent != "" {
			deltas = append(deltas, d.ReasoningContent)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Choices[0].Message.ReasoningContent != "думаю" {
		t.Fatalf("reasoning=%q", out.Choices[0].Message.ReasoningContent)
	}
	if strings.Join(deltas, "") != "думаю" {
		t.Fatalf("deltas=%v", deltas)
	}
	if out.Choices[0].Message.Content != "ответ" {
		t.Fatalf("content=%q", out.Choices[0].Message.Content)
	}
}

func TestReasoningContentWinsOverReasoning(t *testing.T) {
	raw := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"a\",\"reasoning\":\"b\"},\"finish_reason\":\"stop\"}]}\n\n"
	out, err := consumeOpenAISSE(strings.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Choices[0].Message.ReasoningContent; got != "a" {
		t.Fatalf("reasoning=%q want a", got)
	}
}

func TestNumCtxNotSentToServer(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "m1")
	c.http = srv.Client()
	// Контекст нужен приложению для индикатора и предупреждений, но в запрос
	// уходить не должен: строгие OpenAI-серверы (Lemonade, vLLM, LM Studio)
	// нестандартные поля могут отвергнуть с 400.
	c.SetNumCtx(8192)
	if _, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["options"]; ok {
		t.Fatalf("options must not be sent: %v", body["options"])
	}
	if _, ok := body["num_ctx"]; ok {
		t.Fatalf("num_ctx must not be sent: %v", body["num_ctx"])
	}
	if c.NumCtx() != 8192 {
		t.Fatalf("numCtx=%d want 8192", c.NumCtx())
	}
}

func TestNumCtxClampsNegative(t *testing.T) {
	c := New("http://127.0.0.1:11434/v1", "", "m")
	c.SetNumCtx(-5)
	if c.NumCtx() != 0 {
		t.Fatalf("numCtx=%d want 0", c.NumCtx())
	}
}
