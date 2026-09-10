package tools

import "testing"

func TestPlanBlocked(t *testing.T) {
	if !PlanBlocked("write_file") || !PlanBlocked("run_terminal") || !PlanBlocked("move_file") {
		t.Fatal("mutating tools must be blocked in plan mode")
	}
	if PlanBlocked("grep") || PlanBlocked("read_file") || PlanBlocked("ask_user") || PlanBlocked("glob") || PlanBlocked("git_log") {
		t.Fatal("read tools must stay available")
	}
	all := SpecsFor(false)
	plan := SpecsFor(true)
	if len(plan) >= len(all) {
		t.Fatalf("plan specs %d want fewer than %d", len(plan), len(all))
	}
	names := map[string]bool{}
	for _, s := range all {
		names[s.Function.Name] = true
	}
	if names["search_files"] {
		t.Fatal("legacy search_files must not be advertised")
	}
	if !names["glob"] || !names["git_log"] || !names["move_file"] {
		t.Fatalf("missing new tools: %v", names)
	}
	for _, s := range plan {
		if PlanBlocked(s.Function.Name) {
			t.Fatalf("plan specs still has %s", s.Function.Name)
		}
	}
}

func TestTodoStoreMerge(t *testing.T) {
	s := NewTodoStore()
	items, err := s.Apply(false, []Todo{
		{ID: "1", Content: "a", Status: "pending"},
		{ID: "2", Content: "b", Status: "in_progress"},
	})
	if err != nil || len(items) != 2 {
		t.Fatalf("seed: %v %v", items, err)
	}
	items, err = s.Apply(true, []Todo{{ID: "2", Content: "b", Status: "completed"}})
	if err != nil || items[1].Status != "completed" || items[0].Status != "pending" {
		t.Fatalf("merge: %+v %v", items, err)
	}
}

func TestDangerousIncludesDelete(t *testing.T) {
	if !DangerousTool("delete_file") || !DangerousTool("apply_patch") || !DangerousTool("move_file") {
		t.Fatal("delete/patch/move should require HITL")
	}
	if ReadOnlyTool("write_file") || !ReadOnlyTool("grep") || !ReadOnlyTool("glob") || !ReadOnlyTool("git_log") {
		t.Fatal("readonly classification")
	}
}
