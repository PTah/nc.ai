package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Todo is one item in the agent's task list.
type Todo struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed | cancelled
}

// TodoStore is the in-memory list shown in the UI.
type TodoStore struct {
	mu    sync.Mutex
	items []Todo
}

func NewTodoStore() *TodoStore {
	return &TodoStore{}
}

func (s *TodoStore) Snapshot() []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Todo, len(s.items))
	copy(out, s.items)
	return out
}

// Apply применяет пришедший список. При merge=true недостающие поля берутся из
// уже сохранённого пункта: агент часто присылает только id+status (или только
// id+content), и раньше это либо ломало обновление («content is empty»), либо
// сбрасывало статус обратно в pending — из-за чего галочки в UI не появлялись.
func (s *TodoStore) Apply(merge bool, incoming []Todo) ([]Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := map[string]Todo{}
	for _, t := range s.items {
		prev[t.ID] = t
	}
	for i := range incoming {
		incoming[i].ID = strings.TrimSpace(incoming[i].ID)
		incoming[i].Content = strings.TrimSpace(incoming[i].Content)
		incoming[i].Status = strings.TrimSpace(incoming[i].Status)
		if incoming[i].ID == "" {
			incoming[i].ID = fmt.Sprintf("t%d", i+1)
		}
		if merge {
			if old, ok := prev[incoming[i].ID]; ok {
				if incoming[i].Content == "" {
					incoming[i].Content = old.Content
				}
				if incoming[i].Status == "" {
					incoming[i].Status = old.Status
				}
			}
		}
		incoming[i].Status = normalizeTodoStatus(incoming[i].Status)
		if incoming[i].Content == "" && incoming[i].Status != "cancelled" {
			return nil, fmt.Errorf("todo %s: content is empty", incoming[i].ID)
		}
	}
	if !merge {
		s.items = incoming
		return s.copyLocked(), nil
	}
	byID := map[string]int{}
	for i, t := range s.items {
		byID[t.ID] = i
	}
	for _, t := range incoming {
		if i, ok := byID[t.ID]; ok {
			s.items[i] = t
			continue
		}
		s.items = append(s.items, t)
		byID[t.ID] = len(s.items) - 1
	}
	return s.copyLocked(), nil
}

func (s *TodoStore) copyLocked() []Todo {
	out := make([]Todo, len(s.items))
	copy(out, s.items)
	return out
}

func normalizeTodoStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "in_progress", "in-progress", "doing":
		return "in_progress"
	case "completed", "done":
		return "completed"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return "pending"
	}
}

func formatTodos(items []Todo) string {
	b, _ := json.MarshalIndent(items, "", "  ")
	return string(b)
}
