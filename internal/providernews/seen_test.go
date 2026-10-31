// Created by nc.ai 1.0.0 on Papasha 2026-10-31 10:39:04
package providernews

import (
	"testing"
	"time"
)

// PayloadOf помечает записи, которых не было в прошлом дайджесте, и переносит
// отметку «анализ уже открывали» — по ней UI гасит зелёную подсветку кнопки.
func TestPayloadOfMarksNewAndSeen(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	s := Snapshot{
		UpdatedAt: now,
		PrevIDs:   []string{"old"},
		SeenIDs:   []string{"fresh-read"},
		Items: []Item{
			{ID: "old"},
			{ID: "fresh-unread"},
			{ID: "fresh-read"},
		},
	}
	p := PayloadOf(s, now)
	if p.NewCount != 2 {
		t.Fatalf("новых должно быть 2: %+v", p)
	}
	want := map[string]struct{ new, seen bool }{
		"old":          {false, false},
		"fresh-unread": {true, false},
		"fresh-read":   {true, true},
	}
	for _, it := range p.Items {
		w, ok := want[it.ID]
		if !ok {
			t.Fatalf("неожиданная запись %q", it.ID)
		}
		if it.New != w.new || it.Seen != w.seen {
			t.Errorf("%s: new=%v seen=%v, ожидалось new=%v seen=%v", it.ID, it.New, it.Seen, w.new, w.seen)
		}
	}
	// В кэш флаги не попадают: снапшот на диске остаётся как был.
	if s.Items[1].New || s.Items[2].Seen {
		t.Errorf("PayloadOf не должен менять исходный снапшот: %+v", s.Items)
	}
}

func TestPruneSeenDropsGoneItems(t *testing.T) {
	got := pruneSeen([]string{"a", "c"}, []string{"a", "b", "c"})
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("отметки исчезнувших записей не вычищены: %+v", got)
	}
	if pruneSeen(nil, nil) != nil {
		t.Errorf("на пустом входе должен быть nil")
	}
}

func TestPruneSeenCapsLength(t *testing.T) {
	ids := make([]string, 0, MaxSeenIDs+10)
	seen := make([]string, 0, MaxSeenIDs+10)
	for i := 0; i < MaxSeenIDs+10; i++ {
		id := string(rune('a'+i%26)) + string(rune('0'+i/26))
		ids = append(ids, id)
		seen = append(seen, id)
	}
	if got := pruneSeen(ids, seen); len(got) > MaxSeenIDs {
		t.Errorf("список прочитанного не ограничен: %d", len(got))
	}
}
