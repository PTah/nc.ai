package agent

import (
	"strings"

	"notcursor.ai/app/internal/llm"
)

// NextAction is the protocol decision after one Chat Completions response.
type NextAction int

const (
	ActionFinal NextAction = iota
	ActionExecuteTools
	ActionError
)

// DecideNext implements DeepSeek + OpenAI tool-call loop:
// continue only while assistant.tool_calls is non-empty.
// finish_reason is not used to skip tools (DeepSeek may send content + tool_calls
// with finish_reason=stop).
func DecideNext(msg llm.Message, finish string) (NextAction, string) {
	finish = strings.TrimSpace(finish)
	switch finish {
	case "content_filter":
		return ActionError, "ответ отфильтрован (content_filter)"
	case "insufficient_system_resource":
		return ActionError, "нехватка ресурсов inference (insufficient_system_resource)"
	}
	if len(msg.ToolCalls) > 0 {
		return ActionExecuteTools, finish
	}
	return ActionFinal, finish
}
