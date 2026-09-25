package tools

import "testing"

func todoIDs(items []Todo) string {
	out := ""
	for _, t := range items {
		out += t.ID
	}
	return out
}

// Пользователь убрал пункт вручную — merge-обновление агента его не воскрешает.
func TestTodoRemoveSticksAcrossMerge(t *testing.T) {
	s := NewTodoStore()
	if _, err := s.Apply(false, []Todo{
		{ID: "1", Content: "пункт 1", Status: "completed"},
		{ID: "2", Content: "пункт 2", Status: "pending"},
		{ID: "3", Content: "пункт 3", Status: "pending"},
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got := s.Remove([]string{"2"})
	if todoIDs(got) != "13" {
		t.Fatalf("после удаления: %q", todoIDs(got))
	}

	// Агент присылает свой план целиком с merge=true — пункт 2 не возвращается.
	after, err := s.Apply(true, []Todo{
		{ID: "2", Content: "пункт 2", Status: "in_progress"},
		{ID: "3", Content: "пункт 3", Status: "completed"},
	})
	if err != nil {
		t.Fatalf("apply merge: %v", err)
	}
	if todoIDs(after) != "13" {
		t.Fatalf("удалённый пункт вернулся: %q", todoIDs(after))
	}
	if after[1].Status != "completed" {
		t.Fatalf("статус соседнего пункта потерялся: %+v", after)
	}

	// Новый план (merge=false) — чистый лист, прежние удаления не действуют.
	fresh, err := s.Apply(false, []Todo{{ID: "2", Content: "пункт 2 заново", Status: "pending"}})
	if err != nil {
		t.Fatalf("apply new plan: %v", err)
	}
	if todoIDs(fresh) != "2" {
		t.Fatalf("новый план: %q", todoIDs(fresh))
	}
}

func TestTodoClear(t *testing.T) {
	s := NewTodoStore()
	if _, err := s.Apply(false, []Todo{
		{ID: "1", Content: "готово", Status: "completed"},
		{ID: "2", Content: "отменено", Status: "cancelled"},
		{ID: "3", Content: "в работе", Status: "in_progress"},
		{ID: "4", Content: "ждёт", Status: "pending"},
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Убрать только закрытые.
	left := s.Clear(true)
	if todoIDs(left) != "34" {
		t.Fatalf("Clear(true): %q", todoIDs(left))
	}

	// Убрать всё.
	if all := s.Clear(false); len(all) != 0 {
		t.Fatalf("Clear(false): %+v", all)
	}
	if snap := s.Snapshot(); len(snap) != 0 {
		t.Fatalf("список не пуст: %+v", snap)
	}

	// Даже пустой merge не возвращает убранное.
	after, err := s.Apply(true, []Todo{{ID: "1", Content: "готово", Status: "completed"}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("пункт вернулся после Clear: %+v", after)
	}
}

func TestTodoRemoveUnknownID(t *testing.T) {
	s := NewTodoStore()
	if _, err := s.Apply(false, []Todo{{ID: "1", Content: "a", Status: "pending"}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := s.Remove([]string{"", "нет-такого"}); todoIDs(got) != "1" {
		t.Fatalf("список изменился на пустом удалении: %q", todoIDs(got))
	}
}
