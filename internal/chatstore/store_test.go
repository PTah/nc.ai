package chatstore

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"notcursor.ai/app/internal/llm"
)

func legacyPath(appDataDir, project string) string {
	sum := sha1.Sum([]byte(project))
	return filepath.Join(appDataDir, "chats", hex.EncodeToString(sum[:12])+".json")
}

func TestMigrationFromSingleState(t *testing.T) {
	appData := t.TempDir()
	old := struct {
		Project   string        `json:"project"`
		ItemsJSON string        `json:"itemsJson"`
		History   []llm.Message `json:"history"`
		UpdatedAt time.Time     `json:"updatedAt"`
	}{
		Project:   "/proj",
		ItemsJSON: `[{"kind":"user","content":"hi"}]`,
		History: []llm.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
		UpdatedAt: time.Now(),
	}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath(appData, "/proj")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath(appData, "/proj"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := New(appData)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.List("/proj")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(b.Sessions))
	}
	sess := b.Sessions[0]
	if sess.Title != "Chat 1" {
		t.Fatalf("title = %q, want %q", sess.Title, "Chat 1")
	}
	if sess.ItemsJSON != old.ItemsJSON {
		t.Fatalf("itemsJson = %q, want %q", sess.ItemsJSON, old.ItemsJSON)
	}
	if len(sess.History) != 2 || sess.History[1].Content != "hello" {
		t.Fatalf("history not preserved: %+v", sess.History)
	}
	if b.ActiveID == "" {
		t.Fatal("activeId empty after migration")
	}

	// Migration must be persisted so List → Get (separate loads) keep the same id.
	raw, err := os.ReadFile(legacyPath(appData, "/proj"))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if _, ok := probe["sessions"]; !ok {
		t.Fatal("legacy file was not rewritten as multi-session")
	}
	if _, err := os.Stat(legacyPath(appData, "/proj") + ".legacy.bak"); err != nil {
		t.Fatalf("legacy backup missing: %v", err)
	}

	b2, err := s.List("/proj")
	if err != nil {
		t.Fatal(err)
	}
	if b2.ActiveID != b.ActiveID {
		t.Fatalf("activeId changed across List: %q → %q", b.ActiveID, b2.ActiveID)
	}
	got, err := s.Get("/proj", b.ActiveID)
	if err != nil {
		t.Fatalf("Get after migrate: %v", err)
	}
	if got.ItemsJSON != old.ItemsJSON {
		t.Fatalf("Get itemsJson = %q, want legacy content", got.ItemsJSON)
	}
}

func TestMigrationListThenGetStable(t *testing.T) {
	// Reproduces the UI restoreChat path: ListChatSessions then LoadChat/Get.
	appData := t.TempDir()
	legacy := []byte(`{"project":"/p","itemsJson":"[{\"kind\":\"user\",\"content\":\"keep me\"}]","history":[{"role":"user","content":"keep me"}]}`)
	if err := os.MkdirAll(filepath.Dir(legacyPath(appData, "/p")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath(appData, "/p"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(appData)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.List("/p")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.Get("/p", b.ActiveID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ItemsJSON == "" || sess.ItemsJSON == "[]" {
		t.Fatalf("restored empty transcript: %q", sess.ItemsJSON)
	}
}

func TestSessionCRUD(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const proj = "/proj"

	// First List creates a default tab.
	b, err := s.List(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(b.Sessions))
	}
	firstID := b.Sessions[0].ID
	if b.ActiveID != firstID {
		t.Fatalf("activeId = %q, want %q", b.ActiveID, firstID)
	}

	// Add a second tab and switch to it.
	second, err := s.NewSession(proj, "Second")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive(proj, second.ID); err != nil {
		t.Fatal(err)
	}
	b, _ = s.List(proj)
	if len(b.Sessions) != 2 || b.ActiveID != second.ID {
		t.Fatalf("bundle after NewSession+SetActive = %+v", b)
	}

	// Update and read back a session.
	second.Title = "Renamed"
	second.History = []llm.Message{{Role: "user", Content: "x"}}
	if err := s.SaveSession(proj, second); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(proj, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Renamed" || len(got.History) != 1 {
		t.Fatalf("saved session = %+v", got)
	}

	// Delete the second tab; active falls back to the first.
	if err := s.DeleteSession(proj, second.ID); err != nil {
		t.Fatal(err)
	}
	b, _ = s.List(proj)
	if len(b.Sessions) != 1 || b.ActiveID != firstID {
		t.Fatalf("bundle after delete = %+v", b)
	}
}

func TestSetActiveUnknownSession(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive("/proj", "nope"); err == nil {
		t.Fatal("SetActive(unknown) = nil error")
	}
}
