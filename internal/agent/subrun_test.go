package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/tools"
)

func TestRunTask_Disabled(t *testing.T) {
	r := &Runner{SubagentsEnabled: false, Depth: 0}
	msg := r.runTask(context.Background(), llm.ToolCall{
		ID: "c1",
		Function: llm.FunctionCall{
			Name:      "task",
			Arguments: `{"description":"x","prompt":"y"}`,
		},
	}, func(Event) {})
	if !strings.Contains(msg.Content, "disabled") {
		t.Fatalf("want disabled error, got %q", msg.Content)
	}
}

func TestRunTask_NoRecursion(t *testing.T) {
	r := &Runner{SubagentsEnabled: true, Depth: 1}
	msg := r.runTask(context.Background(), llm.ToolCall{
		ID: "c1",
		Function: llm.FunctionCall{
			Name:      "task",
			Arguments: `{"description":"x","prompt":"y"}`,
		},
	}, func(Event) {})
	if !strings.Contains(msg.Content, "depth") {
		t.Fatalf("want depth error, got %q", msg.Content)
	}
}

func TestRunTask_MissingPrompt(t *testing.T) {
	r := &Runner{SubagentsEnabled: true, Depth: 0}
	msg := r.runTask(context.Background(), llm.ToolCall{
		ID: "c1",
		Function: llm.FunctionCall{
			Name:      "task",
			Arguments: `{"description":"x"}`,
		},
	}, func(Event) {})
	if !strings.Contains(msg.Content, "prompt") {
		t.Fatalf("want prompt error, got %q", msg.Content)
	}
}

func TestSpawnChild_NoTaskAndReadOnlySpecs(t *testing.T) {
	parent := &Runner{
		SubagentsEnabled:    true,
		SubagentsAllowWrite: false,
		SubagentsMaxSteps:   8,
		PreferredModel:      "deepseek-flash",
		ProviderID:          "deepseek",
	}
	child := parent.spawnChild("desc", 8, "auto")
	if child.Depth != 1 || child.SubagentsEnabled {
		t.Fatalf("child depth/enabled = %d/%v", child.Depth, child.SubagentsEnabled)
	}
	if !child.subagentMode {
		t.Fatal("child should use subagentMode")
	}
	specs := tools.SpecsForSubagent(false)
	for _, s := range specs {
		if s.Function.Name == "task" {
			t.Fatal("subagent specs must not include task")
		}
		if tools.SubagentWriteTool(s.Function.Name) {
			t.Fatalf("write tool leaked: %s", s.Function.Name)
		}
	}
	with := tools.WithTaskTool(tools.SpecsFor(false))
	found := false
	for _, s := range with {
		if s.Function.Name == "task" {
			found = true
		}
	}
	if !found {
		t.Fatal("WithTaskTool should append task")
	}
}

func TestRunTask_MaxPerRun(t *testing.T) {
	r := &Runner{
		SubagentsEnabled:   true,
		SubagentsMaxPerRun: 1,
		subCount:           1,
	}
	var ended bool
	msg := r.runTask(context.Background(), llm.ToolCall{
		ID: "c1",
		Function: llm.FunctionCall{
			Name:      "task",
			Arguments: `{"description":"x","prompt":"find auth"}`,
		},
	}, func(e Event) {
		if e.Type == "tool_end" {
			ended = true
		}
	})
	if !ended {
		t.Fatal("expected tool_end")
	}
	if !strings.Contains(msg.Content, "limit") {
		t.Fatalf("want limit error, got %q", msg.Content)
	}
}

func TestSubrunResultJSON(t *testing.T) {
	raw, err := json.Marshal(SubrunResult{
		Summary: "ok",
		Files:   []string{"a.go"},
		Steps:   2,
		Tokens:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got SubrunResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary != "ok" || len(got.Files) != 1 || got.Steps != 2 {
		t.Fatalf("roundtrip failed: %+v", got)
	}
}

func TestExtractJSONStringField(t *testing.T) {
	if got := extractJSONStringField(`{"path":"internal/agent/subrun.go"}`, "path"); got != "internal/agent/subrun.go" {
		t.Fatalf("got %q", got)
	}
	if got := extractJSONStringField(`not json`, "path"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}
