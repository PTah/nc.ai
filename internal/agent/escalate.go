// Edited by nc.ai 0.8.6.4 on Papasha 2026-10-16 15:43:59
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

// escalateEnabled is true when Auto-models is on and the provider has a stronger
// model to bump to: DeepSeek Pro, glm-5.3, qwen-max, qwen3-235b, qwen3-coder.
func (r *Runner) escalateEnabled() bool {
	if r == nil || !r.AutoModels {
		return false
	}
	if r.isOpenRouter() || r.isZai() || r.isQwen() || r.isKimi() || r.isYandex() {
		return true
	}
	if !r.isDeepSeek() {
		return false
	}
	return !appmeta.DeepSeekProRetired(time.Now())
}

func (r *Runner) isDeepSeek() bool {
	p := strings.ToLower(strings.TrimSpace(r.ProviderID))
	return p == "" || p == "deepseek"
}

// strongModel is the "smarter" model for the active provider: the one a stalled
// run is bumped to.
func (r *Runner) strongModel() string {
	switch {
	case r.isOpenRouter():
		return ModelORStrong
	case r.isZai():
		return ModelZaiStrong
	case r.isQwen():
		return ModelQwenStrong
	case r.isKimi():
		return ModelKimiStrong
	case r.isYandex():
		return ModelYandexStrong
	default:
		return ModelPro
	}
}

// isWeakAutoModel reports whether model is the provider's cheap default, i.e.
// the one worth replacing when the run gets stuck.
func (r *Runner) isWeakAutoModel(model string) bool {
	m := strings.TrimSpace(model)
	if m == "" {
		return true
	}
	switch {
	case r.isOpenRouter():
		base := strings.ToLower(strings.TrimSuffix(m, ":floor"))
		return base == "qwen/qwen3-coder-flash" || base == "qwen/qwen3-coder-30b-a3b-instruct"
	case r.isZai():
		return strings.EqualFold(m, ModelZaiFree)
	case r.isQwen():
		return strings.EqualFold(m, ModelQwenFast) || strings.EqualFold(m, ModelQwenPlus)
	case r.isKimi():
		return strings.EqualFold(m, ModelKimiFast)
	case r.isYandex():
		return strings.EqualFold(m, ModelYandexDefault) || strings.EqualFold(m, ModelYandexFast)
	}
	return strings.EqualFold(m, ModelFlash) ||
		strings.EqualFold(m, "deepseek-v4-flash") ||
		strings.EqualFold(m, "deepseek-v4-flash-vision-exp")
}

// providerLabel names the model a stalled run is bumped to, for the chat line.
func (r *Runner) providerLabel() string {
	switch {
	case r.isOpenRouter():
		return "OpenRouter " + ModelORStrong
	case r.isZai():
		return "Z.ai " + ModelZaiStrong
	case r.isQwen():
		return "Qwen " + ModelQwenStrong
	case r.isKimi():
		return "Kimi " + ModelKimiStrong
	case r.isYandex():
		return "Yandex " + ModelYandexStrong
	default:
		return "DeepSeek Pro"
	}
}

// escalateNoticeRU is the chat line for a mid-run bump on the active provider.
// keep: escalateNotice (below) stays the DeepSeek wording used by the tests.
func (r *Runner) escalateNoticeRU(reason string) string {
	if r == nil || r.isDeepSeek() {
		return escalateNotice(reason)
	}
	label := r.providerLabel()
	switch reason {
	case "user-pro":
		return fmt.Sprintf("Режим сильной модели (%s) по запросу (/pro).", label)
	case "escalate-dup-tool":
		return fmt.Sprintf("Поднял на %s: повтор одного и того же tool call.", label)
	case "escalate-tool-fail":
		return fmt.Sprintf("Поднял на %s: два провала инструментов подряд.", label)
	case "escalate-empty":
		return fmt.Sprintf("Поднял на %s: пустой ответ модели.", label)
	case "escalate-invalid-tool":
		return fmt.Sprintf("Поднял на %s: битый / невалидный tool call.", label)
	default:
		return fmt.Sprintf("Поднял на %s (%s).", label, reason)
	}
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
