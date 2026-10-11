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

func TestMergeStampBlockFirstLineAtTop(t *testing.T) {
	line := "// Created by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00"
	got := mergeStampBlock("package main\n", line, "HOST1")
	if !strings.HasPrefix(got, line+"\npackage main") {
		t.Fatalf("line not inserted at the top: %q", got)
	}
}

func TestMergeStampBlockSameHostAddsNothing(t *testing.T) {
	src := "// Created by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00\npackage main\n"
	line := "// Edited by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:05:00"
	if got := mergeStampBlock(src, line, "HOST1"); got != src {
		t.Fatalf("same host must not add a line: %q", got)
	}
}

func TestMergeStampBlockOtherHostAppends(t *testing.T) {
	src := "// Created by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00\npackage main\n"
	line := "// Edited by nc.ai 0.8.3.3 on HOST2 2026-10-11 16:00:00"
	got := mergeStampBlock(src, line, "HOST2")
	if strings.Count(got, "by nc.ai") != 2 {
		t.Fatalf("another host must add its own line: %q", got)
	}
	if strings.Index(got, "HOST1") > strings.Index(got, "HOST2") {
		t.Fatalf("oldest line must come first: %q", got)
	}
}

func TestMergeStampBlockSameHostAgainAfterOther(t *testing.T) {
	src := "// Created by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00\n" +
		"// Edited by nc.ai 0.8.3.3 on HOST2 2026-10-11 16:00:00\npackage main\n"
	line := "// Edited by nc.ai 0.8.3.3 on HOST1 2026-10-11 17:00:00"
	got := mergeStampBlock(src, line, "HOST1")
	if strings.Count(got, "by nc.ai") != 3 {
		t.Fatalf("HOST1 must add a line again after HOST2: %q", got)
	}
}

func TestMergeStampBlockKeepsShebangFirst(t *testing.T) {
	line := "# Created by nc.ai 0.8.3.3 on HOST1 2026-10-11 15:00:00"
	got := mergeStampBlock("#!/usr/bin/env python3\nprint(1)\n", line, "HOST1")
	if !strings.HasPrefix(got, "#!/usr/bin/env python3\n"+line) {
		t.Fatalf("shebang must stay first: %q", got)
	}
}
