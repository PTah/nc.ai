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
	if err := llm.ValidateToolHistory("deepseek", msgs); err != nil {
		t.Fatalf("validateToolHistory = %v, want nil", err)
	}
}

func TestValidateToolHistoryMissingCallID(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("", "list_dir")}},
		llm.ToolResultMessage("", "ok"),
	}
	if err := llm.ValidateToolHistory("deepseek", msgs); err == nil {
		t.Fatal("missing tool_call id: want error")
	}
}

func TestValidateToolHistoryMissingCallName(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("call_1", "")}},
		llm.ToolResultMessage("call_1", "ok"),
	}
	if err := llm.ValidateToolHistory("deepseek", msgs); err == nil {
		t.Fatal("missing function.name: want error")
	}
}

func TestValidateToolHistoryToolMissingID(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("call_1", "list_dir")}},
		{Role: "tool", Content: "ok"},
	}
	if err := llm.ValidateToolHistory("deepseek", msgs); err == nil {
		t.Fatal("tool message without tool_call_id: want error")
	}
}

func TestValidateToolHistoryOrphanToolResult(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call("call_1", "list_dir")}},
		llm.ToolResultMessage("call_404", "ok"),
	}
	if err := llm.ValidateToolHistory("deepseek", msgs); err == nil {
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

func TestMergeKnownModelsUsesAPICatalog(t *testing.T) {
	got := MergeKnownModels([]string{"deepseek-flash", "deepseek-flash", " other "})
	if len(got) != 2 || got[0] != "deepseek-flash" || got[1] != "other" {
		t.Fatalf("MergeKnownModels = %v, want trimmed/deduped catalog", got)
	}
	// Retired ids must not be injected when the API catalog is available.
	for _, id := range got {
		if id == "deepseek-v4-flash-vision-exp" {
			t.Fatal("retired ids must not be merged into the catalog")
		}
	}
	if fb := MergeKnownModels(nil); len(fb) == 0 || fb[0] != DefaultModel {
		t.Fatalf("empty catalog should fall back to %s, got %v", DefaultModel, fb)
	}
}

func TestModelDiff(t *testing.T) {
	added, removed := ModelDiff([]string{"a", "b"}, []string{"b", "c"})
	if len(added) != 1 || added[0] != "c" {
		t.Fatalf("added = %v, want [c]", added)
	}
	if len(removed) != 1 || removed[0] != "a" {
		t.Fatalf("removed = %v, want [a]", removed)
	}
}

func TestPreferAndOrderModels(t *testing.T) {
	got := OrderModels(MergeKnownModels([]string{"deepseek-flash", "deepseek-v4-pro"}))
	if len(got) != 2 {
		t.Fatalf("OrderModels = %v, want the API catalog", got)
	}
	if PreferModel(got, "deepseek-v4-pro") != "deepseek-v4-pro" {
		t.Fatal("PreferModel should keep current when listed")
	}
	if PreferModel(got, "gone") != DefaultModel {
		t.Fatal("PreferModel should fall back to flash")
	}
}
