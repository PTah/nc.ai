package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/tools"
)

const DefaultMaxSteps = 40

const maxToolResultBytes = 12288

const SystemPrompt = `You are NotCursor.ai, a coding agent like Cursor.
You work inside the user's local workspace.
Use tools to read/write files, run shell/PowerShell, use git, and SSH when needed.

Critical path rules:
- NEVER invent file paths or filenames.
- Before read_file, confirm the path via list_dir or search_files.
- If read_file fails with "file not found", use the suggested siblings / list_dir and retry the real path.
- Paths are relative to the workspace root (use forward slashes).

Token and edit discipline (save cost; follow project rules when they conflict with defaults):
- Prefer apply_patch for partial file edits; use write_file only for new files or full rewrites.
- Prefer small precise edits over rewrites or copy-paste duplicates.
- Do not add verbose comments, docstrings, or drive-by refactors unless the user asks.
- Do not expand scope beyond the requested task.
- Avoid re-reading huge files or dumping entire directories when a targeted search/read suffices.

When the user attaches screenshots/images, describe and use what you see; then act with tools.
Never invent file contents — read with tools first.
After tool results, always give a final textual answer to the user.`

// Event is pushed to the UI during an agent run.
type Event struct {
	Type      string `json:"type"` // delta|reasoning|tool_start|tool_end|reconnect|done|error|persist|usage|model
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
	// ModelOverride forces a model for this run (e.g. vision / Auto pick).
	ModelOverride string
	// PreferredModel is used when AutoModels is off and ModelOverride is empty.
	PreferredModel string
	// AutoModels enables PickModel routing for this run.
	AutoModels bool
	// Route context for AutoModels (filled by the app before Run*).
	UserText      string
	HasImages     bool
	HintPathCount int
	// RulesText is appended to the system prompt (Cursor .cursorrules / .cursor/rules).
	RulesText string
	// RetryCount is the number of automatic reconnect attempts for transient network errors.
	RetryCount int
	// RetryBackoff is the base wait between reconnect attempts (grows linearly per attempt).
	RetryBackoff time.Duration
	// OnUsage, when set, is called after each provider response with the model
	// that produced it and its token usage (usage may be nil for some providers).
	OnUsage func(model string, u *llm.Usage)

	lastEmittedModel string
}

func (r *Runner) reportUsage(model string, u *llm.Usage) {
	if r.OnUsage == nil {
		return
	}
	r.OnUsage(model, u)
}

// resolveModel picks the model for this step and emits a "model" event when it changes.
func (r *Runner) resolveModel(step int, emit EmitFunc) string {
	var model, reason string
	switch {
	case r.AutoModels:
		d := PickModel(RouteInput{
			UserText:      r.UserText,
			HasImages:     r.HasImages,
			HintPathCount: r.HintPathCount,
			Step:          step,
		})
		model, reason = d.Model, d.Reason
	case r.HasImages:
		model, reason = ModelVision, "image"
	case strings.TrimSpace(r.ModelOverride) != "":
		model = strings.TrimSpace(r.ModelOverride)
	case strings.TrimSpace(r.PreferredModel) != "":
		model = strings.TrimSpace(r.PreferredModel)
	default:
		model = ModelFlash
	}
	r.ModelOverride = model
	if model != "" && model != r.lastEmittedModel {
		r.lastEmittedModel = model
		emit(Event{Type: "model", Content: model, Name: reason})
	}
	return model
}

func (r *Runner) systemPrompt() string {
	rules := strings.TrimSpace(r.RulesText)
	if rules == "" {
		return SystemPrompt
	}
	return SystemPrompt + "\n\n" +
		"## Cursor / project rules for this turn\n" +
		"These rules come from Cursor (.cursorrules / .cursor/rules / AGENTS.md) and were refreshed for this request. " +
		"Follow them strictly; when they conflict with this base prompt, the rules win.\n\n" +
		rules
}

func (r *Runner) retryCount() int {
	if r.RetryCount <= 0 {
		return 3
	}
	return r.RetryCount
}

func (r *Runner) retryBackoff() time.Duration {
	if r.RetryBackoff <= 0 {
		return 2 * time.Second
	}
	return r.RetryBackoff
}

// chat calls the provider and automatically retries transient network errors,
// emitting a "reconnect" event before each attempt so the UI can show progress.
func (r *Runner) chat(ctx context.Context, req *llm.ChatRequest, emit EmitFunc) (*llm.ChatResponse, error) {
	resp, err := r.Provider.ChatCompletion(ctx, req)
	for attempt := 0; err != nil && isTransientError(err) && attempt < r.retryCount(); attempt++ {
		wait := r.retryBackoff() * time.Duration(attempt+1)
		emit(Event{Type: "reconnect", Content: fmt.Sprintf(
			"Соединение с LLM потеряно (%v). Повторная попытка %d/%d через %.0f сек…",
			err, attempt+1, r.retryCount(), wait.Seconds(),
		)})
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		resp, err = r.Provider.ChatCompletion(ctx, req)
	}
	return resp, err
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	needles := []string{
		// network
		"wsarecv", "wsaetimeout", "connection attempt failed", "connection refused",
		"connection reset", "no such host", "i/o timeout", "dial tcp", "read tcp",
		"write tcp", "eof", "context deadline exceeded", "connection was forcibly closed",
		"server misbehaving", "temporary failure",
		// provider-side transient states
		"перегружена", "лимит запросов", "позднее", "rate limit",
		"temporarily unavailable", "недоступен", "overloaded",
	}
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
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
	if len(images) > 0 {
		r.HasImages = true
	}
	if r.UserText == "" {
		r.UserText = extraText.String()
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

		model := r.resolveModel(step, emit)
		req := &llm.ChatRequest{
			Messages:        CompactHistory(messages),
			Tools:           tools.Specs(),
			ToolChoice:      "auto",
			Thinking:        map[string]any{"type": "enabled"},
			ReasoningEffort: "high",
			Stream:          false,
			Model:           model,
		}
		resp, err := r.chat(ctx, req, emit)
		if err != nil {
			emit(Event{Type: "error", Content: err.Error()})
			return messages, err
		}
		r.reportUsage(billingModel(resp.Model, model), resp.Usage)
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
				messages = append(messages, llm.ToolResultMessage(call.ID, truncate(result, maxToolResultBytes)))
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
	wrapModel := r.resolveModel(max, emit)
	req := &llm.ChatRequest{
		Messages:        CompactHistory(messages),
		Thinking:        map[string]any{"type": "enabled"},
		ReasoningEffort: "high",
		Stream:          false,
		Model:           wrapModel,
	}
	resp, err := r.chat(ctx, req, emit)
	if err != nil {
		emit(Event{Type: "error", Content: fmt.Sprintf("лимит шагов (%d); финальный ответ не получен: %v", max, err)})
		return messages, err
	}
	r.reportUsage(billingModel(resp.Model, wrapModel), resp.Usage)
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
