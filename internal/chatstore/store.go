package chatstore

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"notcursor.ai/app/internal/fsx"
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
	// Legacy combined fields kept for migration; prefer per-provider buckets.
	CostUSD       float64 `json:"costUsd,omitempty"`
	InputTokens   int     `json:"inputTokens,omitempty"`
	OutputTokens  int     `json:"outputTokens,omitempty"`
	DeepSeekCostUSD      float64 `json:"deepseekCostUsd,omitempty"`
	DeepSeekInputTokens  int     `json:"deepseekInputTokens,omitempty"`
	DeepSeekOutputTokens int     `json:"deepseekOutputTokens,omitempty"`
	ZaiCostUSD      float64 `json:"zaiCostUsd,omitempty"`
	ZaiInputTokens  int     `json:"zaiInputTokens,omitempty"`
	ZaiOutputTokens int     `json:"zaiOutputTokens,omitempty"`

	OpenRouterCostUSD      float64 `json:"openrouterCostUsd,omitempty"`
	OpenRouterInputTokens  int     `json:"openrouterInputTokens,omitempty"`
	OpenRouterOutputTokens int     `json:"openrouterOutputTokens,omitempty"`
}

// ProjectBundle holds all sessions for one workspace.
type ProjectBundle struct {
	Project  string    `json:"project"`
	ActiveID string    `json:"activeId"`
	Sessions []Session `json:"sessions"`
}

// ArchivedChat is one entry under <AppData>/NotCursor/chat_archive/.
type ArchivedChat struct {
	ID           string        `json:"id"`
	Project      string        `json:"project"`
	ProjectName  string        `json:"projectName"`
	Title        string        `json:"title"`
	ArchivedAt   time.Time     `json:"archivedAt"`
	ItemsJSON    string        `json:"itemsJson"`
	History      []llm.Message `json:"history,omitempty"`
	MessageCount int           `json:"messageCount,omitempty"`
	FileName     string        `json:"fileName,omitempty"`
}

type Store struct {
	mu  sync.Mutex
	dir string // .../NotCursor/chats
}

func New(appDataDir string) (*Store, error) {
	dir := filepath.Join(appDataDir, "chats")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Join(appDataDir, "chat_archive"), 0o700)
	return &Store{dir: dir}, nil
}

func (s *Store) archiveDir() string {
	return filepath.Join(filepath.Dir(s.dir), "chat_archive")
}

func projectDisplayName(project string) string {
	project = strings.TrimSpace(project)
	if project == "" || project == "_global" {
		return "(no project)"
	}
	base := filepath.Base(filepath.Clean(project))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return project
	}
	return base
}

func countItemsJSON(itemsJSON string) int {
	itemsJSON = strings.TrimSpace(itemsJSON)
	if itemsJSON == "" || itemsJSON == "[]" || itemsJSON == "null" {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(itemsJSON), &arr); err != nil {
		return 0
	}
	return len(arr)
}

func (s *Store) writeArchivedFile(project string, sess Session, title string) error {
	archiveTitle := strings.TrimSpace(title)
	if archiveTitle == "" {
		archiveTitle = sess.Title
	}
	if archiveTitle == "" {
		archiveTitle = "Chat"
	}
	dir := s.archiveDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := sanitizeFilename(archiveTitle)
	if len(name) > 120 {
		name = name[:120]
	}
	if len(sess.ID) >= 8 {
		name = name + "-" + sess.ID[:8]
	}
	entry := ArchivedChat{
		ID:           sess.ID,
		Project:      project,
		ProjectName:  projectDisplayName(project),
		Title:        archiveTitle,
		ArchivedAt:   time.Now(),
		ItemsJSON:    sess.ItemsJSON,
		History:      sess.History,
		MessageCount: countItemsJSON(sess.ItemsJSON),
	}
	data, err := json.MarshalIndent(&entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+".json"), data, 0o600)
}

