// Edited by nc.ai 0.9.4 on new-admin-pc 2026-10-30 09:18:24
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"notcursor.ai/app/internal/llm"
)

// Subagent system prompt: short, forces a structured wrap-up for the parent.
const SubagentPrompt = `You are a nested subagent of NotCursor.ai.
Explore the workspace with the tools you have and answer the parent's assignment.
Rules:
- Match the user's language.
- Be concise. Prefer facts and file paths over essays.
- NEVER call task (you cannot nest further).
- Do not invent file contents — read with tools.
- When done, write a short prose answer (findings, file paths with line hints, what the parent should do next).
- Then, on its own line, emit a fenced JSON block with structured findings:
  ` + "```json" + `
  {"findings":[{"category":"bug|security|perf|style","severity":"low|medium|high","file":"path:line","message":"..."}]}
  ` + "```" + `
  List only real findings; an empty list is fine. Do not invent issues.
Lead with the result.`

// SubrunFinding is one structured issue a subagent reports to the parent.
type SubrunFinding struct {
	Category string `json:"category,omitempty"`
	Severity string `json:"severity,omitempty"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message,omitempty"`
}

// taskArgs is the JSON payload of the parent "task" tool.
type taskArgs struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model"`
}

// SubrunResult is what the parent sees as the tool result (JSON).
type SubrunResult struct {
	Summary  string          `json:"summary"`
	Files    []string        `json:"files"`
	Findings []SubrunFinding `json:"findings,omitempty"`
	Steps    int             `json:"steps"`
	Tokens   int             `json:"tokens"`
	Error    string          `json:"error,omitempty"`
}

// runTask executes a nested agent run and returns a tool result message for the parent.
func (r *Runner) runTask(ctx context.Context, call llm.ToolCall, emit EmitFunc) llm.Message {
	name := call.Function.Name
	fail := func(out string) llm.Message {
		emit(Event{Type: "tool_start", Name: name, Content: call.Function.Arguments, CallID: call.ID})
		emit(Event{Type: "tool_end", Name: name, Content: out, OK: false, CallID: call.ID})
		return llm.ToolResultMessage(call.ID, out)
	}
	if !r.SubagentsEnabled {
		return fail("ERROR: subagents are disabled. Enable Settings → Agent → Subagents.")
	}
	if r.Depth > 0 {
		return fail("ERROR: nested task is not allowed (depth limit 1).")
	}

	var args taskArgs
	raw := strings.TrimSpace(call.Function.Arguments)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return fail(fmt.Sprintf("ERROR: invalid task arguments: %v", err))
		}
	}
	desc := strings.TrimSpace(args.Description)
	prompt := strings.TrimSpace(args.Prompt)
	if desc == "" {
		desc = "подагент"
	}
	if prompt == "" {
		return fail("ERROR: task.prompt is required")
	}

	maxPer := r.SubagentsMaxPerRun
	if maxPer <= 0 {
		maxPer = 3
	}
	// Serialize nested runs (Local usually has one model slot) and enforce the cap.
	r.subMu.Lock()
	defer r.subMu.Unlock()
	if r.subCount >= maxPer {
		out := fmt.Sprintf("ERROR: subagent limit reached (%d per run). Summarize with what you have.", maxPer)
		emit(Event{Type: "tool_start", Name: name, Content: call.Function.Arguments, CallID: call.ID})
		emit(Event{Type: "tool_end", Name: name, Content: out, OK: false, CallID: call.ID})
		return llm.ToolResultMessage(call.ID, out)
	}
	r.subCount++

	emit(Event{Type: "tool_start", Name: name, Content: call.Function.Arguments, CallID: call.ID})
	emit(Event{
		Type:         "subrun_start",
		Name:         desc,
		Content:      prompt,
		CallID:       call.ID,
		ParentCallID: call.ID,
	})

	maxSteps := r.SubagentsMaxSteps
	if maxSteps <= 0 {
		maxSteps = 12
	}

	var (
		steps   int
		tokens  int
		files   []string
		fileSet = map[string]struct{}{}
		summary strings.Builder
	)

	childEmit := func(e Event) {
		e.ParentCallID = call.ID
		switch e.Type {
		case "done", "persist", "error", "tool_ask", "progress":
			// Keep parent UI from finishing / prompting on child lifecycle.
			if e.Type == "error" && strings.TrimSpace(e.Content) != "" {
				emit(Event{
					Type:         "subrun_step",
					Name:         "error",
					Content:      e.Content,
					CallID:       call.ID,
					ParentCallID: call.ID,
					OK:           false,
				})
			}
			return
		case "delta":
			summary.WriteString(e.Content)
			emit(Event{
				Type:         "subrun_delta",
				Content:      e.Content,
				CallID:       call.ID,
				ParentCallID: call.ID,
			})
			return
		case "tool_start":
			steps++
			notePathFromTool(e.Name, e.Content, fileSet, &files)
			emit(Event{
				Type:         "subrun_step",
				Name:         e.Name,
				Content:      e.Content,
				CallID:       call.ID,
				ParentCallID: call.ID,
			})
			return
		case "tool_end":
			notePathFromTool(e.Name, e.Content, fileSet, &files)
			// Не шлём второй subrun_step на каждый end (ok omitempty путает UI).
			// Только ошибки — чтобы карточка могла пометиться fail.
			if !e.OK {
				emit(Event{
					Type:         "subrun_step",
					Name:         "error",
					Content:      truncate(e.Content, 500),
					CallID:       call.ID,
					ParentCallID: call.ID,
				})
			}
			return
		case "usage":
			tokens += roughUsageTokens(e.Content)
			emit(e)
			return
		default:
			emit(e)
		}
	}

	child := r.spawnChild(desc, maxSteps, strings.TrimSpace(args.Model))
	_, runErr := child.Run(ctx, nil, prompt, childEmit)

	sum := strings.TrimSpace(summary.String())
	if sum == "" && runErr != nil {
		sum = runErr.Error()
	}
	if sum == "" {
		sum = "(подагент не вернул текст — смотри шаги выше)"
	}
	// Структурированный хвост findings[] вынимаем из прозы, чтобы родитель
	// получал и читаемый summary, и машинный список замечаний.
	sum, findings := extractFindings(sum)
	// Cap what the parent re-ingests.
	if len(sum) > 6000 {
		sum = sum[:6000] + "…"
	}

	result := SubrunResult{
		Summary:  sum,
		Files:    files,
		Findings: findings,
		Steps:    steps,
		Tokens:  tokens,
	}
	if runErr != nil && ctx.Err() == nil {
		result.Error = runErr.Error()
	}
	if ctx.Err() != nil {
		result.Error = ctx.Err().Error()
	}

	payload, _ := json.Marshal(result)
	ok := result.Error == ""
	emit(Event{
		Type:         "subrun_end",
		Name:         desc,
		Content:      string(payload),
		OK:           ok,
		CallID:       call.ID,
		ParentCallID: call.ID,
	})
	emit(Event{Type: "tool_end", Name: name, Content: truncate(string(payload), 4000), OK: ok, CallID: call.ID})
	return llm.ToolResultMessage(call.ID, truncate(string(payload), maxToolResultBytes))
}

