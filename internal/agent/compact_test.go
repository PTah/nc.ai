package agent

import (
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestCompactHistory_KeepsTrailingToolResults(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "1", Function: llm.FunctionCall{Name: "read_file"}},
			{ID: "2", Function: llm.FunctionCall{Name: "read_file"}},
			{ID: "3", Function: llm.FunctionCall{Name: "git_status"}},
			{ID: "4", Function: llm.FunctionCall{Name: "git_status"}},
			{ID: "5", Function: llm.FunctionCall{Name: "git_status"}},
		}},
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("old-tool-1\n", 80)},
		{Role: "tool", ToolCallID: "2", Content: strings.Repeat("old-tool-2\n", 80)},
		{Role: "tool", ToolCallID: "3", Content: "recent-a"},
		{Role: "tool", ToolCallID: "4", Content: "recent-b"},
		{Role: "tool", ToolCallID: "5", Content: "recent-c"},
	}
	out := CompactHistory(msgs)
	if !strings.HasPrefix(out[3].Content, compactedToolMark) {
		t.Fatalf("oldest tool should compact: %q", out[3].Content[:min(40, len(out[3].Content))])
	}
	if !strings.Contains(out[3].Content, "read_file result omitted") {
		t.Fatalf("heavy read_file should be aggressively compacted: %q", out[3].Content)
	}
	for _, i := range []int{5, 6, 7} {
		if strings.HasPrefix(out[i].Content, compactedToolMark) {
			t.Fatalf("trailing tool %d must stay full: %q", i, out[i].Content)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
