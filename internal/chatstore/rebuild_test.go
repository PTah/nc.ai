// Created by nc.ai 1.2.0 on new-admin-pc 2026-11-12 11:29:46
package chatstore

import (
	"encoding/json"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestItemsFromHistoryKeepsReplies(t *testing.T) {
	history := []llm.Message{
		{Role: "system", Content: "правила"},
		{Role: "user", Content: "задание из нескольких пунктов"},
		{Role: "assistant", Content: "делаю", ReasoningContent: "думаю"},
		{Role: "tool", ToolCallID: "t1", Content: "вывод инструмента"},
		{Role: "assistant", Content: "  "},
		{Role: "user", Content: "продолжай"},
	}
	raw := ItemsFromHistory(history)
	var items []RebuiltItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatalf("json: %v (%s)", err, raw)
	}
	if len(items) != 3 {
		t.Fatalf("элементов %d, want 3: %s", len(items), raw)
	}
	if items[0].Kind != "user" || items[0].Content != "задание из нескольких пунктов" {
		t.Fatalf("первый элемент: %+v", items[0])
	}
	if items[1].Kind != "assistant" || items[1].Content != "делаю" {
		t.Fatalf("второй элемент: %+v", items[1])
	}
	if items[2].Content != "продолжай" {
		t.Fatalf("третий элемент: %+v", items[2])
	}
}

func TestItemsFromHistoryEmpty(t *testing.T) {
	if got := ItemsFromHistory(nil); got != "[]" {
		t.Fatalf("пустая история: %q", got)
	}
	if got := ItemsFromHistory([]llm.Message{{Role: "tool", Content: "x"}, {Role: "system", Content: "y"}}); got != "[]" {
		t.Fatalf("без реплик: %q", got)
	}
}
