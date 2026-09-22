package agent

import (
	"fmt"
	"strings"

	"notcursor.ai/app/internal/llm"
)

// Список ToDo в UI висел в исходном состоянии: модель создавала его и больше не
// трогала. Напоминаем об обновлении, если список несколько шагов не менялся.
const (
	todoIdleSteps  = 4
	todoIdleNudges = 2
)

// todoStaleReminder возвращает напоминание об обновлении списка задач
// ("" — напоминать не нужно) и ведёт счётчик шагов без обновления.
func (r *Runner) todoStaleReminder(calls []llm.ToolCall) string {
	for _, c := range calls {
		if strings.EqualFold(strings.TrimSpace(c.Function.Name), "todo_write") {
			r.todoIdle = 0
			return ""
		}
	}
	if r.Tools == nil || r.Tools.Todos == nil {
		return ""
	}
	items := r.Tools.Todos.Snapshot()
	pending := 0
	for _, it := range items {
		switch strings.ToLower(strings.TrimSpace(it.Status)) {
		case "completed", "cancelled":
		default:
			pending++
		}
	}
	if len(items) == 0 || pending == 0 {
		r.todoIdle = 0
		return ""
	}
	r.todoIdle++
	if r.todoIdle < todoIdleSteps || r.todoNudges >= todoIdleNudges {
		return ""
	}
	r.todoIdle = 0
	r.todoNudges++
	return fmt.Sprintf(
		"Список задач в UI не менялся %d шагов. Вызови todo_write (merge=true): закрытые пункты — completed, текущий — in_progress. Пользователь смотрит этот список.",
		todoIdleSteps)
}
