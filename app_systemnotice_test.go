// Created by nc.ai 0.8.6.0 on macbook 2026-10-13 22:34:43
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// appendSystemNotice: добавляет системную заметку в конец, идемпотентно, не
// трогает существующие элементы и не падает на пустом/битом транскрипте.
func TestAppendSystemNotice(t *testing.T) {
	base := `[{"kind":"user","content":"привет"},{"kind":"assistant","content":"ответ"}]`

	got, changed := appendSystemNotice(base, "⚠️ Пополнение DeepSeek меньше $1")
	if !changed {
		t.Fatal("заметка не добавилась")
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(got), &items); err != nil {
		t.Fatalf("результат не JSON: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("элементов %d, хотели 3", len(items))
	}
	last := items[2]
	if last["kind"] != "system" {
		t.Fatalf("kind = %v, хотели system", last["kind"])
	}
	if txt, _ := last["content"].(string); !strings.Contains(txt, "меньше $1") {
		t.Fatalf("текст заметки = %q", txt)
	}
	if prev, _ := items[1]["content"].(string); prev != "ответ" {
		t.Fatalf("существующий элемент изменился: %q", prev)
	}

	// Идемпотентность: если последний элемент уже содержит текст — не дублируем.
	again, changed := appendSystemNotice(got, "Пополнение DeepSeek меньше $1")
	if changed {
		t.Fatal("заметка добавилась повторно")
	}
	if again != got {
		t.Fatal("повторный вызов изменил транскрипт")
	}

	for _, empty := range []string{"", "   ", "не json", "[]"} {
		if _, changed := appendSystemNotice(empty, "текст"); changed {
			t.Fatalf("для %q ожидали отсутствие изменений", empty)
		}
	}
}
