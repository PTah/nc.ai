package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/workspace"
)

func callFor(name, args string) llm.ToolCall {
	return llm.ToolCall{ID: name, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: args}}
}

// TestForRunPinsRootAndTodos covers the "agent works in two projects at once"
// case: a run started in project A must keep touching A after the user opens B.
func TestForRunPinsRootAndTodos(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	ws := workspace.NewManager()
	if _, err := ws.Open(rootA); err != nil {
		t.Fatalf("open: %v", err)
	}
	base := NewRegistry(ws, "")
	run := base.ForRun(rootA)
	if run == nil {
		t.Fatal("ForRun returned nil")
	}
	if run.WSRoot != rootA {
		t.Fatalf("run root = %q, want %q", run.WSRoot, rootA)
	}
	if base.WSRoot != "" {
		t.Fatal("the shared registry must stay unpinned")
	}
	if run.Todos == base.Todos {
		t.Fatal("todo store must not be shared between runs")
	}
	if run.Git == nil || run.Git.Root != rootA {
		t.Fatalf("git is not pinned: %+v", run.Git)
	}
	if base.Git.Root != "" {
		t.Fatal("the shared git service must stay unpinned")
	}

	// The user switches project while the agent is still working.
	if err := ws.SetActive(rootB); err != nil {
		t.Fatalf("set active: %v", err)
	}
	ctx := context.Background()
	if _, err := run.Execute(ctx, callFor("write_file", `{"path":"pinned.txt","content":"project A"}`)); err != nil {
		t.Fatalf("pinned write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootA, "pinned.txt")); err != nil {
		t.Fatalf("file is not in project A: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootB, "pinned.txt")); err == nil {
		t.Fatal("pinned write leaked into project B")
	}
	out, err := run.Execute(ctx, callFor("read_file", `{"path":"pinned.txt"}`))
	if err != nil || !strings.Contains(out, "project A") {
		t.Fatalf("pinned read = %q, %v", out, err)
	}

	// Plan mode lives in the run copy, so a parallel run is not affected.
	run.PlanMode = true
	if base.PlanMode {
		t.Fatal("PlanMode leaked into the shared registry")
	}
	if _, err := run.Execute(ctx, callFor("write_file", `{"path":"blocked.txt","content":"x"}`)); err == nil {
		t.Fatal("plan mode write must be blocked")
	}

	// The shared registry follows the active project: nothing was written to B.
	if _, err := base.Execute(ctx, callFor("read_file", `{"path":"pinned.txt"}`)); err == nil {
		t.Fatal("unpinned registry must read the active project, not project A")
	}
}
