package agent

import (
	"strings"
	"unicode/utf8"

	"notcursor.ai/app/internal/llm/providers/deepseek"
	"notcursor.ai/app/internal/llm/providers/openrouter"
	"notcursor.ai/app/internal/llm/providers/qwen"
	"notcursor.ai/app/internal/llm/providers/zai"
)

// Auto model ids (DeepSeek).
const (
	ModelFlash  = deepseek.DefaultModel // deepseek-flash (V4.1 Flash, multimodal)
	ModelPro    = "deepseek-v4-pro"     // legacy id, routed to V4.1 Flash
	ModelVision = deepseek.VisionModel  // deepseek-flash (native image input)
)

// Auto model ids (Z.ai).
const (
	ModelZaiFree   = zai.DefaultModel // glm-4.7-flash ($0)
	ModelZaiStrong = "glm-5.3"        // flagship for complex work
	ModelZaiVision = "glm-5.3-flash"  // multimodal + cheaper than full 5.3
)

// Auto model ids (OpenRouter / Qwen coder).
const (
	ModelORFlash  = openrouter.DefaultModel // qwen3-coder-flash:floor
	ModelORStrong = openrouter.StrongModel  // qwen3-coder:floor
	ModelORVision = openrouter.VisionModel  // qwen3-vl-8b-instruct
)

// Auto model ids (Qwen / DashScope).
const (
	ModelQwenPlus   = qwen.DefaultModel // qwen-plus (balanced)
	ModelQwenFast   = qwen.FastModel    // qwen-turbo (cheapest)
	ModelQwenStrong = qwen.StrongModel  // qwen-max (flagship)
	ModelQwenVision = qwen.VisionModel  // qwen-vl-max (multimodal)
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

// complexNeedles triggers pro / glm-5.3 for heavier coding / planning work.
var complexNeedles = []string{
	"рефактор", "refactor", "архитектур", "architecture",
	"миграц", "migration", "перепиши", "rewrite", "переписать",
	"весь проект", "across the codebase", "multiple files", "много файл",
	"спроектируй", "design a", "оптимиз", "optimize",
	"root cause", "debug why", "почему не", "security", "безопасност",
	"performance", "производительн", "concurrent", "race condition",
}

// PickModel chooses flash / pro / vision for DeepSeek Auto-models mode.
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

// PickZaiModel chooses free flash vs flagship glm-5.3 for Z.ai Auto-models.
//
// Priority:
//  1. Any image → glm-5.3-flash (vision)
//  2. Long tool run / complex prompt / many paths → glm-5.3
//  3. Otherwise → glm-4.7-flash (free)
func PickZaiModel(in RouteInput) RouteDecision {
	if in.HasImages {
		return RouteDecision{Model: ModelZaiVision, Reason: "image"}
	}
	if in.Step >= 8 {
		return RouteDecision{Model: ModelZaiStrong, Reason: "long-run"}
	}
	if in.HintPathCount >= 4 || isComplexTask(in.UserText) {
		return RouteDecision{Model: ModelZaiStrong, Reason: "complex"}
	}
	return RouteDecision{Model: ModelZaiFree, Reason: "default"}
}

// PickOpenRouterModel chooses cheap Qwen flash vs stronger coder for OpenRouter Auto.
//
// Priority:
//  1. Any image → qwen3-vl
//  2. Long tool run / complex prompt / many paths → qwen3-coder:floor
//  3. Otherwise → qwen3-coder-flash:floor
func PickOpenRouterModel(in RouteInput) RouteDecision {
	if in.HasImages {
		return RouteDecision{Model: ModelORVision, Reason: "image"}
	}
	if in.Step >= 8 {
		return RouteDecision{Model: ModelORStrong, Reason: "long-run"}
	}
	if in.HintPathCount >= 4 || isComplexTask(in.UserText) {
		return RouteDecision{Model: ModelORStrong, Reason: "complex"}
	}
	return RouteDecision{Model: ModelORFlash, Reason: "default"}
}

// PickQwenModel chooses turbo / plus / qwen-max for Qwen (DashScope) Auto-models.
//
// Priority:
//  1. Any image → qwen-vl-max
//  2. Long tool run / complex prompt / many paths → qwen-max
//  3. Otherwise → qwen-plus (balanced default)
func PickQwenModel(in RouteInput) RouteDecision {
	if in.HasImages {
		return RouteDecision{Model: ModelQwenVision, Reason: "image"}
	}
	if in.Step >= 8 {
		return RouteDecision{Model: ModelQwenStrong, Reason: "long-run"}
	}
	if in.HintPathCount >= 4 || isComplexTask(in.UserText) {
		return RouteDecision{Model: ModelQwenStrong, Reason: "complex"}
	}
	return RouteDecision{Model: ModelQwenPlus, Reason: "default"}
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
