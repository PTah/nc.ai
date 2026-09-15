package llm

import (
	"strings"
	"testing"
)

func TestPromoteTextToolCallsSingle(t *testing.T) {
	known := map[string]bool{"ask_user": true, "git_status": true}
	msg := Message{Role: "assistant", Content: `{"name": "ask_user", "arguments": {"question": "Проверка?", "options": ["A", "B"]}}`}
	if !PromoteTextToolCalls(&msg, known) {
		t.Fatal("expected promote")
	}
	if msg.Content != "" {
		t.Fatalf("content should be cleared, got %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("calls=%d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.Function.Name != "ask_user" {
		t.Fatalf("name=%q", tc.Function.Name)
	}
	if tc.ID == "" || tc.Type != "function" {
		t.Fatalf("id/type=%q/%q", tc.ID, tc.Type)
	}
	if !strings.Contains(tc.Function.Arguments, "Проверка") {
		t.Fatalf("args=%s", tc.Function.Arguments)
	}
}

func TestPromoteTextToolCallsMultiple(t *testing.T) {
	known := map[string]bool{"git_status": true, "git_diff": true, "git_log": true}
	msg := Message{Role: "assistant", Content: `{"name": "git_status", "arguments": {}} {"name": "git_diff", "arguments": {}} {"name": "git_log", "arguments": {"n": 5}}`}
	if !PromoteTextToolCalls(&msg, known) {
		t.Fatal("expected promote")
	}
	if len(msg.ToolCalls) != 3 {
		t.Fatalf("calls=%d %+v", len(msg.ToolCalls), msg.ToolCalls)
	}
	if msg.ToolCalls[2].Function.Name != "git_log" {
		t.Fatalf("third=%q", msg.ToolCalls[2].Function.Name)
	}
	if !strings.Contains(msg.ToolCalls[2].Function.Arguments, `"n"`) {
		t.Fatalf("args=%s", msg.ToolCalls[2].Function.Arguments)
	}
}

func TestPromoteTextToolCallsArray(t *testing.T) {
	known := map[string]bool{"git_status": true, "git_diff": true}
	msg := Message{Role: "assistant", Content: `[{"name":"git_status","arguments":{}},{"name":"git_diff","arguments":{}}]`}
	if !PromoteTextToolCalls(&msg, known) {
		t.Fatal("expected promote")
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("calls=%d", len(msg.ToolCalls))
	}
}

func TestPromoteTextToolCallsCodeFence(t *testing.T) {
	known := map[string]bool{"git_status": true}
	msg := Message{Role: "assistant", Content: "```json\n{\"name\":\"git_status\",\"arguments\":{}}\n```"}
	if !PromoteTextToolCalls(&msg, known) {
		t.Fatal("expected promote")
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "git_status" {
		t.Fatalf("%+v", msg.ToolCalls)
	}
}

func TestPromoteTextToolCallsOpenAIShape(t *testing.T) {
	known := map[string]bool{"read_file": true}
	msg := Message{Role: "assistant", Content: `{"id":"x1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.go\"}"}}`}
	if !PromoteTextToolCalls(&msg, known) {
		t.Fatal("expected promote")
	}
	if msg.ToolCalls[0].ID != "x1" || msg.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("%+v", msg.ToolCalls[0])
	}
	if msg.ToolCalls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("args=%s", msg.ToolCalls[0].Function.Arguments)
	}
}

func TestPromoteTextToolCallsRejectsUnknown(t *testing.T) {
	known := map[string]bool{"git_status": true}
	msg := Message{Role: "assistant", Content: `{"name":"not_a_tool","arguments":{}}`}
	if PromoteTextToolCalls(&msg, known) {
		t.Fatal("should reject unknown tool")
	}
	if msg.Content == "" {
		t.Fatal("content should remain")
	}
}

func TestPromoteTextToolCallsRejectsProse(t *testing.T) {
	known := map[string]bool{"git_status": true}
	msg := Message{Role: "assistant", Content: `Вот статус репозитория. Пример: {"name":"git_status","arguments":{}} и дальше текст.`}
	if PromoteTextToolCalls(&msg, known) {
		t.Fatal("should not promote prose+json")
	}
}

func TestPromoteTextToolCallsSkipsIfAlreadyStructured(t *testing.T) {
	known := map[string]bool{"git_status": true}
	msg := Message{
		Role:    "assistant",
		Content: `{"name":"git_status","arguments":{}}`,
		ToolCalls: []ToolCall{{
			ID: "already", Type: "function",
			Function: FunctionCall{Name: "git_status", Arguments: "{}"},
		}},
	}
	if PromoteTextToolCalls(&msg, known) {
		t.Fatal("should not override existing tool_calls")
	}
	if msg.ToolCalls[0].ID != "already" {
		t.Fatal("mutated existing")
	}
}

func TestPromoteEmptyArguments(t *testing.T) {
	known := map[string]bool{"git_status": true}
	msg := Message{Role: "assistant", Content: `{"name":"git_status"}`}
	if !PromoteTextToolCalls(&msg, known) {
		t.Fatal("expected promote")
	}
	if msg.ToolCalls[0].Function.Arguments != "{}" {
		t.Fatalf("args=%q", msg.ToolCalls[0].Function.Arguments)
	}
}
