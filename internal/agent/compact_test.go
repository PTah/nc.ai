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
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("old-tool-1\n", 80)},
		{Role: "tool", ToolCallID: "2", Content: strings.Repeat("old-tool-2\n", 80)},
		{Role: "tool", ToolCallID: "3", Content: "recent-a"},
		{Role: "tool", ToolCallID: "4", Content: "recent-b"},
		{Role: "tool", ToolCallID: "5", Content: "recent-c"},
	}
	out := CompactHistory(msgs)
	if !strings.HasPrefix(out[2].Content, compactedToolMark) {
		t.Fatalf("oldest tool should compact: %q", out[2].Content[:40])
	}
	if !strings.HasPrefix(out[3].Content, compactedToolMark) {
		t.Fatalf("2nd oldest tool should compact: %q", out[3].Content[:40])
	}
	for _, i := range []int{4, 5, 6} {
		if strings.HasPrefix(out[i].Content, compactedToolMark) {
			t.Fatalf("trailing tool %d must stay full: %q", i, out[i].Content)
		}
	}
	if out[0].Content != "sys" || out[1].Content != "hi" {
		t.Fatal("non-tool messages must be untouched")
	}
}