// ListArchived returns all chats from chat_archive (newest first).
// Supports both the current ArchivedChat envelope and legacy Session-only files.
func (s *Store) ListArchived() ([]ArchivedChat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.archiveDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]ArchivedChat, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		chat, ok := parseArchivedFile(data, e.Name())
		if !ok {
			continue
		}
		out = append(out, chat)
	}
	// Newest first.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].ArchivedAt.After(out[i].ArchivedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func parseArchivedFile(data []byte, fileName string) (ArchivedChat, bool) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return ArchivedChat{}, false
	}
	// Current envelope: has projectName and/or archivedAt + itemsJson.
	if _, hasPN := probe["projectName"]; hasPN {
		var c ArchivedChat
		if err := json.Unmarshal(data, &c); err != nil {
			return ArchivedChat{}, false
		}
		if c.Title == "" {
			c.Title = "Chat"
		}
		if c.ProjectName == "" {
			c.ProjectName = projectDisplayName(c.Project)
		}
		if c.MessageCount == 0 {
			c.MessageCount = countItemsJSON(c.ItemsJSON)
		}
		if c.ID == "" {
			c.ID = strings.TrimSuffix(fileName, filepath.Ext(fileName))
		}
		c.FileName = fileName
		return c, true
	}
	if _, hasArchivedAt := probe["archivedAt"]; hasArchivedAt {
		var c ArchivedChat
		if err := json.Unmarshal(data, &c); err != nil {
			return ArchivedChat{}, false
		}
		if c.Title == "" {
			c.Title = "Chat"
		}
		if c.ProjectName == "" {
			c.ProjectName = projectDisplayName(c.Project)
		}
		if c.MessageCount == 0 {
			c.MessageCount = countItemsJSON(c.ItemsJSON)
		}
		if c.ID == "" {
			c.ID = strings.TrimSuffix(fileName, filepath.Ext(fileName))
		}
		c.FileName = fileName
		return c, true
	}
	// Legacy: raw Session JSON.
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil || sess.ID == "" {
		return ArchivedChat{}, false
	}
	title := strings.TrimSpace(sess.Title)
	if title == "" {
		title = "Chat"
	}
	at := sess.UpdatedAt
	if at.IsZero() {
		at = time.Now()
	}
	return ArchivedChat{
		ID:           sess.ID,
		Project:      "",
		ProjectName:  "(unknown)",
		Title:        title,
		ArchivedAt:   at,
		ItemsJSON:    sess.ItemsJSON,
		History:      sess.History,
		MessageCount: countItemsJSON(sess.ItemsJSON),
		FileName:     fileName,
	}, true
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
	return fsx.WriteFileAtomic(s.path(b.Project), data, 0o600)
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
	// No silent fallback: an unknown sessionID must never resolve to another
	// session — it used to let a stale cross-project save overwrite real chat.
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

func (s *Store) ArchiveSession(project, sessionID, title string) (*ProjectBundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i := range b.Sessions {
		if b.Sessions[i].ID == sessionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("session not found")
	}
	sess := b.Sessions[idx]
	if err := s.writeArchivedFile(project, sess, title); err != nil {
		return nil, err
	}
	b.Sessions = append(b.Sessions[:idx], b.Sessions[idx+1:]...)
	if len(b.Sessions) == 0 {
		id := uuid.NewString()
		b.Sessions = []Session{{ID: id, Title: "Chat 1", ItemsJSON: "[]", UpdatedAt: time.Now()}}
		b.ActiveID = id
	} else if b.ActiveID == sessionID {
		b.ActiveID = b.Sessions[0].ID
	}
	if err := s.saveBundle(b); err != nil {
		return nil, err
	}
	return b, nil
}

func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	replacer := strings.NewReplacer(
		"<", "_", ">", "_", ":", "_", "\"", "_", "/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
	)
	s = replacer.Replace(s)
	s = strings.TrimRight(s, ". ")
	if s == "" {
		s = "Chat"
	}
	return s
}

func (s *Store) RenameSession(project, sessionID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return err
	}
	for i := range b.Sessions {
		if b.Sessions[i].ID == sessionID {
			b.Sessions[i].Title = title
			b.Sessions[i].UpdatedAt = time.Now()
			return s.saveBundle(b)
		}
	}
	return fmt.Errorf("session not found")
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

