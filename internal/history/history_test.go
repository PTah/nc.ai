// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:39:06
package history

import (
	"strings"
	"testing"
	"time"

	"notcursor.ai/app/internal/chatstore"
	"notcursor.ai/app/internal/llm"
)

func newStore(t *testing.T) *chatstore.Store {
	t.Helper()
	st, err := chatstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("chatstore: %v", err)
	}
	return st
}

func saveChat(t *testing.T, st *chatstore.Store, project, id, title string, msgs []llm.Message) {
	t.Helper()
	err := st.SaveSession(project, &chatstore.Session{
		ID:        id,
		Title:     title,
		History:   msgs,
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
}

func TestSearchFindsRememberedDiscussion(t *testing.T) {
	st := newStore(t)
	project := t.TempDir()
	saveChat(t, st, project, "c1", "разбор NetLynx", []llm.Message{
		{Role: "user", Content: "Как считается порт монитора в NetLynx?"},
		{Role: "assistant", Content: "Порт — это hex от строки Windows NDIS, парсится в internal/netx/ndis.go."},
	})
	saveChat(t, st, project, "c2", "сборка exe", []llm.Message{
		{Role: "user", Content: "Как собрать установщик?"},
		{Role: "assistant", Content: "build.ps1, артефакт в build/bin."},
	})
	// Another project must not leak into results.
	saveChat(t, st, t.TempDir(), "x1", "чужая беседа", []llm.Message{
		{Role: "user", Content: "порт hex NDIS"},
	})

	hits := New(st).Search(project, "hex NDIS порт", 5)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].SessionID != "c1" {
		t.Fatalf("wrong chat ranked first: %+v", hits[0])
	}
	if hits[0].Title != "разбор NetLynx" {
		t.Fatalf("title = %q", hits[0].Title)
	}
	if !strings.Contains(hits[0].Snippet, "NDIS") {
		t.Fatalf("snippet misses the match: %q", hits[0].Snippet)
	}
	for _, h := range hits {
		if h.SessionID == "x1" {
			t.Fatal("foreign project leaked into results")
		}
	}
}

func TestSearchIgnoresShortAndEmptyQueries(t *testing.T) {
	st := newStore(t)
	project := t.TempDir()
	saveChat(t, st, project, "c1", "t", []llm.Message{{Role: "user", Content: "про порт и hex"}})

	if hits := New(st).Search(project, "  ", 5); len(hits) != 0 {
		t.Fatalf("empty query must return nothing: %+v", hits)
	}
	if hits := New(st).Search(project, "a i", 5); len(hits) != 0 {
		t.Fatalf("one-letter tokens must be ignored: %+v", hits)
	}
	if hits := New(st).Search(project, "порт", 5); len(hits) == 0 {
		t.Fatal("expected a hit")
	}
}

func TestSearchLimitsPerSession(t *testing.T) {
	st := newStore(t)
	project := t.TempDir()
	msgs := make([]llm.Message, 0, 20)
	for i := 0; i < 10; i++ {
		msgs = append(msgs,
			llm.Message{Role: "user", Content: "вопрос про порт NDIS номер " + strings.Repeat("x", i+1)},
			llm.Message{Role: "assistant", Content: "ответ про порт NDIS " + strings.Repeat("y", i+1)},
		)
	}
	saveChat(t, st, project, "c1", "много про порт", msgs)

	hits := New(st).Search(project, "NDIS порт", 20)
	if len(hits) > perSession {
		t.Fatalf("per-session cap broken: %d hits", len(hits))
	}
}

func TestNilSearcherAndFormat(t *testing.T) {
	var s *Searcher
	if hits := s.Search("/tmp/x", "query", 3); hits != nil {
		t.Fatalf("nil searcher returned %+v", hits)
	}
	if New(nil).Search("/tmp/x", "query", 3) != nil {
		t.Fatal("nil store must return nothing")
	}

	out := Format([]Hit{{
		SessionID: "abc123", Title: "разбор", Role: "assistant",
		Updated: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		Snippet: "…порт — hex от строки Windows NDIS…",
	}})
	if !strings.Contains(out, "«разбор»") || !strings.Contains(out, "abc123") || !strings.Contains(out, "NDIS") {
		t.Fatalf("format output unexpected:\n%s", out)
	}
}
