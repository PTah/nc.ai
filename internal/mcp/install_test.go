package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEnsureInstalled_NpxNoDownload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, res, err := EnsureInstalled(ctx, "codegraph")
	if err != nil {
		t.Fatalf("npx preset should not fail without network if npx exists: %v", err)
	}
	if cfg.Command != "npx" || len(cfg.Args) < 2 {
		t.Fatalf("cfg=%+v", cfg)
	}
	if !res.OK {
		t.Fatalf("res=%+v", res)
	}
}

func TestEnsureInstalled_PipAlreadyOrInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg, res, err := EnsureInstalled(ctx, "code-review-graph")
	if err != nil {
		t.Skipf("pip/binary unavailable in CI: %v", err)
	}
	if !res.OK {
		t.Fatalf("res=%+v", res)
	}
	if cfg.Command == "" {
		t.Fatal("empty command")
	}
	if !strings.Contains(strings.ToLower(cfg.Command), "code-review-graph") && cfg.Command != "code-review-graph" {
		// absolute path ending with binary is ok
		if !strings.HasSuffix(strings.ToLower(cfg.Command), "code-review-graph.exe") &&
			!strings.HasSuffix(strings.ToLower(cfg.Command), "code-review-graph") {
			t.Fatalf("unexpected command %q", cfg.Command)
		}
	}
	if len(cfg.Args) == 0 || cfg.Args[0] != "serve" {
		t.Fatalf("args=%v want [serve]", cfg.Args)
	}
}

func TestPresetCommandsMatchInstall(t *testing.T) {
	for _, p := range Presets() {
		if p.Install.Kind == "" {
			t.Fatalf("preset %s missing Install", p.ID)
		}
		if p.UsageHint == "" {
			t.Fatalf("preset %s missing UsageHint", p.ID)
		}
	}
}
