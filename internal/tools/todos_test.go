package tools

import "testing"

func newStore(t *testing.T, items []Todo) *TodoStore {
	t.Helper()
	s := NewTodoStore()
	if _, err := s.Apply(false, items); err != nil {
		t.Fatalf("apply initial: %v", err)
	}
	return s
}

// Ключевой кейс: агент обновляет только статус, не повторяя content.
func TestApplyMergeStatusOnlyKeepsContent(t *testing.T) {
	s := newStore(t, []Todo{
		{ID: "1", Content: "первый шаг", Status: "pending"},
		{ID: "2", Content: "второй шаг", Status: "pending"},
	})
	got, err := s.Apply(true, []Todo{{ID: "1", Status: "completed"}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got[0].Status != "completed" {
		t.Fatalf("status = %q, want completed", got[0].Status)
	}
	if got[0].Content != "первый шаг" {
		t.Fatalf("content потерян: %q", got[0].Content)
	}
	if got[1].Status != "pending" {
		t.Fatalf("второй пункт не должен меняться: %q", got[1].Status)
	}
}

// Обратный кейс: обновили только текст — статус не должен сбрасываться в pending.
func TestApplyMergeContentOnlyKeepsStatus(t *testing.T) {
	s := newStore(t, []Todo{{ID: "1", Content: "шаг", Status: "completed"}})
	got, err := s.Apply(true, []Todo{{ID: "1", Content: "шаг (уточнено)"}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got[0].Status != "completed" {
		t.Fatalf("статус сброшен: %q", got[0].Status)
	}
	if got[0].Content != "шаг (уточнено)" {
		t.Fatalf("content = %q", got[0].Content)
	}
}

func TestApplyMergeAppendsUnknownID(t *testing.T) {
	s := newStore(t, []Todo{{ID: "1", Content: "a", Status: "completed"}})
	got, err := s.Apply(true, []Todo{{ID: "2", Content: "b", Status: "in_progress"}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(got) != 2 || got[1].ID != "2" || got[1].Status != "in_progress" {
		t.Fatalf("append: %+v", got)
	}
}

func TestApplyReplaceDropsOthers(t *testing.T) {
	s := newStore(t, []Todo{{ID: "1", Content: "a", Status: "completed"}})
	got, err := s.Apply(false, []Todo{{ID: "9", Content: "новый план", Status: "pending"}})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(got) != 1 || got[0].ID != "9" {
		t.Fatalf("replace: %+v", got)
	}
}

func TestApplyStatusAliasesAndEmptyID(t *testing.T) {
	s := NewTodoStore()
	got, err := s.Apply(false, []Todo{{Content: "без id", Status: "done"}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got[0].ID == "" {
		t.Fatal("id должен быть сгенерирован")
	}
	if got[0].Status != "completed" {
		t.Fatalf("аlias done → completed, получено %q", got[0].Status)
	}
}
