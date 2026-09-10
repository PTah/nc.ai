package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"notcursor.ai/app/internal/appmeta"
	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/redact"
	"notcursor.ai/app/internal/tools"
)

const DefaultMaxSteps = 40

const maxToolResultBytes = 12288

const SystemPrompt = `You are NotCursor.ai, a coding agent like Cursor.
You work inside the user's local workspace.

Communication:
- Match the user's language.
- Lead with the result. Do not name tools or recap every step.
- When changing code, use tools — do not dump full files in chat unless the user asked to see the code.
- Be concise. Use backticks for file, function, and symbol names.
- Cite code as startLine:endLine:filepath when that helps the user.

Tools:
- Call independent read-only tools together in one response (glob + grep + read_file + list_dir).
- NEVER invent file paths. Confirm via list_dir, glob, find_files, or grep before read_file.
- Prefer glob for path patterns (**/*.go). find_files for fuzzy names. grep for content (regex). Do not use the shell for rg/find.
- For large files, call read_file with start_line/end_line. Output lines are numbered (N|); never copy those numbers into apply_patch or write_file.
- Prefer apply_patch for partial edits; write_file only for new files or full rewrites.
- Use delete_file for removals and move_file for renames (do not rm/mv via shell).
- Call get_env_info when OS/toolchain matters.
- Use todo_write for multi-step work; keep the list current; do not narrate todo updates.
- Use ask_user only when a real user decision is required — not for facts you can look up.

Shell:
- cwd is the workspace root unless you pass cwd (relative). Do not cd inside the command.
- Non-interactive only: pass -y/--yes; never wait for a prompt.
- No pagers. Long-running servers: is_background=true, then command_status.
- Never include newlines in command.

Git:
- Do not commit or push unless the user explicitly asked.
- Before git_commit, call git_status, git_diff, and git_log together.
- git_commit: pass paths of files you changed. Never git add .
- Never force-push or change git config.

Edits:
- Read the section you will change first.
- Follow existing style and dependencies in neighboring files; do not assume a library exists.
- Do not expand scope, add comments/docstrings, or drive-by refactors unless asked.
- Do not modify tests unless asked.
- Never hardcode API keys or secrets; point the user to env/settings.
- If linting the same file fails 3 times, stop and ask.
- After edits, call read_lints on the files you changed when diagnostics exist.

Debugging:
- Fix the root cause, not symptoms. Gather evidence (read, grep, logs) before changing code if you are not sure.

Never invent file contents — read with tools first.
After tool results, always give a final textual answer to the user.`

const PlanModePrompt = `You are in PLAN MODE.
Explore the codebase with read-only tools and ask_user if a real decision is blocked.
Do not write, patch, delete, move files, run shell, commit, push, or SSH.
When you have a concrete plan (files to touch, approach, risks), present it clearly and wait for the user to switch to Act.`