// HasMeaningfulChats reports whether the project bundle has any non-empty chat.
func (s *Store) HasMeaningfulChats(project string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path(project)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	b, err := s.loadBundle(project)
	if err != nil {
		return false, err
	}
	for _, sess := range b.Sessions {
		items := strings.TrimSpace(sess.ItemsJSON)
		if items != "" && items != "[]" && items != "null" {
			return true, nil
		}
		if len(sess.History) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// DeleteBundle removes the on-disk chat file for a project (all tabs).
func (s *Store) DeleteBundle(project string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(project))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(s.path(project) + ".legacy.bak")
	return nil
}

// ArchiveAllSessions writes every session to chat_archive and removes the project bundle.
func (s *Store) ArchiveAllSessions(project string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.loadBundle(project)
	if err != nil {
		return 0, err
	}
	if len(b.Sessions) == 0 {
		_ = os.Remove(s.path(project))
		return 0, nil
	}
	n := 0
	for _, sess := range b.Sessions {
		items := strings.TrimSpace(sess.ItemsJSON)
		if (items == "" || items == "[]" || items == "null") && len(sess.History) == 0 {
			continue
		}
		if err := s.writeArchivedFile(project, sess, sess.Title); err != nil {
			return n, err
		}
		n++
	}
	if err := os.Remove(s.path(project)); err != nil && !os.IsNotExist(err) {
		return n, err
	}
	_ = os.Remove(s.path(project) + ".legacy.bak")
	return n, nil
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
	sess.DeepSeekCostUSD = 0
	sess.DeepSeekInputTokens = 0
	sess.DeepSeekOutputTokens = 0
	sess.ZaiCostUSD = 0
	sess.ZaiInputTokens = 0
	sess.ZaiOutputTokens = 0
	return s.SaveSession(project, sess)
}

// ProviderUsage returns spend for one provider in this session.
// Pre-split sessions: legacy CostUSD is treated as DeepSeek.
func (sess *Session) ProviderUsage(provider string) (cost float64, in, out int) {
	if sess == nil {
		return 0, 0, 0
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "zai":
		return sess.ZaiCostUSD, sess.ZaiInputTokens, sess.ZaiOutputTokens
	case "openrouter":
		return sess.OpenRouterCostUSD, sess.OpenRouterInputTokens, sess.OpenRouterOutputTokens
	default:
		if sess.DeepSeekCostUSD == 0 && sess.DeepSeekInputTokens == 0 && sess.DeepSeekOutputTokens == 0 &&
			sess.ZaiCostUSD == 0 && sess.ZaiInputTokens == 0 && sess.ZaiOutputTokens == 0 &&
			sess.OpenRouterCostUSD == 0 && sess.OpenRouterInputTokens == 0 && sess.OpenRouterOutputTokens == 0 &&
			(sess.CostUSD != 0 || sess.InputTokens != 0 || sess.OutputTokens != 0) {
			return sess.CostUSD, sess.InputTokens, sess.OutputTokens
		}
		return sess.DeepSeekCostUSD, sess.DeepSeekInputTokens, sess.DeepSeekOutputTokens
	}
}

// AddUsage accumulates per-chat spend counters on the given session and persists.
func (s *Store) AddUsage(project, sessionID, provider string, costUSD float64, inputTokens, outputTokens int) (*Session, error) {
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
			switch strings.ToLower(strings.TrimSpace(provider)) {
			case "zai":
				b.Sessions[i].ZaiCostUSD += costUSD
				b.Sessions[i].ZaiInputTokens += inputTokens
				b.Sessions[i].ZaiOutputTokens += outputTokens
			case "openrouter":
				b.Sessions[i].OpenRouterCostUSD += costUSD
				b.Sessions[i].OpenRouterInputTokens += inputTokens
				b.Sessions[i].OpenRouterOutputTokens += outputTokens
			default:
				b.Sessions[i].DeepSeekCostUSD += costUSD
				b.Sessions[i].DeepSeekInputTokens += inputTokens
				b.Sessions[i].DeepSeekOutputTokens += outputTokens
			}
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
