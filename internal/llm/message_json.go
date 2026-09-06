package llm

import (
	"encoding/json"
	"strings"
)

type wireMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	Name             string          `json:"name,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
	ReasoningContent *string         `json:"reasoning_content"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var w wireMessage
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	m.Role = w.Role
	m.Name = w.Name
	m.ToolCallID = w.ToolCallID
	m.ToolCalls = w.ToolCalls
	m.Content = parseContent(w.Content)
	if w.ReasoningContent != nil {
		m.ReasoningContent = *w.ReasoningContent
	} else {
		m.ReasoningContent = ""
	}
	return nil
}

func (m Message) MarshalJSON() ([]byte, error) {
	// DeepSeek canon: append assistant message as returned.
	// - content may be null when tool_calls present and text empty
	// - reasoning_content must round-trip whenever tools are used in the request
	var content any
	if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.Content == "" {
		content = nil
	} else {
		content = m.Content
	}

	type outMsg struct {
		Role             string     `json:"role"`
		Content          any        `json:"content"`
		Name             string     `json:"name,omitempty"`
		ToolCallID       string     `json:"tool_call_id,omitempty"`
		ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
		ReasoningContent any        `json:"reasoning_content,omitempty"`
	}
	out := outMsg{
		Role:       m.Role,
		Content:    content,
		Name:       m.Name,
		ToolCallID: m.ToolCallID,
		ToolCalls:  m.ToolCalls,
	}
	// Always include reasoning_content for assistant turns when non-empty
	// (required when subsequent requests carry tools).
	if m.Role == "assistant" && m.ReasoningContent != "" {
		out.ReasoningContent = m.ReasoningContent
	}
	return json.Marshal(out)
}

func parseContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return strings.TrimSpace(string(raw))
}

func (f *FunctionCall) UnmarshalJSON(data []byte) error {
	var w struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	f.Name = w.Name
	if len(w.Arguments) == 0 || string(w.Arguments) == "null" {
		f.Arguments = "{}"
		return nil
	}
	var s string
	if err := json.Unmarshal(w.Arguments, &s); err == nil {
		f.Arguments = s
		return nil
	}
	// Some gateways return arguments as object — canonicalize to JSON string.
	f.Arguments = string(w.Arguments)
	return nil
}

func (f FunctionCall) MarshalJSON() ([]byte, error) {
	args := f.Arguments
	if args == "" {
		args = "{}"
	}
	// Protocol: arguments MUST be a JSON string, never a nested object.
	return json.Marshal(struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{
		Name:      f.Name,
		Arguments: args,
	})
}
