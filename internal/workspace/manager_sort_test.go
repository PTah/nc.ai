package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// Панель проектов идёт по алфавиту, а не в порядке добавления.
func TestListSortedByName(t *testing.T) {
	root := t.TempDir()
	names := []string{"Pentest", "nc.ai", "Android", "ботва", "NotCursor"}
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", n, err)
		}
	}
	m := NewManager()
	// Открываем вразнобой — специально не по алфавиту.
	for _, n := range []string{"Pentest", "NotCursor", "nc.ai", "ботва", "Android"} {
		if _, err := m.Open(filepath.Join(root, n)); err != nil {
			t.Fatalf("open %s: %v", n, err)
		}
	}

	got := make([]string, 0, len(names))
	for _, p := range m.List() {
		got = append(got, p.Name)
	}
	want := []string{"Android", "nc.ai", "NotCursor", "Pentest", "ботва"} // регистр не важен: nc.ai < NotCursor
	if len(got) != len(want) {
		t.Fatalf("проектов %d, ожидали %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ожидали %v", got, want)
		}
	}
}

// Сортировка — только для выдачи: активный проект остаётся активным, а в
// настройках проекты лежат как раньше (новые добавляются сверху).
func TestListSortKeepsActiveAndStoredOrder(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"b-project", "c-project", "a-project"} {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	m := NewManager()
	for _, n := range []string{"b-project", "c-project", "a-project"} {
		if _, err := m.Open(filepath.Join(root, n)); err != nil {
			t.Fatalf("open %s: %v", n, err)
		}
	}

	wantActive := filepath.Join(root, "a-project")
	if active, err := m.ActiveRoot(); err != nil || active != wantActive {
		t.Fatalf("активный проект=%q (ожидали %q) err=%v", active, wantActive, err)
	}

	// Внутренний порядок: последний открытый сверху (a, c, b) — его не меняем.
	if m.projects[0].Name != "a-project" || m.projects[2].Name != "b-project" {
		t.Fatalf("внутренний порядок изменился: %+v", m.projects)
	}

	// Выдача — по алфавиту.
	list := m.List()
	if len(list) != 3 || list[0].Name != "a-project" || list[1].Name != "b-project" || list[2].Name != "c-project" {
		got := make([]string, 0, len(list))
		for _, p := range list {
			got = append(got, p.Name)
		}
		t.Fatalf("выдача не отсортирована: %v", got)
	}
}
