package agent

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"notcursor.ai/app/internal/appmeta"
	"notcursor.ai/app/internal/llm"
)

// proDirectiveRE matches a leading /pro or [pro] token (case-insensitive).
var proDirectiveRE = regexp.MustCompile(`(?i)^\s*(?:/pro\b|\[pro\])[ \t]*`)

// StripProDirective removes a leading /pro or [pro] from text.
// Returns the cleaned text and whether the directive was present.
func StripProDirective(text string) (cleaned string, force bool) {
	loc := proDirectiveRE.FindStringIndex(text)
	if loc == nil {
		return text, false
	}
	cleaned = strings.TrimSpace(text[loc[1]:])
	return cleaned, true
}

// escalateTracker watches mid-run signals that Flash is stuck.
type escalateTracker struct {
	lastToolSig             string
	sameToolStreak          int
	consecutiveToolFailures int
}

func toolCallSignature(calls []llm.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range calls {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(strings.TrimSpace(c.Function.Name))
		b.WriteByte('#')
		b.WriteString(strings.TrimSpace(c.Function.Arguments))
	}
	return b.String()
}

func (t *escalateTracker) noteToolCalls(calls []llm.ToolCall) (dup bool) {
	if t == nil || len(calls) == 0 {
		return false
	}
	sig := toolCallSignature(calls)
	if sig == "" {
		return false
	}
	if sig == t.lastToolSig {
		t.sameToolStreak++
	} else {
		t.lastToolSig = sig
		t.sameToolStreak = 1
	}
	return t.sameToolStreak >= 2
}

func toolResultFailed(content string) bool {
	c := strings.TrimSpace(content)
	if c == "" {
		return false
	}
	up := strings.ToUpper(c)
	return strings.HasPrefix(up, "ERROR:") || strings.HasPrefix(up, "DENIED")
}

func (t *escalateTracker) noteToolResults(outs []llm.Message) (failStreak bool) {
	if t == nil || len(outs) == 0 {
		return false
	}
	anyFail := false
	for _, m := range outs {
		if toolResultFailed(m.Content) {
			anyFail = true
			break
		}
	}
	if anyFail {
		t.consecutiveToolFailures++
	} else {
		t.consecutiveToolFailures = 0
	}
	return t.consecutiveToolFailures >= 2
}

// escalateEnabled is DeepSeek Auto-models with a live Pro model.
func (r *Runner) escalateEnabled() bool {
	if r == nil || !r.AutoModels || !r.isDeepSeek() {
		return false
	}
	return !appmeta.DeepSeekProRetired(time.Now())
}

func (r *Runner) isDeepSeek() bool {
	p := strings.ToLower(strings.TrimSpace(r.ProviderID))
	return p == "" || p == "deepseek"
}

func (r *Runner) strongModel() string {
	return ModelPro
}

func (r *Runner) isWeakAutoModel(model string) bool {
	m := strings.TrimSpace(model)
	if m == "" {
		return true
	}
	return strings.EqualFold(m, ModelFlash) ||
		strings.EqualFold(m, "deepseek-v4-flash") ||
		strings.EqualFold(m, "deepseek-v4-flash-vision-exp")
}

// shouldBypassSticky drops a weak session sticky when the new turn looks hard or /pro.
func (r *Runner) shouldBypassSticky(sticky string) bool {
	if !r.escalateEnabled() {
		return false
	}
	if r.ForcePro {
		return true
	}
	if !r.isWeakAutoModel(sticky) {
		return false
	}
	if r.HintPathCount >= 4 || isComplexTask(r.UserText) {
		return true
	}
	return false
}

// applyProDirective strips /pro|[pro] from the user turn and sets ForcePro.
func (r *Runner) applyProDirective(userMsg *llm.Message) {
	if r == nil {
		return
	}
	if cleaned, force := StripProDirective(r.UserText); force {
		r.ForcePro = true
		r.UserText = cleaned
	}
	if userMsg == nil {
		return
	}
	if cleaned, force := StripProDirective(userMsg.Content); force {
		r.ForcePro = true
		userMsg.Content = cleaned
	}
	for i := range userMsg.Parts {
		if userMsg.Parts[i].Type != "text" {
			continue
		}
		if cleaned, force := StripProDirective(userMsg.Parts[i].Text); force {
			r.ForcePro = true
			userMsg.Parts[i].Text = cleaned
		}
	}
}

// bumpToStrong upgrades runModel to Pro for the rest of this run (one-way).
// Returns true when the model actually changed.
func (r *Runner) bumpToStrong(emit EmitFunc, reason, noticeRU string) bool {
	if !r.escalateEnabled() {
		return false
	}
	strong := r.strongModel()
	cur := strings.TrimSpace(r.runModel)
	if cur == "" {
		cur = strings.TrimSpace(r.ModelOverride)
	}
	if strings.EqualFold(cur, strong) || (!r.isWeakAutoModel(cur) && cur != "") {
		return false
	}
	r.runModel = strong
	r.ModelOverride = strong
	r.emitModel(emit, strong, reason)
	r.logf("escalate model=%s reason=%q from=%q", strong, reason, cur)
	if noticeRU != "" && emit != nil {
		emit(Event{Type: "notice", Content: noticeRU})
	}
	return true
}

func escalateNotice(reason string) string {
	switch reason {
	case "escalate-dup-tool":
		return "Поднял на DeepSeek Pro: повтор одного и того же tool call."
	case "escalate-tool-fail":
		return "Поднял на DeepSeek Pro: два провала инструментов подряд."
	case "escalate-empty":
		return "Поднял на DeepSeek Pro: пустой ответ модели."
	case "escalate-invalid-tool":
		return "Поднял на DeepSeek Pro: битый / невалидный tool call."
	case "user-pro":
		return "Режим Pro по запросу (/pro)."
	default:
		return fmt.Sprintf("Поднял на DeepSeek Pro (%s).", reason)
	}
}
