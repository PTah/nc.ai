package agent

import (
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/tools"
)

func toolCallsNamed(names ...string) []llm.ToolCall {
	out := make([]llm.ToolCall, 0, len(names))
	for i, n := range names {
		out = append(out, llm.ToolCall{ID: string(rune('a' + i)), Function: llm.FunctionCall{Name: n}})
	}
	return out
}

func runnerWithTodos(items []tools.Todo) *Runner {
	st := tools.NewTodoStore()
	if len(items) > 0 {
		_, _ = st.Apply(false, items)
	}
	return &Runner{Tools: &tools.Registry{Todos: st}}
}

func TestTodoStaleReminderFiresAfterIdleSteps(t *testing.T) {
	r := runnerWithTodos([]tools.Todo{
		{ID: "1", Content: "прочитать файлы", Status: "pending"},
		{ID: "2", Content: "написать описание", Status: "pending"},
	})
	calls := toolCallsNamed("read_file")
	for i := 1; i < todoIdleSteps; i++ {
		if got := r.todoStaleReminder(calls); got != "" {
			t.Fatalf("step %d: unexpected reminder %q", i, got)
		}
	}
	got := r.todoStaleReminder(calls)
	if got == "" || !strings.Contains(got, "todo_write") {
		t.Fatalf("reminder=%q", got)
	}
	// Второе напоминание — ещё через todoIdleSteps шагов, дальше молчим.
	for i := 0; i < todoIdleSteps-1; i++ {
		if got := r.todoStaleReminder(calls); got != "" {
			t.Fatalf("unexpected early reminder %q", got)
		}
	}
	if got := r.todoStaleReminder(calls); got == "" {
		t.Fatal("second reminder expected")
	}
	for i := 0; i < todoIdleSteps*2; i++ {
		if got := r.todoStaleReminder(calls); got != "" {
			t.Fatalf("reminder limit exceeded: %q", got)
		}
	}
}

func TestTodoStaleReminderResetsOnTodoWrite(t *testing.T) {
	r := runnerWithTodos([]tools.Todo{{ID: "1", Content: "шаг", Status: "pending"}})
	calls := toolCallsNamed("read_file")
	for i := 0; i < todoIdleSteps-1; i++ {
		r.todoStaleReminder(calls)
	}
	if got := r.todoStaleReminder(toolCallsNamed("read_file", "todo_write")); got != "" {
		t.Fatalf("todo_write must reset, got %q", got)
	}
	if r.todoIdle != 0 || r.todoNudges != 0 {
		t.Fatalf("counter=%d nudges=%d want 0/0", r.todoIdle, r.todoNudges)
	}
}

func TestTodoStaleReminderSilentWhenDone(t *testing.T) {
	r := runnerWithTodos([]tools.Todo{
		{ID: "1", Content: "шаг", Status: "completed"},
		{ID: "2", Content: "шаг", Status: "cancelled"},
	})
	calls := toolCallsNamed("read_file")
	for i := 0; i < todoIdleSteps*2; i++ {
		if got := r.todoStaleReminder(calls); got != "" {
			t.Fatalf("closed list must stay silent, got %q", got)
		}
	}
}

func TestTodoStaleReminderWithoutTodos(t *testing.T) {
	r := &Runner{}
	calls := toolCallsNamed("read_file")
	for i := 0; i < todoIdleSteps*2; i++ {
		if got := r.todoStaleReminder(calls); got != "" {
			t.Fatalf("no list must stay silent, got %q", got)
		}
	}
}
