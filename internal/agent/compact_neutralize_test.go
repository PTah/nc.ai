package agent

import (
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestNeutralizeToollessAssistants(t *testing.T) {
	long := strings.Repeat("Вероятно это приложение DeepSeek. ", 20)
	in := []llm.Message{
		{Role: "user", Content: "что это?"},
		{Role: "assistant", Content: long},
		{Role: "user", Content: "прочитай код"},
		{Role: "assistant", Content: "ok", ToolCalls: []llm.ToolCall{{ID: "1", Function: llm.FunctionCall{Name: "read_file"}}}},
		{Role: "assistant", Content: "short"},
	}
	out := NeutralizeToollessAssistants(in)
	if !strings.Contains(out[1].Content, "omitted") {
		t.Fatalf("long toolless assistant should be neutralized, got %q", out[1].Content)
	}
	if out[3].Content != "ok" {
		t.Fatalf("assistant with tools must stay, got %q", out[3].Content)
	}
	if out[4].Content != "short" {
		t.Fatalf("short toolless must stay, got %q", out[4].Content)
	}
}
