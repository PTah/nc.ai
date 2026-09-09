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

// CompactHistory returns a copy of messages where all but the last
// keepFullToolResults tool messages have their content replaced with a short
// summary line. Non-tool messages are never touched.
func CompactHistory(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))
	copy(out, messages)

	// indexes of tool messages, in order
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
		out[i].Content = compactedToolMark + summarizeToolContent(out[i].Content)
	}
	return out
}

// summarizeToolContent squeezes a tool result to a few informative lines.
func summarizeToolContent(content string) string {
	if content == "" {
		return "(empty result)"
	}
	if len(content) <= 400 {
		return strings.TrimSpace(content)
	}
	lines := strings.Split(content, "\n")
	// keep first 2 and last 3 lines, note the elision size
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
