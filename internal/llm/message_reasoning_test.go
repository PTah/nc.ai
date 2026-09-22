package llm

import (
	"encoding/json"
	"testing"
)

// Ollama отдаёт трассу размышлений в поле "reasoning" (а не deepseek-овский
// reasoning_content) — иначе ответ thinking-модели выглядит пустым.
func TestMessageReasoningFieldFallback(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"","reasoning":"trace"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.ReasoningContent != "trace" {
		t.Fatalf("reasoning=%q want trace", m.ReasoningContent)
	}
}

func TestMessageReasoningContentWins(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","reasoning_content":"a","reasoning":"b"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.ReasoningContent != "a" {
		t.Fatalf("reasoning=%q want a", m.ReasoningContent)
	}
}

func TestMessageReasoningEmpty(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"hi"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.ReasoningContent != "" {
		t.Fatalf("reasoning=%q want empty", m.ReasoningContent)
	}
}
