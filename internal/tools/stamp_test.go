package tools

import (
	"strings"
	"testing"
)

func TestStampStyleFor(t *testing.T) {
	if s, ok := stampStyleFor("a.go"); !ok || s.line != "//" {
		t.Fatalf("go → //, got %+v ok=%v", s, ok)
	}
	if s, ok := stampStyleFor("a.py"); !ok || s.line != "#" {
		t.Fatalf("py → #, got %+v ok=%v", s, ok)
	}
	if _, ok := stampStyleFor("a.json"); ok {
		t.Fatal("json must be skipped (a comment would break it)")
	}
	if _, ok := stampStyleFor("a.exe"); ok {
		t.Fatal("unknown/binary must be skipped")
	}
	if s, ok := stampStyleFor("a.html"); !ok || s.open != "<!--" || s.close != "-->" {
		t.Fatalf("html → block comment, got %+v ok=%v", s, ok)
	}
}

func TestMergeStampBlockInsertsAndAppends(t *testing.T) {
	line1 := "// Created by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00"
	body1 := "Created by nc.ai 0.8.3.3 on HOST1"

	got := mergeStampBlock("package main\n", line1, body1)
	if !strings.HasPrefix(got, line1+"\npackage main") {
		t.Fatalf("line not inserted at the top: %q", got)
	}

	line2 := "// Edited by nc.ai 0.8.3.3 on HOST2 2026-10-11 16:00:00"
	body2 := "Edited by nc.ai 0.8.3.3 on HOST2"
	got = mergeStampBlock(got, line2, body2)
	if !strings.Contains(got, line1) || !strings.Contains(got, line2) {
		t.Fatalf("both lines must be present: %q", got)
	}
	if strings.Index(got, line1) > strings.Index(got, line2) {
		t.Fatalf("oldest line must come first: %q", got)
	}
}

func TestMergeStampBlockRefreshesSameAgent(t *testing.T) {
	body := "Edited by nc.ai 0.8.3.3 on HOST1"
	src := "// Edited by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00\npackage main\n"
	updated := "// Edited by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:05:00"

	got := mergeStampBlock(src, updated, body)
	if strings.Contains(got, "15:00:00") {
		t.Fatalf("old timestamp must be replaced: %q", got)
	}
	if strings.Count(got, "by nc.ai") != 1 {
		t.Fatalf("must not duplicate the line: %q", got)
	}
}

func TestMergeStampBlockKeepsShebangFirst(t *testing.T) {
	line := "# Created by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00"
	got := mergeStampBlock("#!/usr/bin/env python3\nprint(1)\n", line, "Created by nc.ai 0.8.3.3 on HOST1")
	if !strings.HasPrefix(got, "#!/usr/bin/env python3\n"+line) {
		t.Fatalf("shebang must stay first: %q", got)
	}
}

func TestMergeStampBlockDifferentHostAppends(t *testing.T) {
	src := "// Edited by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00\nx = 1\n"
	body2 := "Edited by nc.ai 0.8.3.3 on HOST2"
	line2 := "// Edited by nc.ai 0.8.3.3 on HOST2 2026-10-11 16:00:00"

	got := mergeStampBlock(src, line2, body2)
	if strings.Count(got, "by nc.ai") != 2 {
		t.Fatalf("another host must add its own line: %q", got)
	}
}