func (r *Runner) spawnChild(desc string, maxSteps int, modelOverride string) *Runner {
	child := &Runner{
		Provider:            r.Provider,
		Tools:               r.Tools,
		MaxSteps:            maxSteps,
		WarnSteps:           -1, // silent inside nested run
		StallLimit:          r.StallLimit,
		RetryCount:          r.RetryCount,
		RetryBackoff:        r.RetryBackoff,
		RetryBudget:         r.RetryBudget,
		OnNetworkRetry:      r.OnNetworkRetry,
		Logf:                r.Logf,
		RulesText:           r.RulesText,
		ProjectMap:          r.ProjectMap,
		PlanMode:            false, // tool allowlist handles writes; PlanModePrompt not wanted
		AutoModels:          r.AutoModels,
		LocalLite:           true, // cheap nested explore
		LocalModels:         r.LocalModels,
		LocalNumCtx:         r.LocalNumCtx,
		LocalServerKind:     r.LocalServerKind,
		ProviderID:          r.ProviderID,
		PreferredModel:      r.PreferredModel,
		StickyModel:         r.StickyModel,
		UserText:            desc,
		ApproveTool:         r.ApproveTool,
		OnUsage:             r.OnUsage,
		SubagentsEnabled:    false, // child must not see task
		SubagentsMaxSteps:   maxSteps,
		SubagentsAllowWrite: r.SubagentsAllowWrite,
		Depth:               1,
		subagentMode:        true,
	}
	if modelOverride != "" && !strings.EqualFold(modelOverride, "auto") {
		child.AutoModels = false
		child.PreferredModel = modelOverride
		child.ModelOverride = modelOverride
		child.StickyModel = modelOverride
	}
	return child
}

func notePathFromTool(name, argsOrResult string, set map[string]struct{}, files *[]string) {
	path := ""
	switch name {
	case "read_file", "write_file", "apply_patch", "delete_file", "list_dir", "grep", "glob", "find_files":
		path = extractJSONStringField(argsOrResult, "path")
		if path == "" {
			path = extractJSONStringField(argsOrResult, "pattern")
		}
		if path == "" {
			path = extractJSONStringField(argsOrResult, "query")
		}
	}
	path = strings.TrimSpace(path)
	if path == "" || len(path) > 200 {
		return
	}
	if _, ok := set[path]; ok {
		return
	}
	set[path] = struct{}{}
	*files = append(*files, path)
}

func extractJSONStringField(raw, key string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '{' {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

// extractFindings отрезает от прозы необязательный fenced JSON-блок с findings[]
// и возвращает чистый summary + разобранные замечания. Если блока нет — отдаёт
// текст как есть.
func extractFindings(text string) (string, []SubrunFinding) {
	if idx := strings.LastIndex(text, "```json"); idx >= 0 {
		rest := text[idx+len("```json"):]
		if end := strings.Index(rest, "```"); end >= 0 {
			if fs, ok := parseFindingsJSON(strings.TrimSpace(rest[:end])); ok {
				clean := strings.TrimSpace(text[:idx] + rest[end+len("```"):])
				return clean, fs
			}
		}
	}
	if i := strings.LastIndex(text, `{"findings"`); i >= 0 {
		if fs, ok := parseFindingsJSON(strings.TrimSpace(text[i:])); ok {
			return strings.TrimSpace(text[:i]), fs
		}
	}
	return text, nil
}

func parseFindingsJSON(raw string) ([]SubrunFinding, bool) {
	var wrap struct {
		Findings []SubrunFinding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(raw), &wrap); err != nil {
		return nil, false
	}
	return wrap.Findings, true
}

// roughUsageTokens parses the compact usage payload from OnUsage→emit if present.
// When usage is only via OnUsage callback, tokens stay 0 — acceptable for MVP.
func roughUsageTokens(content string) int {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return 0
	}
	n := 0
	for _, k := range []string{"inputTokens", "outputTokens", "prompt_tokens", "completion_tokens"} {
		switch v := m[k].(type) {
		case float64:
			n += int(v)
		case int:
			n += v
		}
	}
	return n
}
