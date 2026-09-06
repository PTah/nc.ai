package agent

import (
	"encoding/json"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestDecideNext(t *testing.T) {
	withTools := llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: llm.FunctionCall{
				Name:      "list_dir",
				Arguments: `{"path":"."}`,
			},
		}},
	}
	cases := []struct {
		name   string
		finish string
		tools  bool
		want   NextAction
	}{
		{"tool_calls", "tool_calls", true, ActionExecuteTools},
		{"stop_with_tools", "stop", true, ActionExecuteTools},
		{"stop_no_tools", "stop", false, ActionFinal},
		{"empty_finish_with_tools", "", true, ActionExecuteTools},
		{"length_no_tools", "length", false, ActionFinal},
		{"length_with_tools", "length", true, ActionExecuteTools},
		{"content_filter", "content_filter", false, ActionError},
		{"resource", "insufficient_system_resource", true, ActionError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := withTools
			if !tc.tools {
				msg.ToolCalls = nil
			}
			got, _ := DecideNext(msg, tc.finish)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// TestOfficialSampleAppendShape mirrors DeepSeek docs:
// messages.append({role, content, reasoning_content, tool_calls})
func TestOfficialSampleAppendShape(t *testing.T) {
	msg := llm.Message{
		Role:             "assistant",
		Content:          "Let me check.",
		ReasoningContent: "Need date then weather.",
		ToolCalls: []llm.ToolCall{{
			ID:   "call_00_kw66",
			Type: "function",
			Function: llm.FunctionCall{
				Name:      "get_date",
				Arguments: "{}",
			},
		}},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["role"] != "assistant" {
		t.Fatalf("role=%v", raw["role"])
	}
	if raw["content"] != "Let me check." {
		t.Fatalf("content=%v", raw["content"])
	}
	if raw["reasoning_content"] != "Need date then weather." {
		t.Fatalf("reasoning_content=%v", raw["reasoning_content"])
	}
	calls, _ := raw["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool_calls=%v", raw["tool_calls"])
	}
	// Intermediate content + tool_calls ⇒ still execute tools
	action, _ := DecideNext(msg, "stop")
	if action != ActionExecuteTools {
		t.Fatalf("action=%v want ExecuteTools", action)
	}
}

func TestFinalWhenToolCallsNil(t *testing.T) {
	msg := llm.Message{
		Role:             "assistant",
		Content:          "Done.",
		ReasoningContent: "share result",
	}
	action, _ := DecideNext(msg, "stop")
	if action != ActionFinal {
		t.Fatalf("action=%v", action)
	}
}
