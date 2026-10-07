package chatstore

import (
	"path/filepath"
	"testing"
)

// Своя модель чата переживает перезапуск: она пишется в JSON сессии.
func TestSetSessionModelPersists(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	project := filepath.Join(dir, "proj")
	sess, err := s.NewSession(project, "first")
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := s.SetSessionModel(project, sess.ID, "deepseek-v4-pro"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	got, err := s.Get(project, sess.ID)
	if err != nil || got.Model != "deepseek-v4-pro" {
		t.Fatalf("model=%q err=%v", got.Model, err)
	}

	// Открываем заново (как после перезапуска) — модель на месте.
	reopened, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err = reopened.Get(project, sess.ID)
	if err != nil || got.Model != "deepseek-v4-pro" {
		t.Fatalf("после переоткрытия model=%q err=%v", got.Model, err)
	}

	// Пустая строка — «как в настройках» (сбрасываем ручной выбор).
	if err := reopened.SetSessionModel(project, sess.ID, ""); err != nil {
		t.Fatalf("reset model: %v", err)
	}
	got, _ = reopened.Get(project, sess.ID)
	if got.Model != "" {
		t.Fatalf("ожидали пустую модель, получили %q", got.Model)
	}
}

func TestSetSessionModelUnknownSession(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.SetSessionModel("proj", "нет-такой", "m"); err == nil {
		t.Fatal("ожидали ошибку для неизвестной сессии")
	}
}
