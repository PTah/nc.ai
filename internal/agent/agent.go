package agent

import (
	"context"
	"fmt"
	"strings"

	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/llm/providers/deepseek"
	"notcursor.ai/app/internal/tools"
)

const DefaultMaxSteps = 40

const SystemPrompt = `You are NotCursor.ai, a coding agent like Cursor.
You work inside the user's local workspace.
Use tools to read/write files, run shell/PowerShell, use git, and SSH when needed.

Critical path rules:
- NEVER invent file paths or filenames.
- Before read_file, confirm the path via list_dir or search_files.
- If read_file fails with "file not found", use the suggested siblings / list_dir and retry the real path.
- Paths are relative to the workspace root (use forward slashes).

When the user attaches screenshots/images, describe and use what you see; then act with tools.
Never invent file contents — read with tools first.
After tool results, always give a final textual answer to the user.
Prefer small precise edits.`

// Event is pushed to the UI during an agent run.
type Event struct {
	Type      string `json:"type"` // delta|reasoning|tool_start|tool_end|done|error|persist
	Content   string `json:"content,omitempty"`
	Name      string `json:"name,omitempty"`
	OK        bool   `json:"ok,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

type EmitFunc func(Event)

// Attachment is a chat paste/drop payload from the UI.
type Attachment struct {
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	DataURL string `json:"dataUrl"` // data:...;base64,... for images
	Text    string `json:"text"`    // extracted text for non-image files
	IsImage bool   `json:"isImage"`
}

type Runner struct {
	Provider llm.Provider
	Tools    *tools.Registry
	MaxSteps int
	// ModelOverride forces a model for this run (e.g. vision).
	ModelOverride string
	// RulesText is appended to the system prompt (Cursor .cursorrules / .cursor/rules).
	RulesText string
	// OnUsage, when set, is called after each provider response with the model
	// that produced it and its token usage (usage may be nil for some providers).
	OnUsage func(model string, u *llm.Usage)
}

func (r *Runner) reportUsage(model string, u *llm.Usage) {
	if r.OnUsage == nil {
		return
	}
	r.OnUsage(model, u)
}

func (r *Runner) systemPrompt() string {
	rules := strings.TrimSpace(r.RulesText)
	if rules == "" {
		return SystemPrompt
	}
	return SystemPrompt + "\n\n" +
		"## Cursor rules loaded for this project\n" +
		"The user has configured rules via Cursor (.cursorrules / .cursor/rules). " +
		"Apply them when they are relevant to the task and follow them over this base prompt:\n\n" +
		rules
}

func (r *Runner) Run(ctx context.Context, history []llm.Message, userText string, emit EmitFunc) ([]llm.Message, error) {
	return r.RunMessage(ctx, history, llm.UserText(userText), emit)
}

func (r *Runner) RunWithAttachments(ctx context.Context, history []llm.Message, userText string, atts []Attachment, emit EmitFunc) ([]llm.Message, error) {
	var images []string
	var extraText strings.Builder
	if userText != "" {
		extraText.WriteString(userText)
	}
	for _, a := range atts {
		if a.IsImage && a.DataURL != "" {
			images = append(images, a.DataURL)
			continue
		}
		if a.Text != "" {
			if extraText.Len() > 0 {
				extraText.WriteString("\n\n")
			}
			extraText.WriteString("----- attached file: ")
			extraText.WriteString(a.Name)
			extraText.WriteString(" -----\n")
			extraText.WriteString(a.Text)
		} else if a.Name != "" {
			if extraText.Len() > 0 {
				extraText.WriteString("\n\n")
			}
			extraText.WriteString("(binary attachment not inlined: ")
			extraText.WriteString(a.Name)
			extraText.WriteString(" — save it into the workspace and use tools)")
		}
	}
	msg := llm.UserMultimodal(extraText.String(), images)
	if len(images) > 0 && r.ModelOverride == "" {
		r.ModelOverride = deepseek.VisionModel
	}
	return r.RunMessage(ctx, history, msg, emit)
}

// RunMessage implements DeepSeek Thinking+Tool Calls loop.
func (r *Runner) RunMessage(ctx context.Context, history []llm.Message, userMsg llm.Message, emit EmitFunc) ([]llm.Message, error) {
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
	for _, m := range history {
		if m.Role == "system" {
			continue
		}
		messages = append(messages, m)
	}
	// Always prepend a fresh system prompt so the current Cursor rules are in
	// effect even when the persisted history already contains an old system message.
	messages = append([]llm.Message{{Role: "system", Content: r.systemPrompt()}}, messages...)
	messages = append(messages, userMsg)

	for step := 0; step < max; step++ {
		if ctx.Err() != nil {
			emit(Event{Type: "error", Content: ctx.Err().Error()})
			return messages, ctx.Err()
		}

		req := &llm.ChatRequest{
			Messages:        messages,
			Tools:           tools.Specs(),
			ToolChoice:      "auto",
			Thinking:        map[string]any{"type": "enabled"},
			ReasoningEffort: "high",
			Stream:          false,
			Model:           r.ModelOverride,
		}
		resp, err := r.Provider.ChatCompletion(ctx, req)
		if err != nil {
			emit(Event{Type: "error", Content: err.Error()})
			return messages, err
		}
		r.reportUsage(billingModel(resp.Model, r.ModelOverride), resp.Usage)
		if len(resp.Choices) == 0 {
			err = fmt.Errorf("empty model response")
			emit(Event{Type: "error", Content: err.Error()})
			return messages, err
		}

		msg := resp.Choices[0].Message
		msg.Role = "assistant"
		finish := strings.TrimSpace(resp.Choices[0].FinishReason)

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
				messages = append(messages, llm.ToolResultMessage(call.ID, result))
			}
			continue
		default:
			if strings.TrimSpace(msg.Content) == "" {
				emit(Event{Type: "error", Content: fmt.Sprintf("пустой финальный ответ (finish=%s)", finish)})
			} else if finish == "length" {
				emit(Event{Type: "error", Content: "ответ обрезан (finish=length)"})
			}
			emit(Event{Type: "done", Content: finish})
			return messages, nil
		}
	}

	// Soft landing: ask for a final answer without more tools instead of hard-failing.
	emit(Event{Type: "delta", Content: fmt.Sprintf("\n\n_(достигнут лимит %d шагов tools — прошу финальный ответ)_\n\n", max)})
	messages = append(messages, llm.UserText(
		"Stop using tools now. Summarize what you already found/did and give the best final answer to the user in their language. If something is unfinished, say exactly what remains.",
	))
	req := &llm.ChatRequest{
		Messages:        messages,
		Thinking:        map[string]any{"type": "enabled"},
		ReasoningEffort: "high",
		Stream:          false,
		Model:           r.ModelOverride,
	}
	resp, err := r.Provider.ChatCompletion(ctx, req)
	if err != nil {
		emit(Event{Type: "error", Content: fmt.Sprintf("лимит шагов (%d); финальный ответ не получен: %v", max, err)})
		return messages, err
	}
	r.reportUsage(billingModel(resp.Model, r.ModelOverride), resp.Usage)
	if len(resp.Choices) == 0 {
		emit(Event{Type: "error", Content: fmt.Sprintf("лимит шагов (%d); пустой финальный ответ", max)})
		return messages, fmt.Errorf("max agent steps (%d) exceeded", max)
	}
	msg := resp.Choices[0].Message
	msg.Role = "assistant"
	messages = append(messages, msg)
	if strings.TrimSpace(msg.ReasoningContent) != "" {
		emit(Event{Type: "reasoning", Content: msg.ReasoningContent})
	}
	if strings.TrimSpace(msg.Content) != "" {
		emit(Event{Type: "delta", Content: msg.Content})
		emit(Event{Type: "done", Content: "max_steps_wrapup"})
		return messages, nil
	}
	emit(Event{Type: "error", Content: fmt.Sprintf("лимит шагов агента (%d) — модель не дала финальный текст", max)})
	return messages, fmt.Errorf("max agent steps (%d) exceeded", max)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// billingModel prefers the model id DeepSeek returned; falls back to the
// request override so costing still keys the right rate card.
func billingModel(respModel, override string) string {
	if strings.TrimSpace(respModel) != "" {
		return respModel
	}
	return override
}
