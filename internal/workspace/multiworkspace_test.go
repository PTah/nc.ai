package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Рабочее пространство (как в Cursor): ключ — служебная папка без кода, папки с
// кодом — Roots в заданном порядке, «якоря» нет и убрать можно любую, кроме
// последней.
func TestWorkspaceUpsertAndFolders(t *testing.T) {
	m := NewManager()
	key := t.TempDir()
	api, web := filepath.Join(t.TempDir(), "api"), filepath.Join(t.TempDir(), "web")
	for _, d := range []string{api, web} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(api, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := m.UpsertWorkspace(key, "Моё", []string{api, web, api})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !p.Workspace || len(p.Roots) != 2 || p.Roots[0] != api {
		t.Fatalf("workspace = %+v, ожидали 2 папки без повторов, api первой", p)
	}
	// Upsert сам не делает пространство активным; Open — делает и не затирает.
	if _, err := m.ActiveRoot(); err == nil {
		t.Fatal("upsert не должен менять активный проект")
	}
	if _, err := m.Open(key); err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	if got, _ := m.ActiveRoot(); got != key {
		t.Fatalf("ActiveRoot = %q, want ключ %q", got, key)
	}
	if got, err := m.PrimaryRoot(); err != nil || got != api {
		t.Fatalf("PrimaryRoot = %q, %v; want %q", got, err, api)
	}
	list := m.List()
	if len(list) != 1 || !list[0].Workspace || list[0].Name != "Моё" || len(list[0].Roots) != 2 {
		t.Fatalf("после Open список = %+v", list)
	}

	folders := m.Folders()
	if len(folders) != 2 || folders[0].Name != "api" || folders[1].Name != "web" {
		t.Fatalf("folders = %+v", folders)
	}
	if !m.IsMultiRoot() {
		t.Fatal("две папки — это мульти-корень")
	}
	if f, err := m.FolderFor("WEB"); err != nil || f.Path != web {
		t.Fatalf("FolderFor(WEB) = %+v, %v", f, err)
	}
	if f, err := m.FolderFor(""); err != nil || f.Path != api {
		t.Fatalf("FolderFor('') = %+v, %v; want первую папку", f, err)
	}
	if _, err := m.FolderFor("nope"); err == nil || !strings.Contains(err.Error(), "api") {
		t.Fatalf("FolderFor(nope) должен перечислить папки, err=%v", err)
	}

	// Дерево и чтение — через имя папки.
	top, err := m.ListDir(".")
	if err != nil || len(top) != 2 {
		t.Fatalf("ListDir('.') = %+v, %v", top, err)
	}
	if got, err := m.ReadFile("api/main.go"); err != nil || !strings.Contains(got, "package main") {
		t.Fatalf("ReadFile(api/main.go) = %q, %v", got, err)
	}

	// Добавить / убрать папку: ключ не входит в Roots.
	third := t.TempDir()
	if p, err = m.AddRoot(key, third); err != nil || len(p.Roots) != 3 || p.Roots[2] != third {
		t.Fatalf("add root: %+v, %v", p, err)
	}
	if p, err = m.RemoveRoot(key, api); err != nil || len(p.Roots) != 2 || p.Roots[0] != web {
		t.Fatalf("remove first root: %+v, %v", p, err)
	}
	if got, _ := m.PrimaryRoot(); got != web {
		t.Fatalf("PrimaryRoot после удаления = %q, want %q", got, web)
	}
	if _, err := m.RemoveRoot(key, web); err != nil {
		t.Fatalf("remove web: %v", err)
	}
	if _, err := m.RemoveRoot(key, third); err == nil {
		t.Fatal("последнюю папку убирать нельзя")
	}
}

func TestWorkspaceUpsertValidation(t *testing.T) {
	m := NewManager()
	key := t.TempDir()
	if _, err := m.UpsertWorkspace(key, "x", nil); err == nil {
		t.Fatal("без папок пространство создавать нельзя")
	}
	if _, err := m.UpsertWorkspace(key, "", []string{t.TempDir()}); err == nil {
		t.Fatal("без имени пространство создавать нельзя")
	}
	missing := filepath.Join(t.TempDir(), "gone")
	if _, err := m.UpsertWorkspace(key, "x", []string{missing}); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("ошибка должна называть недоступную папку, err=%v", err)
	}
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.UpsertWorkspace(key, "x", []string{file}); err == nil {
		t.Fatal("файл не папка")
	}
}

// Прогон, начатый в пространстве, видит его папки даже после переключения на другой проект.
func TestWorkspaceScopedKeepsFolders(t *testing.T) {
	m := NewManager()
	key, other := t.TempDir(), t.TempDir()
	a, b := t.TempDir(), t.TempDir()
	if _, err := m.UpsertWorkspace(key, "ws", []string{a, b}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(key); err != nil {
		t.Fatal(err)
	}
	run := m.Scoped(key)
	if _, err := m.Open(other); err != nil {
		t.Fatal(err)
	}
	if got := run.Roots(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("scoped roots = %v", got)
	}
	if got, _ := run.PrimaryRoot(); got != a {
		t.Fatalf("scoped primary = %q", got)
	}
}
