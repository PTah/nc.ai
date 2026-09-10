package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"notcursor.ai/app/internal/workspace"
)

func TestLogAndCommitPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt", "b.txt")
	run("commit", "-m", "seed")

	m := workspace.NewManager()
	if _, err := m.Open(root); err != nil {
		t.Fatal(err)
	}
	s := New(m)
	logOut, err := s.Log(5, "")
	if err != nil || !strings.Contains(logOut, "seed") {
		t.Fatalf("log: %q %v", logOut, err)
	}

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("two\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitPaths("only a", []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st, "b.txt") {
		t.Fatalf("b.txt should still be dirty: %s", st)
	}
	if strings.Contains(st, "a.txt") {
		t.Fatalf("a.txt should be committed: %s", st)
	}
}
