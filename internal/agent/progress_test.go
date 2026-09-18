package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"notcursor.ai/app/internal/llm"
)

func toolCall(name, args string) llm.ToolCall {
	return llm.ToolCall{ID: name, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: args}}
}

func decodeProgress(t *testing.T, raw string) ProgressInfo {
	t.Helper()
	if raw == "" {
		t.Fatal("empty progress payload")
	}
	var info ProgressInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatalf("progress payload is not JSON: %v (%q)", err, raw)
	}
	if info.Text == "" {
		t.Fatalf("progress text is empty: %q", raw)
	}
	return info
}

func TestProgressMilestoneAfterTools(t *testing.T) {
	p := newRunProgress(120, "Перепроверь MAC-таблицы на всех EdgeSwitch\nвторая строка")
	defer p.stopRun()
	p.beginStep(4)

	if text, due := p.noteTools([]llm.ToolCall{toolCall("read_file", `{"path":"app.go"}`)}); due {
		t.Fatalf("single tool must not emit a milestone: %q", text)
	}
	// Three finished tools in one batch are worth reporting.
	p.setCurrent("run_terminal", `{"command":"ping 10.0.0.1"}`)
	text, due := p.noteTools([]llm.ToolCall{
		toolCall("read_file", `{"path":"internal/agent/agent.go"}`),
		toolCall("grep", `{"query":"EdgeSwitch"}`),
		toolCall("run_terminal", `{"command":"ping 10.0.0.1"}`),
	})
	if !due {
		t.Fatal("three tools must emit a milestone")
	}
	info := decodeProgress(t, text)
	if info.Phase != "step" || info.Step != 4 || info.Total != 120 || info.Tools != 4 {
		t.Fatalf("unexpected milestone: %+v", info)
	}
	if !strings.Contains(info.Done, "чтение ×3") || !strings.Contains(info.Done, "команды ×1") {
		t.Fatalf("done summary misses categories: %q", info.Done)
	}
	if !strings.Contains(info.Text, "Сейчас:") {
		t.Fatalf("milestone text has no current action: %q", info.Text)
	}
	if strings.Contains(info.Text, "вторая строка") {
		t.Fatalf("task leaked past the first line: %q", info.Text)
	}
}

func TestProgressCurrentActionAndStart(t *testing.T) {
	p := newRunProgress(120, "проверь порты")
	defer p.stopRun()
	p.setCurrent("apply_patch", `{"path":"frontend/src/App.tsx"}`)
	p.beginStep(1)

	info := decodeProgress(t, p.payload("start"))
	if info.Phase != "start" || info.Step != 1 {
		t.Fatalf("start payload = %+v", info)
	}
	if !strings.Contains(info.Text, "проверь порты") {
		t.Fatalf("start payload lost the task: %q", info.Text)
	}
	if !strings.Contains(info.Current, "apply_patch") || !strings.Contains(info.Current, "App.tsx") {
		t.Fatalf("current = %q", info.Current)
	}

	hb := decodeProgress(t, p.payload("heartbeat"))
	if hb.Phase != "heartbeat" || !strings.Contains(hb.Text, "Работа идёт") {
		t.Fatalf("heartbeat payload = %+v", hb)
	}
}

func TestProgressStopsEmitting(t *testing.T) {
	p := newRunProgress(120, "x")
	p.beginStep(2)
	p.noteTools([]llm.ToolCall{toolCall("grep", `{}`), toolCall("grep", `{}`), toolCall("grep", `{}`)})
	p.stopRun()
	p.stopRun() // must be idempotent
	if payload := p.payload("heartbeat"); payload != "" {
		t.Fatalf("payload after stop = %q, want empty", payload)
	}
	if text, due := p.noteTools([]llm.ToolCall{toolCall("grep", `{}`), toolCall("grep", `{}`), toolCall("grep", `{}`)}); due || text != "" {
		t.Fatalf("noteTools after stop = %q, %v", text, due)
	}
}

func TestProgressUnlimitedStepLabel(t *testing.T) {
	p := newRunProgress(0, "долгая задача")
	defer p.stopRun()
	p.beginStep(42)
	info := decodeProgress(t, p.payload("step"))
	if info.Total != 0 {
		t.Fatalf("unlimited run reports total = %d, want 0", info.Total)
	}
	if !strings.Contains(info.Text, "без лимита") {
		t.Fatalf("unlimited step label = %q", info.Text)
	}
	if start := decodeProgress(t, p.payload("start")); !strings.Contains(start.Text, "без лимита шагов") {
		t.Fatalf("unlimited start label = %q", start.Text)
	}
}

func TestStepWarning(t *testing.T) {
	unlimited := stepWarning(240, 100000, true, 120)
	if !strings.Contains(unlimited, "лимит шагов не задан") || !strings.Contains(unlimited, "240") {
		t.Fatalf("unlimited warning = %q", unlimited)
	}
	capped := stepWarning(120, 120, false, 120)
	if !strings.Contains(capped, "из 120") {
		t.Fatalf("capped warning = %q", capped)
	}
}

func TestToolCategoryAndHint(t *testing.T) {
	cases := map[string]string{
		"read_file":    "чтение",
		"apply_patch":  "правки",
		"run_terminal": "команды",
		"git_status":   "git",
		"ssh_exec":     "ssh",
		"web_search":   "web",
		"todo_write":   "план",
		"whatever":     "инструменты",
	}
	for name, want := range cases {
		if got := toolCategory(name); got != want {
			t.Fatalf("toolCategory(%q) = %q, want %q", name, got, want)
		}
	}
	if got := toolArgHint("read_file", `{"path":"a/b.go"}`); got != "a/b.go" {
		t.Fatalf("toolArgHint(path) = %q", got)
	}
	if got := toolArgHint("grep", `{"query":"EdgeSwitch"}`); got != "EdgeSwitch" {
		t.Fatalf("toolArgHint(query) = %q", got)
	}
	if got := toolArgHint("read_file", "not json"); got != "not json" {
		t.Fatalf("toolArgHint(raw) = %q", got)
	}
	if got := toolArgHint("read_file", "  "); got != "" {
		t.Fatalf("toolArgHint(empty) = %q", got)
	}
	if got := firstLine("a\nb", 10); got != "a" {
		t.Fatalf("firstLine multiline = %q", got)
	}
	if got := firstLine(strings.Repeat("я", 200), 10); len([]rune(got)) != 11 {
		t.Fatalf("firstLine truncation = %q", got)
	}
	if got := fmtElapsed(time.Duration(3725) * time.Second); got != "1:02:05" {
		t.Fatalf("fmtElapsed = %q", got)
	}
	if got := fmtElapsed(75 * time.Second); got != "1:15" {
		t.Fatalf("fmtElapsed(short) = %q", got)
	}
}
