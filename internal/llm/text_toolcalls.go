package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// PromoteTextToolCalls recovers tool calls that local models (Ollama etc.)
// often emit as plain JSON in message.content instead of structured tool_calls.
// Returns true if ToolCalls were filled from content.
//
// Accepted shapes (single or concatenated):
//
//	{"name":"git_status","arguments":{}}
//	{"name":"ask_user","arguments":{"question":"…"}}
//	{"type":"function","function":{"name":"…","arguments":"…"}}
//
// known, if non-nil, restricts promotion to those tool names (recommended).
func PromoteTextToolCalls(msg *Message, known map[string]bool) bool {
	if msg == nil || len(msg.ToolCalls) > 0 {
		return false
	}
	raw := strings.TrimSpace(msg.Content)
	if raw == "" {
		return false
	}
	calls, ok := parseTextToolCalls(raw, known)
	if !ok || len(calls) == 0 {
		return false
	}
	msg.ToolCalls = calls
	msg.Content = ""
	return true
}

// KnownToolNames builds a lookup set from OpenAI-shaped tool specs.
func KnownToolNames(specs []ToolSpec) map[string]bool {
	out := make(map[string]bool, len(specs))
	for _, s := range specs {
		if name := strings.TrimSpace(s.Function.Name); name != "" {
			out[name] = true
		}
	}
	return out
}

func parseTextToolCalls(raw string, known map[string]bool) ([]ToolCall, bool) {
	text := strings.TrimSpace(stripCodeFences(raw))
	if text == "" {
		return nil, false
	}

	if strings.HasPrefix(text, "[") {
		calls, ok := decodeToolCallArray(text, known)
		return calls, ok
	}

	start := strings.IndexByte(text, '{')
	if start < 0 {
		return nil, false
	}
	lead := strings.TrimSpace(text[:start])
	if lead != "" && !isToolCallPreamble(lead) {
		return nil, false
	}

	calls, end := decodeConcatToolObjects(text[start:], known)
	if len(calls) == 0 {
		return nil, false
	}
	rest := strings.TrimSpace(text[start+end:])
	if rest != "" {
		return nil, false
	}
	return calls, true
}

func isToolCallPreamble(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(lower, "tool"):
		return true
	case strings.HasPrefix(lower, "вызов"):
		return true
	case strings.HasPrefix(lower, "function"):
		return true
	}
	if strings.Contains(s, "{") || strings.Count(s, "\n") > 1 {
		return false
	}
	return len([]rune(s)) <= 40
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	lines = lines[1:]
	if n := len(lines); n > 0 && strings.HasPrefix(strings.TrimSpace(lines[n-1]), "```") {
		lines = lines[:n-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func decodeToolCallArray(text string, known map[string]bool) ([]ToolCall, bool) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, false
	}
	calls := make([]ToolCall, 0, len(raw))
	for i, item := range raw {
		tc, ok := decodeOneToolObject(item, known, i)
		if !ok {
			return nil, false
		}
		calls = append(calls, tc)
	}
	return calls, len(calls) > 0
}

// decodeConcatToolObjects reads successive JSON objects from s.
// Returns calls and the byte offset past the last consumed object (and trailing spaces).
func decodeConcatToolObjects(s string, known map[string]bool) ([]ToolCall, int) {
	dec := json.NewDecoder(strings.NewReader(s))
	calls := make([]ToolCall, 0, 4)
	var end int
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		tc, ok := decodeOneToolObject(raw, known, len(calls))
		if !ok {
			if len(calls) == 0 {
				return nil, 0
			}
			break
		}
		calls = append(calls, tc)
		end = int(dec.InputOffset())
		// Peek: more objects?
		rest := s[end:]
		i := 0
		for i < len(rest) && unicode.IsSpace(rune(rest[i])) {
			i++
		}
		if i >= len(rest) || rest[i] != '{' {
			end += i
			break
		}
		// Decoder already positioned after whitespace between values when next Decode runs.
	}
	return calls, end
}

func decodeOneToolObject(raw json.RawMessage, known map[string]bool, idx int) (ToolCall, bool) {
	var plain struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Type      string          `json:"type"`
		Function  *struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"function"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &plain); err != nil {
		return ToolCall{}, false
	}

	name := strings.TrimSpace(plain.Name)
	argsRaw := plain.Arguments
	id := strings.TrimSpace(plain.ID)

	if plain.Function != nil {
		if n := strings.TrimSpace(plain.Function.Name); n != "" {
			name = n
		}
		if len(plain.Function.Arguments) > 0 {
			argsRaw = plain.Function.Arguments
		}
	}
	if name == "" {
		return ToolCall{}, false
	}
	if known != nil && !known[name] {
		return ToolCall{}, false
	}

	args := normalizeArgumentsJSON(argsRaw)
	if id == "" {
		id = fmt.Sprintf("call_text_%d", idx+1)
	}
	typ := strings.TrimSpace(plain.Type)
	if typ == "" {
		typ = "function"
	}
	return ToolCall{
		ID:   id,
		Type: typ,
		Function: FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}, true
}

func normalizeArgumentsJSON(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return "{}"
		}
		return s
	}
	return string(raw)
}
