package config

import "testing"

func TestWorkspaceDefsPersist(t *testing.T) {
	dir := t.TempDir()
	s := NewStoreForTest(dir)
	if _, ok := s.GetWorkspace("k1"); ok {
		t.Fatal("пустые настройки не должны знать о пространствах")
	}
	if err := s.SaveWorkspace(WorkspaceDef{Key: "k1", Name: "Один", Roots: []string{"/a", "/b"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.SaveWorkspace(WorkspaceDef{Key: "k1", Name: "Один+", Roots: []string{"/b", "/c"}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := s.SaveWorkspace(WorkspaceDef{Key: "k2", Name: "Два", Roots: []string{"/d"}}); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	got, ok := s.GetWorkspace("k1")
	if !ok || got.Name != "Один+" || len(got.Roots) != 2 || got.Roots[0] != "/b" {
		t.Fatalf("после обновления: %+v, %v", got, ok)
	}
	// Возвращается копия: правка снаружи не портит настройки.
	got.Roots[0] = "/hacked"
	if again, _ := s.GetWorkspace("k1"); again.Roots[0] != "/b" {
		t.Fatalf("GetWorkspace отдал внутренний срез: %+v", again)
	}
	if err := s.RemoveWorkspace("k1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := s.GetWorkspace("k1"); ok {
		t.Fatal("k1 должен быть удалён")
	}
	if _, ok := s.GetWorkspace("k2"); !ok {
		t.Fatal("k2 не должен пострадать")
	}
	if err := s.SaveWorkspace(WorkspaceDef{Name: "без ключа"}); err == nil {
		t.Fatal("пустой ключ должен отклоняться")
	}
}
