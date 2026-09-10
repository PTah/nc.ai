package agent

import (
	"fmt"
	"strings"

	"notcursor.ai/app/internal/llm"
)

// History compaction: keep recent tool results verbatim, collapse older ones
// into short placeholders. This bounds context growth on long agent runs.
//
// Call CompactHistory only between user turns (on persisted history before a
// new Run*). Do not re-compact inside the agent step loop — rewriting earlier
// tool messages breaks provider prefix cache hits across steps.

const (
	// keepFullToolResults is how many trailing tool results stay verbatim.
	keepFullToolResults = 3
	// compactedToolMark prefixes collapsed tool outputs.
	compactedToolMark = "[compacted] "
)

// CompactHistory returns a copy of messages where older tool results are
// replaced with short summaries. read_file / list_dir / grep / project dumps
// are compressed more aggressively than short status lines.
// Non-tool messages are never touched.
func CompactHistory(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))
	copy(out, messages)

	toolNameByID := map[string]string{}
	for _, m := range out {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				toolNameByID[tc.ID] = tc.Function.Name
			}
		}
	}

	var toolIdx []int
	for i, m := range out {
		if m.Role == "tool" {
			toolIdx = append(toolIdx, i)
		}
	}
	if len(toolIdx) <= keepFullToolResults {
		return out
	}
	cutoff := len(toolIdx) - keepFullToolResults
	for _, i := range toolIdx[:cutoff] {
		name := toolNameByID[out[i].ToolCallID]
		out[i].Content = compactedToolMark + summarizeToolContent(name, out[i].Content)
	}
	return out
}

// summarizeToolContent squeezes a tool result to a few informative lines.
func summarizeToolContent(toolName, content string) string {
	if content == "" {
		return "(empty result)"
	}
	if strings.HasPrefix(content, compactedToolMark) {
		return strings.TrimSpace(strings.TrimPrefix(content, compactedToolMark))
	}

	heavy := toolName == "read_file" || toolName == "list_dir" || toolName == "grep" ||
		toolName == "glob" || toolName == "find_files"
	if heavy && len(content) > 200 {
		lines := strings.Split(content, "\n")
		head := lines[0]
		if len(head) > 120 {
			head = head[:120] + "…"
		}
		return fmt.Sprintf("%s result omitted (%d lines, %d bytes); re-read if needed. first: %s",
			orTool(toolName), len(lines), len(content), head)
	}

	if len(content) <= 400 {
		return strings.TrimSpace(content)
	}
	lines := strings.Split(content, "\n")
	head := 2
	tail := 3
	if len(lines) <= head+tail {
		return strings.TrimSpace(content)
	}
	var b strings.Builder
	for _, l := range lines[:head] {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "…(%d lines omitted, %d bytes total)…\n", len(lines)-head-tail, len(content))
	for _, l := range lines[len(lines)-tail:] {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

func orTool(name string) string {
	if name == "" {
		return "tool"
	}
	return name
}
