package local

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"notcursor.ai/app/internal/llm"
)

// reasoning_effort уходит только когда его включили в профиле эндпоинта:
// Ollama/vLLM/LM Studio на незнакомое поле отвечают 400.
func TestReasoningEffortOptIn(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	call := func(effort string) map[string]any {
		body = nil
		c := New(srv.URL, "", "m")
		c.SetReasoningEffort(effort)
		if _, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
			Messages: []llm.Message{{Role: "user", Content: "привет"}},
		}); err != nil {
			t.Fatalf("ChatCompletion: %v", err)
		}
		return body
	}

	if got := call(""); true {
		if _, ok := got["reasoning_effort"]; ok {
			t.Errorf("reasoning_effort ушёл без включённой опции: %v", got["reasoning_effort"])
		}
	}
	if got := call("high"); got["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort=%v, want high", got["reasoning_effort"])
	}
	// thinking не отправляем никогда: его формат у каждого сервера свой.
	if got := call("high"); got["thinking"] != nil {
		t.Errorf("thinking не должен уходить: %v", got["thinking"])
	}
}
