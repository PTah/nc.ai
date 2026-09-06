package agent

import (
	"context"
	"fmt"
	"strings"

	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/tools"
)

const DefaultMaxSteps = 12

const SystemPrompt = `You are NotCursor.ai, a coding agent like Cursor.
You work inside the user's local workspace.
Use tools to read/write files, run shell/PowerShell, use git, and SSH when needed.
Never invent file contents — read with tools first.
After tool results, always give a final textual answer to the user.
Prefer small precise edits. Paths are relative to the workspace root.`

// Event is pushed to the UI during an agent run.
type Event struct {
	Type    string `json:"type"` // delta|reasoning|tool_start|tool_end|done|error
	Content string `json:"content,omitempty"`
	Name    string `json:"name,omitempty"`
	OK      bool   `json:"ok,omitempty"`
}

type EmitFunc func(Event)

type Runner struct {
	Provider llm.Provider
	Tools    *tools.Registry
	MaxSteps int
}

// Run implements DeepSeek Thinking+Tool Calls loop exactly:
//
//	messages.append(response.choices[0].message)  // content + reasoning_content + tool_calls
//	if tool_calls is empty: stop
//	else: execute tools, append role=tool, continue
//
// See docs/exchange-protocols/deepseek.md and
// https://api-docs.deepseek.com/guides/tool_calls + thinking_mode.
func (r *Runner) Run(ctx context.Context, history []llm.Message, userText string, emit EmitFunc) ([]llm.Message, error) {
	if r.Provider == nil {
		return history, fmt.Errorf("provider is nil")
	}
	if r.Tools == nil {
		return history, fmt.Errorf("tools registry is nil")
	}
	max := r.MaxSteps
	if max <= 0 {
		max = DefaultMaxSteps
	}
	if emit == nil {
		emit = func(Event) {}
	}

	messages := make([]llm.Message, 0, len(history)+8)
	hasSystem := false
	for _, m := range history {
		if m.Role == "system" {
			hasSystem = true
		}
		messages = append(messages, m)
	}
	if !hasSystem {
		messages = append([]llm.Message{{Role: "system", Content: SystemPrompt}}, messages...)
	}
	messages = append(messages, llm.Message{Role: "user", Content: userText})

	for step := 0; step < max; step++ {
		if ctx.Err() != nil {
			emit(Event{Type: "error", Content: ctx.Err().Error()})
			return messages, ctx.Err()
		}

		// Sub-turn request: always carry tools + thinking (DeepSeek V4 agent canon).
		resp, err := r.Provider.ChatCompletion(ctx, &llm.ChatRequest{
			Messages:        messages,
			Tools:           tools.Specs(),
			ToolChoice:      "auto",
			Thinking:        map[string]any{"type": "enabled"},
			ReasoningEffort: "high",
			Stream:          false,
		})
		if err != nil {
			emit(Event{Type: "error", Content: err.Error()})
			return messages, err
		}
		if len(resp.Choices) == 0 {
			err = fmt.Errorf("empty model response")
			emit(Event{Type: "error", Content: err.Error()})
			return messages, err
		}

		msg := resp.Choices[0].Message
		msg.Role = "assistant"
		finish := strings.TrimSpace(resp.Choices[0].FinishReason)

		// CRITICAL: append assistant message AS RETURNED (do not drop reasoning_content).
		messages = append(messages, msg)

		if strings.TrimSpace(msg.ReasoningContent) != "" {
			emit(Event{Type: "reasoning", Content: msg.ReasoningContent})
		}
		// Intermediate content alongside tool_calls is normal (DeepSeek sample Turn 1.1).
		if strings.TrimSpace(msg.Content) != "" {
			emit(Event{Type: "delta", Content: msg.Content})
		}

		action, detail := DecideNext(msg, finish)
		switch action {
		case ActionError:
			emit(Event{Type: "error", Content: detail})
			return messages, fmt.Errorf("%s", detail)
		case ActionExecuteTools:
			for _, call := range msg.ToolCalls {
				if call.ID == "" {
					emit(Event{Type: "error", Content: "tool_call without id — protocol violation"})
					return messages, fmt.Errorf("tool_call without id")
				}
				name := call.Function.Name
				emit(Event{Type: "tool_start", Name: name, Content: call.Function.Arguments})
				result, execErr := r.Tools.Execute(ctx, call)
				ok := execErr == nil
				if execErr != nil {
					result = fmt.Sprintf("ERROR: %v", execErr)
				}
				emit(Event{Type: "tool_end", Name: name, Content: truncate(result, 4000), OK: ok})
				// Protocol tool result shape:
				// {"role":"tool","tool_call_id":"...","content":"..."}
				messages = append(messages, llm.ToolResultMessage(call.ID, result))
			}
			continue
		default:
			// tool_calls empty → final answer (may still have empty content; surface that)
			if strings.TrimSpace(msg.Content) == "" {
				emit(Event{Type: "error", Content: fmt.Sprintf("пустой финальный ответ (finish=%s)", finish)})
			} else if finish == "length" {
				emit(Event{Type: "error", Content: "ответ обрезан (finish=length)"})
			}
			emit(Event{Type: "done", Content: finish})
			return messages, nil
		}
	}

	err := fmt.Errorf("max agent steps (%d) exceeded", max)
	emit(Event{Type: "error", Content: err.Error()})
	return messages, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
