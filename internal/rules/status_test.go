package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Разбивка объясняет, почему правило применено или нет — это то, что видит
// пользователь в окне «Правила».
func TestStatuses(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(rulesDir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("always.mdc", "---\nalwaysApply: true\n---\n# Always\n")
	write("bare.mdc", "# Без frontmatter\n")
	write("glob-go.mdc", "---\ndescription: Go style\nglobs: \"**/*.go\"\n---\n# Go\n")
	write("glob-ts.mdc", "---\ndescription: TS style\nglobs: \"**/*.ts\"\n---\n# TS\n")
	write("propose-project-ops.mdc", "---\ndescription: Предложить ops\n---\n# ops\n")
	write("AGENTS.md", "# AGENTS\n")

	b := Load(dir)
	// Подсказка про .go — правило про Go должно подключиться к этому запросу.
	statuses := b.Statuses(ExtractHintPaths("поправь internal/agent/agent.go"))

	byName := map[string]Status{}
	for _, st := range statuses {
		byName[st.Name] = st
	}
	check := func(name, state string, wantReason string) {
		t.Helper()
		st, ok := byName[name]
		if !ok {
			t.Fatalf("правило %s не найдено в разбивке", name)
		}
		if st.State != state {
			t.Errorf("%s: state=%q, ожидали %q (%s)", name, st.State, state, st.Reason)
		}
		if wantReason != "" && !strings.Contains(st.Reason, wantReason) {
			t.Errorf("%s: reason=%q, ожидали упоминание %q", name, st.Reason, wantReason)
		}
	}
	check("always.mdc", "always", "alwaysApply")
	check("bare.mdc", "always", "frontmatter")
	check("AGENTS.md", "always", "AGENTS.md")
	check("glob-go.mdc", "turn", "**/*.go")
	check("glob-ts.mdc", "catalog", "**/*.ts")
	check("propose-project-ops.mdc", "skipped", "мета-правило")

	// Без подсказок glob-правила не подключаются — но остаются в каталоге.
	statuses = b.Statuses(nil)
	turn := 0
	for _, st := range statuses {
		if st.State == "turn" {
			turn++
		}
	}
	if turn != 0 {
		t.Fatalf("без подсказок не должно быть turn-правил, получили %d", turn)
	}
}