// Event is pushed to the UI during an agent run.
type Event struct {
	Type      string `json:"type"` // delta|reasoning|tool_start|tool_end|tool_ask|reconnect|done|error|persist|usage|model|notice
	Content   string `json:"content,omitempty"`
	Name      string `json:"name,omitempty"`
	OK        bool   `json:"ok,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	CallID    string `json:"callId,omitempty"`
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
	// AutoModels enables PickModel / PickZaiModel / PickOpenRouterModel routing.
	AutoModels bool
	// ProviderID is "deepseek", "zai", "openrouter", or "local" (Auto routing + image defaults).
	ProviderID string
	// Route context for AutoModels (filled by the app before Run*).
	UserText      string
	HasImages     bool
	HintPathCount int
	// RulesText is the stable rules block appended to the system prompt
	// (always-apply / catalog). Keep it turn-invariant for provider prefix cache.
	RulesText string
	// TurnRulesText is path-scoped rules for this user turn only; sent as a
	// separate message after history so it does not invalidate the system cache.
	TurnRulesText string
	// StickyModel, when AutoModels is on, reuses the session's last non-vision
	// model so consecutive turns stay on one cache namespace.
	StickyModel string
	// ProjectMap is a compact directory tree injected after the system prompt
	// (cache-friendly warm context; see docs/TODO-cache-phase5.md).
	ProjectMap string
	// IDEContext is the active editor file + cursor, injected per turn (not cached).
	IDEContext string
	// PlanMode restricts mutating tools and appends PlanModePrompt.
	PlanMode bool
	// ApproveTool, when set, is called before dangerous tools. Return false to deny.
	// The callback is responsible for asking the user (emitting tool_ask).
	ApproveTool func(ctx context.Context, callID, name, argsJSON string) (bool, error)
	// RetryCount is the number of automatic reconnect attempts for transient network errors.
	RetryCount int
	// RetryBackoff is the base wait between reconnect attempts (grows linearly per attempt).
	RetryBackoff time.Duration
	// OnUsage, when set, is called after each provider response with the model
	// that produced it and its token usage (usage may be nil for some providers).
	OnUsage func(model string, u *llm.Usage)

	lastEmittedModel string
	// runModel locks the model for the rest of this Run* after the first pick
	// (except a one-way upgrade to vision when images appear).
	runModel string
}

func (r *Runner) isZai() bool {
	return strings.EqualFold(strings.TrimSpace(r.ProviderID), "zai")
}

func (r *Runner) isOpenRouter() bool {
	return strings.EqualFold(strings.TrimSpace(r.ProviderID), "openrouter")
}

func (r *Runner) isLocal() bool {
	return strings.EqualFold(strings.TrimSpace(r.ProviderID), "local")
}

func (r *Runner) reportUsage(model string, u *llm.Usage) {
	if r.OnUsage == nil {
		return
	}
	r.OnUsage(model, u)
}

// resolveModel picks the model for this step and emits a "model" event when it changes.
// After the first pick in a run the choice is sticky (cache-friendly), except a
// one-way upgrade to the provider's vision model when the turn has images.
func (r *Runner) resolveModel(step int, emit EmitFunc) string {
	if locked := strings.TrimSpace(r.runModel); locked != "" {
		if r.HasImages && !r.isLocal() && !IsVisionModel(locked) {
			vision, reason := r.visionModel()
			if vision != "" && vision != locked {
				r.runModel = vision
				r.ModelOverride = vision
				r.emitModel(emit, vision, reason)
				return vision
			}
		}
		return locked
	}

	var model, reason string
	switch {
	case r.isLocal():
		// No Auto flash/pro catalog — always the configured local model.
		model = strings.TrimSpace(r.ModelOverride)
		if model == "" {
			model = strings.TrimSpace(r.PreferredModel)
		}
		reason = "local"
	case r.AutoModels:
		sticky := strings.TrimSpace(r.StickyModel)
		if sticky != "" && !r.HasImages && !IsVisionModel(sticky) {
			model, reason = sticky, "session-sticky"
		} else {
			var d RouteDecision
			switch {
			case r.isZai():
				d = PickZaiModel(RouteInput{
					UserText:      r.UserText,
					HasImages:     r.HasImages,
					HintPathCount: r.HintPathCount,
					Step:          step,
				})
			case r.isOpenRouter():
				d = PickOpenRouterModel(RouteInput{
					UserText:      r.UserText,
					HasImages:     r.HasImages,
					HintPathCount: r.HintPathCount,
					Step:          step,
				})
			default:
				d = PickModel(RouteInput{
					UserText:      r.UserText,
					HasImages:     r.HasImages,
					HintPathCount: r.HintPathCount,
					Step:          step,
				})
			}
			if d.Model == ModelPro && appmeta.DeepSeekProRetired(time.Now()) {
				// V4 Pro retired by the provider: route complex work to V4 Flash.
				d = RouteDecision{Model: ModelFlash, Reason: d.Reason + "-pro-retired"}
			}
			model, reason = d.Model, d.Reason
		}
	case r.HasImages:
		model, reason = r.visionModel()
	case strings.TrimSpace(r.ModelOverride) != "":
		model = strings.TrimSpace(r.ModelOverride)
	case strings.TrimSpace(r.PreferredModel) != "":
		model = strings.TrimSpace(r.PreferredModel)
	default:
		switch {
		case r.isZai():
			model = ModelZaiFree
		case r.isOpenRouter():
			model = ModelORFlash
		default:
			model = ModelFlash
		}
	}
	r.runModel = model
	r.ModelOverride = model
	r.emitModel(emit, model, reason)
	return model
}

func (r *Runner) visionModel() (model, reason string) {
	switch {
	case r.isLocal():
		m := strings.TrimSpace(r.PreferredModel)
		if m == "" {
			m = strings.TrimSpace(r.ModelOverride)
		}
		return m, "local"
	case r.isZai():
		return ModelZaiVision, "image"
	case r.isOpenRouter():
		return ModelORVision, "image"
	default:
		return ModelVision, "image"
	}
}

func (r *Runner) emitModel(emit EmitFunc, model, reason string) {
	if model == "" || model == r.lastEmittedModel {
		return
	}
	r.lastEmittedModel = model
	emit(Event{Type: "model", Content: model, Name: reason})
}

// IsVisionModel reports models used only for multimodal turns.
func IsVisionModel(model string) bool {
	switch strings.TrimSpace(model) {
	case ModelVision, ModelZaiVision, ModelORVision:
		return true
	default:
		return false
	}
}

func (r *Runner) systemPrompt() string {
	base := SystemPrompt
	if r.PlanMode {
		base += "\n\n" + PlanModePrompt
	}
	rules := strings.TrimSpace(r.RulesText)
	if rules == "" {
		return base
	}
	return base + "\n\n" +
		"## Cursor / project rules (stable)\n" +
		"These rules come from Cursor (.cursorrules / .cursor/rules / AGENTS.md). " +
		"Follow them strictly; when they conflict with this base prompt, the rules win. " +
		"Path-scoped turn rules, if any, arrive in a later user message.\n\n" +
		rules
}

// LockedModel returns the model locked for the current/last run (may be empty).
func (r *Runner) LockedModel() string {
	return strings.TrimSpace(r.runModel)
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

	prior := make([]llm.Message, 0, len(history))
	for _, m := range history {
		if m.Role == "system" {
			continue
		}
		prior = append(prior, m)
	}
	// Compact once between user turns. Mutating tool results mid-run would
	// invalidate the provider prefix cache on every agent step.
	prior = CompactHistory(prior)

	messages := make([]llm.Message, 0, len(prior)+8)
	messages = append(messages, llm.Message{Role: "system", Content: r.systemPrompt()})
	if tree := strings.TrimSpace(r.ProjectMap); tree != "" {
		messages = append(messages, llm.UserText(
			"<project_map>\n"+tree+"\n</project_map>\n"+
				"Compressed workspace tree (names only). Use glob/find_files/grep/list_dir/read_file for details.",
		))
	}
	messages = append(messages, prior...)
	if turn := strings.TrimSpace(r.TurnRulesText); turn != "" {
		messages = append(messages, llm.UserText(
			"<turn_rules>\n"+turn+"\n</turn_rules>\n"+
				"Apply these path-scoped rules for this turn in addition to the stable system rules.",
		))
	}
	if ide := strings.TrimSpace(r.IDEContext); ide != "" {
		messages = append(messages, llm.UserText(
			"<ide_context>\n"+ide+"\n</ide_context>\n"+
				"Active editor snapshot for this turn. Prefer these files when they are relevant.",
		))
	}
	messages = append(messages, userMsg)

	if r.Tools != nil {
		r.Tools.PlanMode = r.PlanMode
	}

	for step := 0; step < max; step++ {
		if ctx.Err() != nil {
			emit(Event{Type: "error", Content: ctx.Err().Error()})
			return messages, ctx.Err()
		}

		model := r.resolveModel(step, emit)
		req := &llm.ChatRequest{
			Messages:        messages,
			Tools:           tools.SpecsFor(r.PlanMode),
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
			outs, err := r.execTools(ctx, msg.ToolCalls, emit)
			if err != nil {
				emit(Event{Type: "error", Content: err.Error()})
				return messages, err
			}
			messages = append(messages, outs...)
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
		Messages:        messages,
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

func (r *Runner) execTools(ctx context.Context, calls []llm.ToolCall, emit EmitFunc) ([]llm.Message, error) {
	for _, call := range calls {
		if call.ID == "" {
			return nil, fmt.Errorf("tool_call without id")
		}
	}
	parallel := len(calls) > 1
	for _, call := range calls {
		if !tools.ReadOnlyTool(call.Function.Name) {
			parallel = false
			break
		}
	}
	outs := make([]llm.Message, len(calls))
	if !parallel {
		for i, call := range calls {
			outs[i] = r.execOne(ctx, call, emit)
		}
		return outs, nil
	}
	var mu sync.Mutex
	safe := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		emit(e)
	}
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			outs[i] = r.execOne(ctx, call, safe)
		}(i, call)
	}
	wg.Wait()
	return outs, nil
}

func (r *Runner) execOne(ctx context.Context, call llm.ToolCall, emit EmitFunc) llm.Message {
	name := call.Function.Name
	emit(Event{Type: "tool_start", Name: name, Content: call.Function.Arguments, CallID: call.ID})
	if tools.DangerousTool(name) && r.ApproveTool != nil {
		allow, err := r.ApproveTool(ctx, call.ID, name, call.Function.Arguments)
		if err != nil {
			result := redact.String(fmt.Sprintf("ERROR: approval failed: %v", err))
			emit(Event{Type: "tool_end", Name: name, Content: truncate(result, 4000), OK: false, CallID: call.ID})
			return llm.ToolResultMessage(call.ID, truncate(result, maxToolResultBytes))
		}
		if !allow {
			result := "DENIED by user. Do not retry the same dangerous action unless the user explicitly asks; explain what you intended."
			emit(Event{Type: "tool_end", Name: name, Content: result, OK: false, CallID: call.ID})
			return llm.ToolResultMessage(call.ID, result)
		}
	}
	result, execErr := r.Tools.Execute(ctx, call)
	ok := execErr == nil
	if execErr != nil {
		result = fmt.Sprintf("ERROR: %v", execErr)
	}
	result = redact.String(result)
	if name == "todo_write" && ok {
		emit(Event{Type: "todos", Content: result, CallID: call.ID})
	}
	emit(Event{Type: "tool_end", Name: name, Content: truncate(result, 4000), OK: ok, CallID: call.ID})
	return llm.ToolResultMessage(call.ID, truncate(result, maxToolResultBytes))
}

// billingModel prefers the model id DeepSeek returned; falls back to the
// request override so costing still keys the right rate card.
func billingModel(respModel, override string) string {
	if strings.TrimSpace(respModel) != "" {
		return respModel
	}
	return override
}
