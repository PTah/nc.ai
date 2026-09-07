package agent

import (
	"strings"
	"unicode/utf8"

	"notcursor.ai/app/internal/llm/providers/deepseek"
)

// Auto model ids (DeepSeek).
const (
	ModelFlash  = deepseek.DefaultModel               // deepseek-v4-flash
	ModelPro    = "deepseek-v4-pro"
	ModelVision = deepseek.VisionModel                // deepseek-v4-flash-vision-exp
)

// RouteInput feeds the Auto-models picker.
type RouteInput struct {
	UserText      string
	HasImages     bool
	HintPathCount int
	Step          int // 0-based agent loop step
}

// RouteDecision is the chosen model plus a short reason for the UI/logs.
type RouteDecision struct {
	Model  string
	Reason string
}

// complexNeedles triggers pro for heavier coding / planning work.
var complexNeedles = []string{
	"рефактор", "refactor", "архитектур", "architecture",
	"миграц", "migration", "перепиши", "rewrite", "переписать",
	"весь проект", "across the codebase", "multiple files", "много файл",
	"спроектируй", "design a", "оптимиз", "optimize",
	"root cause", "debug why", "почему не", "security", "безопасност",
	"performance", "производительн", "concurrent", "race condition",
}

// PickModel chooses flash / pro / vision for Auto-models mode.
//
// Priority:
//  1. Any image → vision
//  2. Agent step >= 8 → escalate to pro (long tool runs)
//  3. Complex prompt / many hinted paths → pro
//  4. Otherwise → flash (cheaper default)
func PickModel(in RouteInput) RouteDecision {
	if in.HasImages {
		return RouteDecision{Model: ModelVision, Reason: "image"}
	}
	if in.Step >= 8 {
		return RouteDecision{Model: ModelPro, Reason: "long-run"}
	}
	if in.HintPathCount >= 4 || isComplexTask(in.UserText) {
		return RouteDecision{Model: ModelPro, Reason: "complex"}
	}
	return RouteDecision{Model: ModelFlash, Reason: "default"}
}

func isComplexTask(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if utf8.RuneCountInString(t) >= 800 {
		return true
	}
	lower := strings.ToLower(t)
	for _, n := range complexNeedles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}
