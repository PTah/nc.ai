package kimi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestChatCompletionK31SendsReasoningEffort(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "c1",
			"model": DefaultModel,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "2",
				},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(srv.Close)

	c := NewWithBaseURL("sk", DefaultModel, srv.URL)
	_, err := c.ChatCompletion(context.Background(), &llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "1+1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["model"] != DefaultModel {
		t.Errorf("model=%v", got["model"])
	}
	if got["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort=%v", got["reasoning_effort"])
	}
	if _, ok := got["thinking"]; ok {
		t.Errorf("thinking must not be sent for K3.1: %v", got["thinking"])
	}
	if _, ok := got["temperature"]; ok {
		t.Errorf("temperature must not be sent for K3.1: %v", got["temperature"])
	}
}

func TestPreferModel(t *testing.T) {
	ids := []string{FastModel, DefaultModel, "kimi-k3"}
	if got := PreferModel(ids, "kimi-k3"); got != "kimi-k3" {
		t.Errorf("keep current: %s", got)
	}
	if got := PreferModel(ids, "gone"); got != DefaultModel {
		t.Errorf("fallback default: %s", got)
	}
}

func TestOrderModels(t *testing.T) {
	got := OrderModels([]string{"kimi-k2.6", FastModel, DefaultModel})
	if len(got) < 2 || got[0] != DefaultModel || got[1] != FastModel {
		t.Fatalf("%v", got)
	}
}
