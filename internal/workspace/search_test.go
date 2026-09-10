package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindFilesAndGrep(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "api"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "api", "router.go"), []byte("package api\nfunc Routes() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	if _, err := m.Open(root); err != nil {
		t.Fatal(err)
	}

	names, err := m.FindFiles("router", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 || !strings.Contains(names[0], "router.go") {
		t.Fatalf("find_files: %v", names)
	}

	hits, err := m.Grep(GrepOptions{Query: "Routes", PathGlob: "**/*.go", Context: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Line != 2 {
		t.Fatalf("grep hits=%v", hits)
	}
	reHits, err := m.Grep(GrepOptions{Query: `func\s+Routes`, PathGlob: "**/*.go", Limit: 10})
	if err != nil || len(reHits) != 1 {
		t.Fatalf("regex grep: %v %v", reHits, err)
	}
	files := FormatGrepHitsMode(hits, "files_with_matches")
	if !strings.Contains(files, "router.go") {
		t.Fatalf("files mode: %s", files)
	}
	out := FormatGrepHits(hits)
	if !strings.Contains(out, "router.go:2:func Routes") {
		t.Fatalf("format: %s", out)
	}
}

func TestReadFileRange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if _, err := m.Open(root); err != nil {
		t.Fatal(err)
	}
	got, err := m.ReadFileRange("a.txt", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "lines 2-3 of") || !strings.Contains(got, "     2|two") || !strings.Contains(got, "     3|three") {
		t.Fatalf("got %q", got)
	}
}

func TestGlobAndGrepPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "api"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "api", "router.go"), []byte("package api\nfunc Routes() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello Routes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if _, err := m.Open(root); err != nil {
		t.Fatal(err)
	}
	names, err := m.Glob("**/*.go", "", 10)
	if err != nil || len(names) != 1 || !strings.Contains(names[0], "router.go") {
		t.Fatalf("glob: %v %v", names, err)
	}
	hits, err := m.Grep(GrepOptions{Query: "Routes", Path: "internal", Limit: 10})
	if err != nil || len(hits) != 1 || !strings.Contains(hits[0].Path, "router.go") {
		t.Fatalf("grep path dir: %v %v", hits, err)
	}
	mdHits, err := m.Grep(GrepOptions{Query: "Routes", Path: "README.md", Limit: 10})
	if err != nil || len(mdHits) != 1 {
		t.Fatalf("grep path file: %v %v", mdHits, err)
	}
}

func TestMovePath(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.WriteFile("old.txt", "x"); err != nil {
		t.Fatal(err)
	}
	if err := m.MovePath("old.txt", "dir/new.txt"); err != nil {
		t.Fatal(err)
	}
	got, err := m.ReadFile("dir/new.txt")
	if err != nil || !strings.Contains(got, "x") {
		t.Fatalf("moved: %q %v", got, err)
	}
	if _, err := m.ReadFile("old.txt"); err == nil {
		t.Fatal("old path should be gone")
	}
}

func TestProjectTree(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "src"), 0o700)
	_ = os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o600)
	m := NewManager()
	if _, err := m.Open(root); err != nil {
		t.Fatal(err)
	}
	tree, err := m.ProjectTree(50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tree, "src/") || !strings.Contains(tree, "main.go") {
		t.Fatalf("tree=%s", tree)
	}
}
