// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:29:11
// Created by nc.ai 1.1.16 on macbook 2026-11-11
package agent

import (
	"strings"
	"testing"
)

// Правила E4 (краткость) и «отвечай на языке пользователя» должны жить во всех
// трёх системных промптах и не спорить друг с другом: язык задаёт язык, а не
// длину, и наоборот. Тест ловит случайное удаление правила при правках промпта.
func TestPromptsKeepBrevityAndLanguageRules(t *testing.T) {
	cases := []struct {
		name    string
		prompt  string
		markers []string
	}{
		{
			name:   "SystemPrompt",
			prompt: SystemPrompt,
			markers: []string{
				"Match the user's language",
				"Lead with the result",
				"Be concise",
				"Prefer a diff/patch",
			},
		},
		{
			name:   "SystemPromptLite",
			prompt: SystemPromptLite,
			markers: []string{
				"Match the user's language",
				"Be brief",
				"Lead with the result",
			},
		},
		{
			name:   "SubagentPrompt",
			prompt: SubagentPrompt,
			markers: []string{
				"Match the user's language",
				"Be concise",
				"Lead with the result",
			},
		},
		{
			name:   "PlanModePrompt",
			prompt: PlanModePrompt,
			markers: []string{
				"plan",
			},
		},
	}
	for _, c := range cases {
		for _, m := range c.markers {
			if !strings.Contains(c.prompt, m) {
				t.Errorf("%s: пропало правило %q", c.name, m)
			}
		}
	}
}

// Просьба «думай пошагово, расписывай подробно» в системном промпте конфликтует
// с краткостью: в отладке reasoning это ещё и сбивает модели (DeepSeek R1).
func TestPromptsHaveNoVerbosityInstructions(t *testing.T) {
	forbidden := []string{
		"Think step by step",
		"think step-by-step",
		"Let's think step by step",
		"explain every step",
		"распиши подробно",
		"объясняй каждый шаг",
	}
	for name, prompt := range map[string]string{
		"SystemPrompt":     SystemPrompt,
		"SystemPromptLite": SystemPromptLite,
		"SubagentPrompt":   SubagentPrompt,
	} {
		for _, f := range forbidden {
			if strings.Contains(prompt, f) {
				t.Errorf("%s: противоречит краткости — найдено %q", name, f)
			}
		}
	}
}
