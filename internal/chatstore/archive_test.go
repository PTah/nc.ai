package chatstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveAndList(t *testing.T) {
	appData := t.TempDir()
	s, err := New(appData)
	if err != nil {
		t.Fatal(err)
	}
	proj := `D:\Soft\Git\nc.ai`
	sess, err := s.NewSession(proj, "Ctrl-Alt-T chat")
	if err != nil {
		t.Fatal(err)
	}
	sess.ItemsJSON = `[{"kind":"user","content":"hi"},{"kind":"assistant","content":"ok"}]`
	if err := s.SaveSession(proj, sess); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveSession(proj, sess.ID, sess.Title); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListArchived()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("archived=%d want 1", len(list))
	}
	if list[0].ProjectName != "nc.ai" {
		t.Fatalf("projectName=%q", list[0].ProjectName)
	}
	if list[0].MessageCount != 2 {
		t.Fatalf("messageCount=%d", list[0].MessageCount)
	}

	legacy := Session{ID: "legacy-1", Title: "old", ItemsJSON: `[{"kind":"user","content":"x"}]`, UpdatedAt: time.Now()}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appData, "chat_archive", "legacy.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListArchived()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("archived=%d want 2", len(list))
	}
}
