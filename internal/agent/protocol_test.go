package agent

import (
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
