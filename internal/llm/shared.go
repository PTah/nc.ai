// Package llm holds provider-agnostic helpers shared by DeepSeek and Z.ai clients.
package llm

import "fmt"

// ValidateToolHistory mirrors the OpenAI tool-call protocol used by both providers:
// assistant tool_calls must carry id + function.name, and every tool result
// must reference a tool_call_id that an earlier assistant message actually issued.
func ValidateToolHistory(provider string, messages []Message) error {
	known := map[string]bool{}
	for i, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == "" {
				return fmt.Errorf("%s: assistant tool_call missing id (index %d)", provider, i)
			}
			if tc.Function.Name == "" {
				return fmt.Errorf("%s: assistant tool_call missing function.name (index %d)", provider, i)
			}
			known[tc.ID] = true
		}
	}
	for i, m := range messages {
		if m.Role != "tool" {
			continue
		}
		if m.ToolCallID == "" {
			return fmt.Errorf("%s: tool message missing tool_call_id (index %d)", provider, i)
		}
		if !known[m.ToolCallID] {
			return fmt.Errorf("%s: tool message references unknown tool_call_id %q (index %d)", provider, m.ToolCallID, i)
		}
	}
	return nil
}

// TruncateRunes cuts s to at most n bytes without splitting a UTF-8 rune;
// the result is suffixed with an ellipsis when truncation happened.
func TruncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8RuneStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	for len(cut) > 0 && isCont(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return cut + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
func isCont(b byte) bool        { return b&0xC0 == 0x80 }
