// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:35:33
package projmem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRememberDeduplicatesAndPersists(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	st := New(dir)

	n, err := st.Remember(root, "  Порт NDIS   считается как hex от строки Windows  ", []string{"internal/netx/ndis.go"})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if n.ID == "" {
		t.Fatal("empty note id")
	}
	if strings.Contains(n.Text, "  ") {
		t.Fatalf("text not collapsed: %q", n.Text)
	}

	// Same fact, different spacing/case → one note, merged refs.
	if _, err := st.Remember(root, "порт ndis считается как hex от строки windows", []string{"internal/netx/events.go"}); err != nil {
		t.Fatalf("remember again: %v", err)
	}
	notes := st.Recall(root, "", 10)
	if len(notes) != 1 {
		t.Fatalf("expected one deduplicated note, got %d: %+v", len(notes), notes)
	}
	if len(notes[0].Refs) != 2 {
		t.Fatalf("refs not merged: %+v", notes[0].Refs)
	}

	// A fresh store must read the note back from disk.
	again := New(dir).Recall(root, "ndis", 5)
	if len(again) != 1 {
		t.Fatalf("disk reload lost the note: %+v", again)
	}
}

func TestRecallRanksByQuery(t *testing.T) {
	root := t.TempDir()
	st := New("")
	mustRemember(t, st, root, "События online/offline живут в internal/netx/events.go", []string{"internal/netx/events.go"})
	mustRemember(t, st, root, "Сборка exe: build.ps1, артефакт build/bin", []string{"build.ps1"})
	mustRemember(t, st, root, "Конфиг хранится в config.json профиля", nil)

	hits := st.Recall(root, "online offline", 5)
	if len(hits) == 0 || !strings.Contains(hits[0].Text, "online/offline") {
		t.Fatalf("query ranking failed: %+v", hits)
	}
	if all := st.Recall(root, "", 2); len(all) != 2 {
		t.Fatalf("empty query must respect limit, got %d", len(all))
	}
}

func TestBriefMarksStaleNotes(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "internal", "netx")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(src, "events.go")
	if err := os.WriteFile(file, []byte("package netx\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	st := New("")
	if _, err := st.Remember(root, "События online/offline — internal/netx/events.go", []string{"internal/netx/events.go"}); err != nil {
		t.Fatalf("remember: %v", err)
	}
	brief := st.Brief(root, 2000)
	if !strings.Contains(brief, "online/offline") {
		t.Fatalf("brief misses note:\n%s", brief)
	}
	if strings.Contains(brief, "изменён") {
		t.Fatalf("fresh note flagged stale:\n%s", brief)
	}

	// Touch the file into the future: the note must be reported as stale.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(file, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	brief = st.Brief(root, 2000)
	if !strings.Contains(brief, "изменён") {
		t.Fatalf("stale note not flagged:\n%s", brief)
	}
}

func TestBriefRespectsBudgetAndForget(t *testing.T) {
	root := t.TempDir()
	st := New("")
	for i := 0; i < 40; i++ {
		if _, err := st.Remember(root, strings.Repeat("факт о проекте ", 4)+string(rune('a'+i)), nil); err != nil {
			t.Fatalf("remember: %v", err)
		}
	}
	brief := st.Brief(root, 300)
	if len(brief) > 400 {
		t.Fatalf("brief not capped: %d bytes", len(brief))
	}

	notes := st.Recall(root, "", 5)
	if len(notes) == 0 {
		t.Fatal("no notes")
	}
	if !st.Forget(root, notes[0].ID) {
		t.Fatal("forget failed")
	}
	if got := len(st.Recall(root, "", 100)); got != 39 {
		t.Fatalf("expected 39 notes after forget, got %d", got)
	}
	if st.Forget(root, "no-such-id") {
		t.Fatal("forget of missing note must return false")
	}
}

func TestRememberRejectsEmpty(t *testing.T) {
	if _, err := New("").Remember(t.TempDir(), "   ", nil); err == nil {
		t.Fatal("empty text must fail")
	}
}

func mustRemember(t *testing.T, st *Store, root, text string, refs []string) {
	t.Helper()
	if _, err := st.Remember(root, text, refs); err != nil {
		t.Fatalf("remember %q: %v", text, err)
	}
}
