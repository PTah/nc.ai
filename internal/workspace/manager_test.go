package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	m := NewManager()
	if _, err := m.Open(root); err != nil {
		t.Fatalf("open: %v", err)
	}
	return m, root
}

// TestScopedKeepsRunRoot is the "two projects at once" guard: a run that
// started in project A must keep writing to A after the user opens B.
func TestScopedKeepsRunRoot(t *testing.T) {
	m, rootA := newTestManager(t)
	rootB := t.TempDir()

	run := m.Scoped(rootA)
	// The user switches to another project while the agent is still working.
	if err := m.SetActive(rootB); err != nil {
		t.Fatalf("set active: %v", err)
	}
	if got, err := run.ActiveRoot(); err != nil || got != rootA {
		t.Fatalf("scoped root = %q, %v; want %q", got, err, rootA)
	}
	if err := run.WriteFile("pinned.txt", "run A"); err != nil {
		t.Fatalf("scoped write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootA, "pinned.txt")); err != nil {
		t.Fatalf("file not written into project A: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootB, "pinned.txt")); err == nil {
		t.Fatal("scoped write leaked into project B")
	}
	// Without a pin, operations follow the active project again.
	if got, err := m.ActiveRoot(); err != nil || got != rootB {
		t.Fatalf("active root = %q, %v; want %q", got, err, rootB)
	}
	// The scoped view owns its own lock and project list: switching must not
	// change it, and it must survive concurrent use.
	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			done <- run.WriteFile("pinned.txt", "again")
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent scoped write: %v", err)
		}
	}
}

func TestScopedEmptyRootFallsBackToActive(t *testing.T) {
	m, root := newTestManager(t)
	view := m.Scoped("  ")
	if got, err := view.ActiveRoot(); err != nil || got != root {
		t.Fatalf("Scoped('') root = %q, %v; want %q", got, err, root)
	}
}

func TestResolveWithinRoot(t *testing.T) {
	m, root := newTestManager(t)

	if got, err := m.Resolve("."); err != nil || got != root {
		t.Fatalf("Resolve('.') = %q, %v; want %q", got, err, root)
	}
	want := filepath.Join(root, "src")
	if got, err := m.Resolve("src"); err != nil || got != want {
		t.Fatalf("Resolve('src') = %q, %v; want %q", got, err, want)
	}
	want = filepath.Join(root, "b")
	if got, err := m.Resolve("a/../b"); err != nil || got != want {
		t.Fatalf("Resolve('a/../b') = %q, %v; want %q", got, err, want)
	}
}

func TestResolveBlocksEscape(t *testing.T) {
	m, _ := newTestManager(t)

	cases := []string{
		"../escape",
		"../../escape",
		"..\\escape",
		"..\\..\\escape",
		"a/../../escape",
		"a\\..\\..\\escape",
	}
	for _, rel := range cases {
		if got, err := m.Resolve(rel); err == nil {
			t.Fatalf("Resolve(%q) = %q, want escape error", rel, got)
		}
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	m, root := newTestManager(t)

	if err := m.WriteFile("dir/file.txt", "hello world"); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := m.ReadFile("dir/file.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got != "     1|hello world" {
		t.Fatalf("ReadFile = %q, want numbered line", got)
	}
	// The file must actually land inside the workspace root.
	if _, err := os.Stat(filepath.Join(root, "dir", "file.txt")); err != nil {
		t.Fatalf("stat written file: %v", err)
	}
}

func TestReadFileNotFoundHint(t *testing.T) {
	m, _ := newTestManager(t)

	if err := m.WriteFile("config.json", "{}"); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := m.ReadFile("config.yml")
	if err == nil {
		t.Fatal("ReadFile(missing) = nil error")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("error = %q, want 'file not found' hint", err.Error())
	}
	if !strings.Contains(err.Error(), "config.json") {
		t.Fatalf("error = %q, want sibling suggestion", err.Error())
	}
}

func TestIgnoredName(t *testing.T) {
	for _, name := range []string{".git", "node_modules", "frontend/dist", "dist", ".wails", "build/bin"} {
		if !ignoredName(name) {
			t.Fatalf("ignoredName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"src", "main.go", "docs"} {
		if ignoredName(name) {
			t.Fatalf("ignoredName(%q) = true, want false", name)
		}
	}
}

func TestSearchFilesRespectsLimit(t *testing.T) {
	m, _ := newTestManager(t)

	for _, f := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if err := m.WriteFile(f, "needle "+f); err != nil {
			t.Fatalf("WriteFile(%s): %v", f, err)
		}
	}
	hits, err := m.SearchFiles("needle", 2)
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("SearchFiles returned %d hits, want 2: %v", len(hits), hits)
	}
}
