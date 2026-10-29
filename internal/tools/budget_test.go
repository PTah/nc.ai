// Created by nc.ai 0.9.4 on new-admin-pc 2026-10-30 08:40:38
package tools

import (
	"strings"
	"testing"
)

func TestClipHeadWithinBudget(t *testing.T) {
	s := "a\nb\nc"
	if got := clipHead(s, 1024, 10, "hint"); got != s {
		t.Fatalf("within budget must be untouched, got %q", got)
	}
}

func TestClipHeadByLines(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		b.WriteString("line\n")
	}
	got := clipHead(b.String(), 1<<20, 100, "pass start_line/end_line")
	lines := strings.Split(got, "\n")
	if len(lines) > 101 { // 100 kept + marker line
		t.Fatalf("kept too many lines: %d", len(lines))
	}
	if !strings.Contains(got, "truncated") || !strings.Contains(got, "start_line/end_line") {
		t.Fatalf("missing truncated marker or hint")
	}
	if !strings.HasPrefix(got, "line\nline\n") {
		t.Fatalf("head not preserved")
	}
}

func TestClipHeadByBytes(t *testing.T) {
	long := strings.Repeat("x", 5000)
	got := clipHead(long, 1000, 0, "narrow")
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected marker, got %q", got)
	}
	if !strings.HasPrefix(got, "xxxx") {
		t.Fatalf("head not preserved: %q", got[:40])
	}
	if len(got) > 1100 {
		t.Fatalf("byte budget exceeded: %d bytes", len(got))
	}
}

func TestClipSplitKeepsHeadAndTail(t *testing.T) {
	var b strings.Builder
	b.WriteString("HEAD\n")
	for i := 0; i < 2000; i++ {
		b.WriteString("mid\n")
	}
	b.WriteString("TAIL")
	got := clipSplit(b.String(), 1<<20, 100, "narrow the command")
	if !strings.Contains(got, "HEAD") || !strings.Contains(got, "TAIL") {
		t.Fatalf("head/tail not preserved")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("missing truncated marker")
	}
}

func TestClipShortStatusUntouched(t *testing.T) {
	s := "exit=0\nstdout:\nok\nstderr:\n"
	if got := clipSplit(s, toolResultMaxBytes, toolResultMaxLines, "x"); got != s {
		t.Fatalf("short result must be untouched, got %q", got)
	}
	if got := clipHead(s, toolResultMaxBytes, readFileMaxLines, "x"); got != s {
		t.Fatalf("short read must be untouched, got %q", got)
	}
}
