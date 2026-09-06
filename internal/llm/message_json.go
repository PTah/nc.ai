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
	ReasoningContent string          `json:"reasoning_content,omitempty"`
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
	m.ReasoningContent = w.ReasoningContent
	m.Content = parseContent(w.Content)
	return nil
}

func (m Message) MarshalJSON() ([]byte, error) {
	var content any
	if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.Content == "" {
		content = nil
	} else {
		content = m.Content
	}
	out := struct {
		Role             string     `json:"role"`
		Content          any        `json:"content"`
		Name             string     `json:"name,omitempty"`
		ToolCallID       string     `json:"tool_call_id,omitempty"`
		ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
		ReasoningContent string     `json:"reasoning_content,omitempty"`
	}{
		Role:             m.Role,
		Content:          content,
		Name:             m.Name,
		ToolCallID:       m.ToolCallID,
		ToolCalls:        m.ToolCalls,
		ReasoningContent: m.ReasoningContent,
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
	f.Arguments = string(w.Arguments)
	return nil
}
