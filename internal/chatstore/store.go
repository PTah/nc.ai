package chatstore

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"notcursor.ai/app/internal/llm"
)

// Session is one chat tab (multitasking).
type Session struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	ItemsJSON string        `json:"itemsJson"`
	History   []llm.Message `json:"history"`
	UpdatedAt time.Time     `json:"updatedAt"`
	// Per-chat API spend (survives restarts with the session JSON).
	CostUSD       float64 `json:"costUsd,omitempty"`
	InputTokens   int     `json:"inputTokens,omitempty"`
	OutputTokens  int     `json:"outputTokens,omitempty"`
}

// ProjectBundle holds all sessions for one workspace.
type ProjectBundle struct {
	Project  string    `json:"project"`
	ActiveID string    `json:"activeId"`
	Sessions []Session `json:"sessions"`
}

type Store struct {
	mu  sync.Mutex
	dir string
}

func New(appDataDir string) (*Store, error) {
	dir := filepath.Join(appDataDir, "chats")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func keyFor(project string) string {
	if project == "" {
		project = "_global"
	}
	sum := sha1.Sum([]byte(project))
	return hex.EncodeToString(sum[:12])
}

func (s *Store) path(project string) string {
	return filepath.Join(s.dir, keyFor(project)+".json")
}

func (s *Store) loadBundle(project string) (*ProjectBundle, error) {
	data, err := os.ReadFile(s.path(project))
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectBundle{Project: project, Sessions: nil}, nil
		}
		return nil, err
	}
	// Migrate old single-State format → multi-session and persist once.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	if _, ok := probe["sessions"]; !ok {
		var old struct {
			Project   string        `json:"project"`
			ItemsJSON string        `json:"itemsJson"`
			History   []llm.Message `json:"history"`
			UpdatedAt time.Time     `json:"updatedAt"`
		}
		if err := json.Unmarshal(data, &old); err != nil {
			return nil, err
		}
		id := uuid.NewString()
		updated := old.UpdatedAt
		if updated.IsZero() {
			updated = time.Now()
		}
		b := &ProjectBundle{
			Project:  project,
			ActiveID: id,
			Sessions: []Session{{
				ID:        id,
				Title:     "Chat 1",
				ItemsJSON: old.ItemsJSON,
				History:   old.History,
				UpdatedAt: updated,
			}},
		}
		// Keep a one-shot backup of the legacy file, then rewrite as multi-session.
		_ = os.WriteFile(s.path(project)+".legacy.bak", data, 0o600)
		if err := s.saveBundle(b); err != nil {
			return nil, fmt.Errorf("migrate legacy chat: %w", err)
		}
		return b, nil
	}
	var b ProjectBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	b.Project = project
	return &b, nil
}

func (s *Store) saveBundle(b *ProjectBundle) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(b.Project), data, 0o600)
}

func (s *Store) List(project string) (*ProjectBundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return nil, err
	}
	if len(b.Sessions) == 0 {
		id := uuid.NewString()
		b.Sessions = []Session{{
			ID:        id,
			Title:     "Chat 1",
			ItemsJSON: "[]",
			UpdatedAt: time.Now(),
		}}
		b.ActiveID = id
		_ = s.saveBundle(b)
	}
	if b.ActiveID == "" {
		b.ActiveID = b.Sessions[0].ID
	}
	return b, nil
}

func (s *Store) NewSession(project, title string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = fmt.Sprintf("Chat %d", len(b.Sessions)+1)
	}
	sess := Session{
		ID:        uuid.NewString(),
		Title:     title,
		ItemsJSON: "[]",
		UpdatedAt: time.Now(),
	}
	b.Sessions = append(b.Sessions, sess)
	b.ActiveID = sess.ID
	if err := s.saveBundle(b); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) SetActive(project, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return err
	}
	found := false
	for _, sess := range b.Sessions {
		if sess.ID == sessionID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("session not found")
	}
	b.ActiveID = sessionID
	return s.saveBundle(b)
}

