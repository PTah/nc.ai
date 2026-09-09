package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectForPrompt_AlwaysApplyAndCatalog(t *testing.T) {
	b := Bundle{
		Global: []Rule{{
			Name:        "cheap.mdc",
			Path:        "/g/cheap.mdc",
			AlwaysApply: true,
			Content:     "Do not add verbose comments.",
		}},
		Project: []Rule{
			{
				Name:        "api.mdc",
				Path:        "/p/api.mdc",
				Description: "API conventions",
				Globs:       "**/*.go",
				Content:     "Use chi router.",
			},
			{
				Name:        "docs.mdc",
				Path:        "/p/docs.mdc",
				Description: "Docs style guide",
				Content:     "Full docs body should stay in catalog only.",
			},
			{
				Name:    "bare.md",
				Path:    "/p/bare.md",
				Content: "Bare rule always on.",
			},
		},
	}

	out := b.SelectForPrompt(nil)
	if !strings.Contains(out, "Do not add verbose comments.") {
		t.Fatalf("alwaysApply body missing:\n%s", out)
	}
	if !strings.Contains(out, "Bare rule always on.") {
		t.Fatalf("bare rule body missing:\n%s", out)
	}
	if strings.Contains(out, "Use chi router.") {
		t.Fatalf("glob rule should not apply without hints:\n%s", out)
	}
	if strings.Contains(out, "Full docs body") {
		t.Fatalf("agent-requested full body must not be inlined:\n%s", out)
	}
	if !strings.Contains(out, "docs.mdc — Docs style guide") {
		t.Fatalf("catalog entry missing:\n%s", out)
	}

	outGo := b.SelectForPrompt([]string{"internal/api/server.go"})
	if !strings.Contains(outGo, "Use chi router.") {
		t.Fatalf("glob-matched body missing:\n%s", outGo)
	}

	stable := b.SelectStableForPrompt()
	if strings.Contains(stable, "Use chi router.") {
		t.Fatalf("stable prompt must not include glob body:\n%s", stable)
	}
	turn := b.SelectTurnRules([]string{"internal/api/server.go"})
	if !strings.Contains(turn, "Use chi router.") {
		t.Fatalf("turn rules missing glob body:\n%s", turn)
	}
	if turn == "" || strings.Contains(turn, "Bare rule always on.") {
		t.Fatalf("turn rules should be glob-only:\n%s", turn)
	}
}

func TestLoad_AgentsMDAndCursorrules(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Agents\nPrefer small diffs.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cursorrules"), []byte("Legacy always.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rulesDir := filepath.Join(root, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mdc := "---\ndescription: Only when asked\nalwaysApply: false\n---\nCatalog body.\n"
	if err := os.WriteFile(filepath.Join(rulesDir, "optional.mdc"), []byte(mdc), 0o600); err != nil {
		t.Fatal(err)
	}

	b := Load(root)
	prompt := b.SelectForPrompt(nil)
	if !strings.Contains(prompt, "Prefer small diffs.") {
		t.Fatalf("AGENTS.md missing from prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Legacy always.") {
		t.Fatalf(".cursorrules missing from prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "Catalog body.") {
		t.Fatalf("optional rule body should be catalog-only:\n%s", prompt)
	}
	if !strings.Contains(prompt, "optional.mdc — Only when asked") {
		t.Fatalf("optional catalog line missing:\n%s", prompt)
	}
}

func TestExtractHintPaths(t *testing.T) {
	paths := ExtractHintPaths("Please fix `frontend/src/App.tsx` and internal/rules/rules.go", "shot.png")
	joined := strings.Join(paths, "|")
	for _, want := range []string{"frontend/src/App.tsx", "internal/rules/rules.go", "shot.png"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, paths)
		}
	}
}

func TestMatchGlobs(t *testing.T) {
	if !matchGlobs("**/*.tsx, **/*.ts", []string{"frontend/src/App.tsx"}) {
		t.Fatal("expected tsx match")
	}
	if matchGlobs("**/*.go", []string{"README.md"}) {
		t.Fatal("did not expect go match")
	}
}
