package agent

import (
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestStripProDirective(t *testing.T) {
	cases := []struct {
		in, want string
		force    bool
	}{
		{"/pro сделай рефакторинг", "сделай рефакторинг", true},
		{"[pro] rewrite auth", "rewrite auth", true},
		{"/PRO\tfix it", "fix it", true},
		{"просто текст", "просто текст", false},
		{"см. /pro в доке", "см. /pro в доке", false},
		{"/pro", "", true},
	}
	for _, tc := range cases {
		got, force := StripProDirective(tc.in)
		if got != tc.want || force != tc.force {
			t.Fatalf("%q → (%q,%v) want (%q,%v)", tc.in, got, force, tc.want, tc.force)
		}
	}
}

func TestShouldBypassSticky(t *testing.T) {
	r := &Runner{AutoModels: true, ProviderID: "deepseek", UserText: "hi"}
	if r.shouldBypassSticky(ModelFlash) {
		t.Fatal("simple turn must keep flash sticky")
	}
	r.UserText = "сделай рефакторинг auth"
	if !r.shouldBypassSticky(ModelFlash) {
		t.Fatal("complex turn must bypass flash sticky")
	}
	if r.shouldBypassSticky(ModelPro) {
		t.Fatal("pro sticky must not be bypassed for complexity alone")
	}
	r.UserText = "hi"
	r.ForcePro = true
	if !r.shouldBypassSticky(ModelFlash) {
		t.Fatal("/pro must bypass flash sticky")
	}
}

func TestBumpToStrongOneWay(t *testing.T) {
	r := &Runner{AutoModels: true, ProviderID: "deepseek", runModel: ModelFlash}
	var notices []string
	emit := func(e Event) {
		if e.Type == "notice" {
			notices = append(notices, e.Content)
		}
	}
	if !r.bumpToStrong(emit, "escalate-dup-tool", escalateNotice("escalate-dup-tool")) {
		t.Fatal("expected bump from flash")
	}
	if r.runModel != ModelPro {
		t.Fatalf("got %s want pro", r.runModel)
	}
	if r.bumpToStrong(emit, "escalate-dup-tool", escalateNotice("escalate-dup-tool")) {
		t.Fatal("second bump should return false")
	}
	if len(notices) != 1 {
		t.Fatalf("notices=%v", notices)
	}
}

func TestEscalateTrackerDupAndFail(t *testing.T) {
	var esc escalateTracker
	calls := []llm.ToolCall{{
		Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`},
	}}
	if esc.noteToolCalls(calls) {
		t.Fatal("first call is not a dup")
	}
	if !esc.noteToolCalls(calls) {
		t.Fatal("second identical call is a dup")
	}
	outsOK := []llm.Message{llm.ToolResultMessage("1", "ok")}
	if esc.noteToolResults(outsOK) {
		t.Fatal("success resets fail streak")
	}
	outsFail := []llm.Message{llm.ToolResultMessage("1", "ERROR: boom")}
	if esc.noteToolResults(outsFail) {
		t.Fatal("one fail is not enough")
	}
	if !esc.noteToolResults(outsFail) {
		t.Fatal("two fails must escalate")
	}
}

func TestResolveModel_ForceProAndStickyBypass(t *testing.T) {
	emit := func(Event) {}
	r := &Runner{
		AutoModels:  true,
		ProviderID:  "deepseek",
		StickyModel: ModelFlash,
		ForcePro:    true,
		UserText:    "hi",
	}
	if got := r.resolveModel(0, emit); got != ModelPro {
		t.Fatalf("force pro: got %s", got)
	}

	r2 := &Runner{
		AutoModels:  true,
		ProviderID:  "deepseek",
		StickyModel: ModelFlash,
		UserText:    "сделай рефакторинг всего auth",
	}
	if got := r2.resolveModel(0, emit); got != ModelPro {
		t.Fatalf("complex bypass: got %s want pro", got)
	}
}

func TestApplyProDirective(t *testing.T) {
	r := &Runner{UserText: "/pro поправь баг"}
	msg := llm.UserText("/pro поправь баг")
	r.applyProDirective(&msg)
	if !r.ForcePro || r.UserText != "поправь баг" || msg.Content != "поправь баг" {
		t.Fatalf("ForcePro=%v text=%q msg=%q", r.ForcePro, r.UserText, msg.Content)
	}
	if strings.Contains(msg.Content, "/pro") {
		t.Fatal("directive must be stripped from message")
	}
}