func (s *Store) Get(project, sessionID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return nil, err
	}
	if sessionID == "" {
		sessionID = b.ActiveID
	}
	for i := range b.Sessions {
		if b.Sessions[i].ID == sessionID {
			cp := b.Sessions[i]
			return &cp, nil
		}
	}
	// Stale id from a pre-persist migration attempt: fall back to active / first tab.
	if b.ActiveID != "" && b.ActiveID != sessionID {
		for i := range b.Sessions {
			if b.Sessions[i].ID == b.ActiveID {
				cp := b.Sessions[i]
				return &cp, nil
			}
		}
	}
	if len(b.Sessions) > 0 {
		cp := b.Sessions[0]
		return &cp, nil
	}
	return nil, fmt.Errorf("session not found")
}

func (s *Store) SaveSession(project string, sess *Session) error {
	if sess == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return err
	}
	sess.UpdatedAt = time.Now()
	found := false
	for i := range b.Sessions {
		if b.Sessions[i].ID == sess.ID {
			b.Sessions[i] = *sess
			found = true
			break
		}
	}
	if !found {
		b.Sessions = append(b.Sessions, *sess)
	}
	b.ActiveID = sess.ID
	return s.saveBundle(b)
}

func (s *Store) DeleteSession(project, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return err
	}
	out := b.Sessions[:0]
	for _, sess := range b.Sessions {
		if sess.ID != sessionID {
			out = append(out, sess)
		}
	}
	b.Sessions = out
	if len(b.Sessions) == 0 {
		id := uuid.NewString()
		b.Sessions = []Session{{ID: id, Title: "Chat 1", ItemsJSON: "[]", UpdatedAt: time.Now()}}
		b.ActiveID = id
	} else if b.ActiveID == sessionID {
		b.ActiveID = b.Sessions[0].ID
	}
	return s.saveBundle(b)
}

// Legacy helpers used by older UI paths.
type State struct {
	Project   string        `json:"project"`
	ItemsJSON string        `json:"itemsJson"`
	History   []llm.Message `json:"history"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

func (s *Store) Load(project string) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return nil, err
	}
	if len(b.Sessions) == 0 {
		return &State{Project: project, ItemsJSON: "[]"}, nil
	}
	sess := b.Sessions[0]
	for i := range b.Sessions {
		if b.Sessions[i].ID == b.ActiveID {
			sess = b.Sessions[i]
			break
		}
	}
	return &State{
		Project:   project,
		ItemsJSON: sess.ItemsJSON,
		History:   sess.History,
		UpdatedAt: sess.UpdatedAt,
	}, nil
}

func (s *Store) Save(st *State) error {
	if st == nil {
		return nil
	}
	b, err := s.List(st.Project)
	if err != nil {
		return err
	}
	sess, err := s.Get(st.Project, b.ActiveID)
	if err != nil {
		sess, err = s.NewSession(st.Project, "Chat 1")
		if err != nil {
			return err
		}
	}
	sess.ItemsJSON = st.ItemsJSON
	sess.History = st.History
	return s.SaveSession(st.Project, sess)
}

func (s *Store) Clear(project string) error {
	b, err := s.List(project)
	if err != nil {
		return err
	}
	sess, err := s.Get(project, b.ActiveID)
	if err != nil {
		return err
	}
	sess.ItemsJSON = "[]"
	sess.History = nil
	sess.Title = "Chat"
	sess.CostUSD = 0
	sess.InputTokens = 0
	sess.OutputTokens = 0
	return s.SaveSession(project, sess)
}

// AddUsage accumulates per-chat spend counters on the given session and persists.
func (s *Store) AddUsage(project, sessionID string, costUSD float64, inputTokens, outputTokens int) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return nil, err
	}
	if sessionID == "" {
		sessionID = b.ActiveID
	}
	for i := range b.Sessions {
		if b.Sessions[i].ID == sessionID {
			b.Sessions[i].CostUSD += costUSD
			b.Sessions[i].InputTokens += inputTokens
			b.Sessions[i].OutputTokens += outputTokens
			b.Sessions[i].UpdatedAt = time.Now()
			if err := s.saveBundle(b); err != nil {
				return nil, err
			}
			cp := b.Sessions[i]
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("session not found")
}
