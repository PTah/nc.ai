// Created by nc.ai 1.2.17 on Papasha 2026-11-21 08:45:00
package main

import (
	"strings"
	"testing"
)

func TestShortChatTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "короткий вопрос остаётся как есть",
			in:   "обнови локальный репо",
			want: "обнови локальный репо",
		},
		{
			name: "приветствие и пунктуация срезаются",
			in:   "ок, тогда работаем в этом репо",
			want: "тогда работаем в этом репо",
		},
		{
			name: "длинная фраза режется по запятой",
			in:   "обнови локальные файлы, скажи что там да как и покажи дифф",
			want: "обнови локальные файлы",
		},
		{
			name: "берётся первая фраза",
			in:   "проверь сборку. потом запусти тесты и покажи отчёт",
			want: "проверь сборку",
		},
		{
			name: "код и ссылки не попадают в имя",
			in:   "почему падает\n```go\nfunc main() {}\n```\nhttps://example.com/log",
			want: "почему падает",
		},
		{
			name: "markdown-маркеры убираются",
			in:   "## Разбор **NDIS** портов",
			want: "Разбор NDIS портов",
		},
		{
			name: "одно слово без обрезки",
			in:   "перезапусти",
			want: "перезапусти",
		},
		{
			name: "пустой ввод — пустое имя",
			in:   "   \n\t ",
			want: "",
		},
		{
			name: "одна длинная фраза режется по границе слова",
			in:   "перечитай локальную папку и сравни с репозиторием чтобы понять что отстаёт",
			want: "перечитай локальную папку и сравни с репозиторием",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ShortChatTitle(c.in)
			if got != c.want {
				t.Fatalf("ShortChatTitle(%q) = %q, want %q", c.in, got, c.want)
			}
			if r := []rune(got); len(r) > chatTitleMaxLen {
				t.Fatalf("title %q is %d runes, max %d", got, len(r), chatTitleMaxLen)
			}
		})
	}
}

// Имя, выбранное человеком, автоматика не трогает; дефолтное — трогает.
func TestIsDefaultChatTitle(t *testing.T) {
	def := []string{"", "  ", "Chat", "chat", "Chat 1", "Chat 7", "Chat  12"}
	for _, s := range def {
		if !isDefaultChatTitle(s) {
			t.Fatalf("%q should be treated as default title", s)
		}
	}
	custom := []string{"Chat about NDIS", "Разбор портов", "Chatter", "Чат 3", "Chat 0"}
	for _, s := range custom {
		if isDefaultChatTitle(s) {
			t.Fatalf("%q must not be treated as default title", s)
		}
	}
}

// Заголовок из транскрипта строится по первому вопросу и уже не обрезается
// посреди слова, как прежняя версия с «…» на 40 символах.
func TestTitleFromItemsJSON(t *testing.T) {
	items := `[{"kind":"system","content":"Новый чат"},` +
		`{"kind":"user","content":"перечитай локальную папку и сравни с репозиторием, чтобы понять что отстаёт"}]`
	got := titleFromItemsJSON(items)
	if got != "перечитай локальную папку и сравни с репозиторием" {
		t.Fatalf("title = %q", got)
	}
	if strings.HasSuffix(got, "…") {
		t.Fatalf("title must not be cut mid-word: %q", got)
	}
	if titleFromItemsJSON(`[]`) != "" || titleFromItemsJSON(`not json`) != "" {
		t.Fatal("empty/invalid transcript must give empty title")
	}
}
