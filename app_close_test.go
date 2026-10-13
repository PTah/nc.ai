// Created by nc.ai 0.8.5.6 on macbook 2026-10-13 21:42:56
package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Заметка о прерванном ответе: дописывается в конец, идемпотентна, не ломает
// существующие элементы и ничего не делает с пустым/битым транскриптом.
func TestAppendInterruptedNotice(t *testing.T) {
	base := `[{"kind":"user","content":"привет"},{"kind":"assistant","content":"начал отвечать и обор"}]`

	got, changed := appendInterruptedNotice(base)
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
	last := items[len(items)-1]
	if last["kind"] != "system" {
		t.Fatalf("kind = %v, хотели system", last["kind"])
	}
	if txt, _ := last["content"].(string); !strings.Contains(txt, "прерван закрытием") {
		t.Fatalf("текст заметки = %q", txt)
	}
	if prev, _ := items[1]["content"].(string); prev != "начал отвечать и обор" {
		t.Fatalf("существующий элемент изменился: %q", prev)
	}

	// Второй раз — ничего не меняем (идемпотентность).
	again, changed := appendInterruptedNotice(got)
	if changed {
		t.Fatal("заметка добавилась повторно")
	}
	if again != got {
		t.Fatal("повторный вызов изменил транскрипт")
	}

	for _, empty := range []string{"", "   ", "не json", "[]"} {
		if _, changed := appendInterruptedNotice(empty); changed {
			t.Fatalf("для %q ожидали отсутствие изменений", empty)
		}
	}
}

func TestMarkerSessions(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/running.json"
	if got := markerSessions(path); got != nil {
		t.Fatalf("нет файла — ожидали nil, получили %v", got)
	}
	if err := os.WriteFile(path, []byte(`{"version":"0.8.5.7","sessions":["a","b"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := markerSessions(path)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("sessions = %v", got)
	}
	if err := os.WriteFile(path, []byte("мусор"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := markerSessions(path); got != nil {
		t.Fatalf("битый маркер — ожидали nil, получили %v", got)
	}
}
