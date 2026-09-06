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

		resp, err := r.Provider.ChatCompletion(ctx, &llm.ChatRequest{
			Messages:        messages,
			Tools:           tools.Specs(),
			ToolChoice:      "auto",
			Thinking:        map[string]any{"type": "enabled"},
			ReasoningEffort: "high",
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
		msg.ToolCalls = normalizeToolCalls(msg.ToolCalls, step)
		finish := strings.TrimSpace(resp.Choices[0].FinishReason)

		// DeepSeek: append the assistant message as returned (content + reasoning_content + tool_calls).
		messages = append(messages, msg)

		if strings.TrimSpace(msg.ReasoningContent) != "" {
			emit(Event{Type: "reasoning", Content: msg.ReasoningContent})
		}
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
				name := call.Function.Name
				emit(Event{Type: "tool_start", Name: name, Content: call.Function.Arguments})
				result, execErr := r.Tools.Execute(ctx, call)
				ok := execErr == nil
				if execErr != nil {
					result = fmt.Sprintf("ERROR: %v", execErr)
				}
				emit(Event{Type: "tool_end", Name: name, Content: truncate(result, 4000), OK: ok})
				messages = append(messages, llm.ToolResultMessage(call.ID, result))
			}
			continue
		default:
			if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.ReasoningContent) == "" {
				emit(Event{Type: "error", Content: fmt.Sprintf("пустой финальный ответ (finish=%s)", finish)})
			}
			if finish == "length" {
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

func normalizeToolCalls(calls []llm.ToolCall, step int) []llm.ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := make([]llm.ToolCall, len(calls))
	for i, c := range calls {
		if c.ID == "" {
			c.ID = fmt.Sprintf("call_%d_%d", step, i)
		}
		if c.Type == "" {
			c.Type = "function"
		}
		out[i] = c
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
