// Created by nc.ai 0.9.4 on new-admin-pc 2026-10-30 08:40:13
package tools

import (
	"fmt"
	"strings"
)

// Бюджет на один tool-результат. Жирный stdout или файл без диапазона иначе
// тянется в каждый следующий запрос прогона и оплачивается на каждом шаге.
// Усечение делается в момент Execute, а не переписыванием уже отправленной
// истории — так prefix-cache не ломается.
const (
	toolResultMaxBytes = 32 << 10 // 32 KB на результат одного инструмента
	toolResultMaxLines = 800
	readFileMaxLines   = 500 // read_file без start_line/end_line отдаёт голову файла
)

// clipHead оставляет начало результата и добавляет пометку, как сузить запрос.
// Режет по строкам, а не посреди строки, чтобы нумерация read_file оставалась
// читаемой.
func clipHead(s string, maxBytes, maxLines int, hint string) string {
	if withinBudget(s, maxBytes, maxLines) {
		return s
	}
	lines := strings.Split(s, "\n")
	kept := 0
	bytes := 0
	for kept < len(lines) {
		if maxLines > 0 && kept >= maxLines {
			break
		}
		next := len(lines[kept]) + 1
		if maxBytes > 0 && bytes+next > maxBytes {
			break
		}
		bytes += next
		kept++
	}
	if kept >= len(lines) {
		return s
	}
	note := fmt.Sprintf("\n(truncated; %d of %d lines omitted", len(lines)-kept, len(lines))
	if hint != "" {
		note += " — " + hint
	}
	note += ")"
	if kept == 0 && len(lines) > 0 {
		// Одна очень длинная строка (минифицированный JSON, длинная строка лога):
		// отдаём хотя бы её начало, иначе результат пуст.
		head := lines[0]
		if maxBytes > 0 && len(head) > maxBytes {
			head = head[:maxBytes]
		}
		return head + note
	}
	return strings.Join(lines[:kept], "\n") + note
}

// clipSplit оставляет голову и хвост: у команд шапка обычно в начале, а ошибка —
// в конце. Середину выбрасываем с пометкой.
func clipSplit(s string, maxBytes, maxLines int, hint string) string {
	if withinBudget(s, maxBytes, maxLines) {
		return s
	}
	lines := strings.Split(s, "\n")
	half := maxLines / 2
	if half < 1 {
		half = 1
	}
	if len(lines) <= 2*half {
		// Строк мало, но байтов много: режем по байтам.
		return clipHead(s, maxBytes, 0, hint)
	}
	head := strings.Join(lines[:half], "\n")
	tail := strings.Join(lines[len(lines)-half:], "\n")
	note := fmt.Sprintf("\n…(truncated; %d lines omitted", len(lines)-2*half)
	if hint != "" {
		note += " — " + hint
	}
	note += ")…\n"
	return head + note + tail
}

func withinBudget(s string, maxBytes, maxLines int) bool {
	if maxBytes > 0 && len(s) > maxBytes {
		return false
	}
	if maxLines > 0 && strings.Count(s, "\n")+1 > maxLines {
		return false
	}
	return true
}
