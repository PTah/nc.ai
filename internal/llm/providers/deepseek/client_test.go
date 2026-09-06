package deepseek

import (
	"testing"

	"notcursor.ai/app/internal/llm"
)

func call(id, name string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: "{}"}}
}

func TestValidateToolHistoryOK(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("call_1", "list_dir")}},
		llm.ToolResultMessage("call_1", "ok"),
		{Role: "assistant", Content: "done"},
	}
	if err := validateToolHistory(msgs); err != nil {
		t.Fatalf("validateToolHistory = %v, want nil", err)
	}
}

func TestValidateToolHistoryMissingCallID(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("", "list_dir")}},
		llm.ToolResultMessage("", "ok"),
	}
	if err := validateToolHistory(msgs); err == nil {
		t.Fatal("missing tool_call id: want error")
	}
}

func TestValidateToolHistoryMissingCallName(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("call_1", "")}},
		llm.ToolResultMessage("call_1", "ok"),
	}
	if err := validateToolHistory(msgs); err == nil {
		t.Fatal("missing function.name: want error")
	}
}

func TestValidateToolHistoryToolMissingID(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("call_1", "list_dir")}},
		{Role: "tool", Content: "ok"},
	}
	if err := validateToolHistory(msgs); err == nil {
		t.Fatal("tool message without tool_call_id: want error")
	}
}

func TestValidateToolHistoryOrphanToolResult(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("call_1", "list_dir")}},
		llm.ToolResultMessage("call_404", "ok"),
	}
	if err := validateToolHistory(msgs); err == nil {
		t.Fatal("orphan tool result: want error")
	}
}

func TestThinkingEnabled(t *testing.T) {
	cases := []struct {
		name   string
		think  map[string]any
		expect bool
	}{
		{"nil", nil, true},
		{"enabled", map[string]any{"type": "enabled"}, true},
		{"disabled", map[string]any{"type": "disabled"}, false},
		{"empty", map[string]any{}, true},
	}
	for _, tc := range cases {
		if got := thinkingEnabled(tc.think); got != tc.expect {
			t.Fatalf("thinkingEnabled(%v) = %v, want %v", tc.name, got, tc.expect)
		}
	}
}
