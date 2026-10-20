package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// XML-ish tool dumps some OpenRouter upstreams paste into content instead of
// structured tool_calls, e.g.:
//
//	<function=web_search>
//	<parameter=query>ОГЭ 2025</parameter>
//	</function>
//
// Optional wrappers <tool_call>…</tool_call> are stripped.
var (
	xmlToolCallWrapRE = regexp.MustCompile(`(?is)^\s*<tool_call>\s*|\s*</tool_call>\s*$`)
	xmlFunctionRE     = regexp.MustCompile(`(?is)<function\s*=\s*([A-Za-z0-9_.\-]+)>(.*?)</function>`)
	xmlParameterRE    = regexp.MustCompile(`(?is)<parameter\s*=\s*([A-Za-z0-9_.\-]+)>\s*(.*?)\s*</parameter>`)
	xmlFunctionOpenRE = regexp.MustCompile(`(?is)<function\s*=\s*[A-Za-z0-9_.\-]+>`)
)

// LooksLikeBrokenToolJSON reports content that looks like a tool-call dump
// (local models often paste JSON into the chat) but is not valid/complete enough
// to promote — typically truncated or looped garbage. Also covers XML-ish
// <function=…> dumps that cannot be promoted.
func LooksLikeBrokenToolJSON(content string) bool {
	text := strings.TrimSpace(stripCodeFences(content))
	if text == "" {
		return false
	}
	if looksLikeBrokenXMLToolMarkup(text) {
		return true
	}
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return false
	}
	lead := strings.TrimSpace(text[:start])
	if lead != "" && !isToolCallPreamble(lead) {
		return false
	}
	body := text[start:]
	lower := strings.ToLower(body)
	if !strings.Contains(lower, `"name"`) || !strings.Contains(lower, `"arguments"`) {
		return false
	}
	// Valid promoteable JSON → not "broken".
	if _, ok := parseTextToolCalls(text, nil); ok {
		return false
	}
	return true
}

// looksLikeBrokenXMLToolMarkup is true when content has <function=…> markup
// that is truncated or otherwise not promoteable.
func looksLikeBrokenXMLToolMarkup(text string) bool {
	norm := stripXMLToolCallWrappers(text)
	if !xmlFunctionOpenRE.MatchString(norm) {
		return false
	}
	if _, ok := parseXMLFunctionToolCalls(norm, nil); ok {
		return false
	}
	return true
}

// PromoteTextToolCalls recovers tool calls that local models (Ollama etc.)
// often emit as plain JSON in message.content instead of structured tool_calls.
// Returns true if ToolCalls were filled from content.
//
// Accepted shapes (single or concatenated):
//
//	{"name":"git_status","arguments":{}}
//	{"name":"ask_user","arguments":{"question":"…"}}
//	{"type":"function","function":{"name":"…","arguments":"…"}}
//	<function=web_search><parameter=query>…</parameter></function>
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
		calls, ok = parseXMLFunctionToolCalls(stripXMLToolCallWrappers(stripCodeFences(raw)), known)
	}
	if !ok || len(calls) == 0 {
		return false
	}
	msg.ToolCalls = calls
	msg.Content = ""
	return true
}

func stripXMLToolCallWrappers(s string) string {
	s = strings.TrimSpace(s)
	for {
		next := strings.TrimSpace(xmlToolCallWrapRE.ReplaceAllString(s, ""))
		if next == s {
			return s
		}
		s = next
	}
}

// parseXMLFunctionToolCalls promotes Hermes/Qwen-style XML function blocks.
// The whole content (after wrappers) must be one or more <function=…> blocks;
// any leftover prose rejects the promote (same rule as JSON).
func parseXMLFunctionToolCalls(raw string, known map[string]bool) ([]ToolCall, bool) {
	text := strings.TrimSpace(raw)
	if text == "" || !xmlFunctionOpenRE.MatchString(text) {
		return nil, false
	}
	matches := xmlFunctionRE.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, false
	}
	// Leading preamble before the first <function=…> must be empty or a short cue.
	first := matches[0][0]
	lead := strings.TrimSpace(text[:first])
	if lead != "" && !isToolCallPreamble(lead) {
		return nil, false
	}
	calls := make([]ToolCall, 0, len(matches))
	cursor := first
	for i, m := range matches {
		if m[0] < cursor {
			return nil, false
		}
		gap := strings.TrimSpace(text[cursor:m[0]])
		if gap != "" {
			return nil, false
		}
		name := text[m[2]:m[3]]
		body := text[m[4]:m[5]]
		if known != nil && !known[name] {
			return nil, false
		}
		args := xmlParametersToJSON(body)
		calls = append(calls, ToolCall{
			ID:   fmt.Sprintf("call_xml_%d", i+1),
			Type: "function",
			Function: FunctionCall{
				Name:      name,
				Arguments: args,
			},
		})
		cursor = m[1]
	}
	rest := strings.TrimSpace(text[cursor:])
	if rest != "" {
		return nil, false
	}
	return calls, len(calls) > 0
}

func xmlParametersToJSON(body string) string {
	params := xmlParameterRE.FindAllStringSubmatch(body, -1)
	if len(params) == 0 {
		inner := strings.TrimSpace(body)
		if inner == "" {
			return "{}"
		}
		// Bare body without <parameter=> — treat as single "query"/"input" is too
		// guessy; keep empty object so the tool can return a clear validation error.
		return "{}"
	}
	obj := make(map[string]string, len(params))
	for _, p := range params {
		key := strings.TrimSpace(p[1])
		val := strings.TrimSpace(p[2])
		if key == "" {
			continue
		}
		obj[key] = val
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return "{}"
	}
	return string(b)
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
