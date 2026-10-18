// Edited by nc.ai 0.8.8.0 on macbook 2026-10-18 19:42:54
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

// todoFinishNudges — сколько раз за прогон просим довести незакрытый план,
// прежде чем принять ответ как есть. Без этого агент останавливался посреди
// работы: модель отдавала «готово», а в списке задач оставались pending.
const todoFinishNudges = 3

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

// todoFinishReminder возвращает напоминание довести незакрытый план ("" — не
// нужно). Раньше цикл принимал любой ответ без tool_calls как финальный, даже
// когда в списке задач оставались pending/in_progress: пользователь просил
// довести работу, а прогон обрывался на середине. Бюджет попыток защищает от
// зацикливания, если пункт невыполним; снять ненужный пункт можно через
// todo_write (status=cancelled).
func (r *Runner) todoFinishReminder() string {
	if r.Tools == nil || r.Tools.Todos == nil || r.todoFinish >= todoFinishNudges {
		return ""
	}
	var pending []string
	for _, it := range r.Tools.Todos.Snapshot() {
		switch strings.ToLower(strings.TrimSpace(it.Status)) {
		case "completed", "cancelled":
		default:
			title := strings.TrimSpace(it.Content)
			if title == "" {
				title = it.ID
			}
			status := strings.TrimSpace(it.Status)
			if status == "" {
				status = "pending"
			}
			pending = append(pending, fmt.Sprintf("%s (%s)", title, status))
		}
	}
	if len(pending) == 0 {
		return ""
	}
	r.todoFinish++
	return fmt.Sprintf(
		"Работу обрывать нельзя: в списке задач осталось незавершённое — %s. "+
			"Продолжай и доведи эти пункты до конца, отмечая выполненные через todo_write (merge=true, status=completed); "+
			"итог дай только когда незакрытых пунктов не останется. Если пункт больше не нужен или пользователь просит другое — "+
			"сними его через todo_write (status=cancelled) и только потом ответь.",
		strings.Join(pending, "; "))
}
