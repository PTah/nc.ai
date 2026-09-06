package llm

import (
	"encoding/json"
	"testing"
)

func TestMessageContentVariants(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"role":"assistant","content":"hello"}`, "hello"},
		{`{"role":"assistant","content":null,"tool_calls":[]}`, ""},
		{`{"role":"assistant","content":[{"type":"text","text":"hi"}]}`, "hi"},
	}
	for _, tc := range cases {
		var m Message
		if err := json.Unmarshal([]byte(tc.in), &m); err != nil {
			t.Fatal(err)
		}
		if m.Content != tc.want {
			t.Fatalf("content=%q want %q for %s", m.Content, tc.want, tc.in)
		}
	}
}

func TestFunctionCallArgumentsObject(t *testing.T) {
	raw := []byte(`{"name":"list_dir","arguments":{"path":"."}}`)
	var f FunctionCall
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Name != "list_dir" {
		t.Fatalf("name=%s", f.Name)
	}
	if f.Arguments != `{"path":"."}` && f.Arguments != `{"path": "."}` {
		var m map[string]any
		if err := json.Unmarshal([]byte(f.Arguments), &m); err != nil || m["path"] != "." {
			t.Fatalf("arguments=%s", f.Arguments)
		}
	}
}

func TestReasoningContentRoundTripWithTools(t *testing.T) {
	m := Message{
		Role:             "assistant",
		ReasoningContent: "need list_dir",
		ToolCalls: []ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: FunctionCall{
				Name:      "list_dir",
				Arguments: `{"path":"."}`,
			},
		}},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["reasoning_content"] != "need list_dir" {
		t.Fatalf("reasoning_content missing: %s", b)
	}
	var back Message
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ReasoningContent != "need list_dir" {
		t.Fatalf("roundtrip reasoning=%q", back.ReasoningContent)
	}
}

func TestAssistantToolCallMarshalsNullContent(t *testing.T) {
	m := Message{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: FunctionCall{
				Name:      "list_dir",
				Arguments: `{"path":"."}`,
			},
		}},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["content"] != nil {
		t.Fatalf("content=%v want null", raw["content"])
	}
}
