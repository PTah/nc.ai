package agent

import (
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestBuildMessages_PrefixCacheOrder(t *testing.T) {
	r := &Runner{
		ProjectMap:     "README.md\napp.go",
		TurnRulesText:  "path-rule-xyz",
		IDEContext:     "open: app.go",
		RulesText:      "always: be brief",
	}
	prior := []llm.Message{
		{Role: "user", Content: "earlier"},
		{Role: "assistant", Content: "ok"},
	}
	user := llm.UserText("do the thing")
	msgs := r.buildMessages(prior, user)

	if len(msgs) < 6 {
		t.Fatalf("want >=6 messages, got %d: %+v", len(msgs), roles(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("first must be system, got %s", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "always: be brief") {
		t.Fatalf("stable rules belong in system: %q", msgs[0].Content[:min(80, len(msgs[0].Content))])
	}
	if !strings.Contains(msgs[1].Content, "<project_map>") {
		t.Fatalf("project_map should follow system: %q", msgs[1].Content[:min(60, len(msgs[1].Content))])
	}
	if msgs[2].Content != "earlier" || msgs[3].Content != "ok" {
		t.Fatalf("prior history misplaced: %v", roles(msgs))
	}
	if !strings.Contains(msgs[4].Content, "<turn_rules>") {
		t.Fatalf("turn_rules must come after prior: %q", msgs[4].Content)
	}
	if !strings.Contains(msgs[5].Content, "<ide_context>") {
		t.Fatalf("ide_context must come after turn_rules: %q", msgs[5].Content)
	}
	if msgs[len(msgs)-1].Content != "do the thing" {
		t.Fatalf("user must be last: %q", msgs[len(msgs)-1].Content)
	}
	// Dynamic bits must not appear before history (would bust prefix cache).
	for i, m := range msgs[:4] {
		if strings.Contains(m.Content, "<turn_rules>") || strings.Contains(m.Content, "<ide_context>") {
			t.Fatalf("dynamic content too early at index %d", i)
		}
	}
}

func roles(msgs []llm.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role
	}
	return out
}
