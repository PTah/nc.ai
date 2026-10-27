// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:35:24
// Package projmem keeps per-project facts the agent discovered: where a thing
// lives, why a decision was made, which gotcha cost a debugging round.
//
// The point is context economy. A model forgets everything between runs, so
// without storage each new chat re-derives the same knowledge with grep and
// read_file. A note is one line plus the paths it refers to: cheap to inject
// on every run, and the next chat starts where the previous one stopped.
//
// Notes live outside the repository (user config dir), keyed by the absolute
// project path, so memory never pollutes git and never travels in a commit.
package projmem

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"notcursor.ai/app/internal/fsx"
)

const (
	// version invalidates the whole store when the format changes.
	version = 1
	// maxNotes caps one project; oldest notes are evicted first.
	maxNotes = 300
	// maxTextLen keeps each note within one prompt line.
	maxTextLen = 400
	// maxRefs caps paths attached to one note.
	maxRefs = 6
)

// Note is one remembered fact about a project.
type Note struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Refs    []string `json:"refs,omitempty"`
	Created int64    `json:"created"`
	Updated int64    `json:"updated"`
}

// Memory is the whole store of one project.
type Memory struct {
	Version int    `json:"version"`
	Root    string `json:"root"`
	Notes   []Note `json:"notes"`
}

// Store keeps one Memory per project root: in memory and on disk.
type Store struct {
	mu     sync.Mutex
	byRoot map[string]*Memory
	dir    string // "" = memory only
}

// New returns a store; dir may be "" to disable persistence.
func New(dir string) *Store {
	return &Store{byRoot: map[string]*Memory{}, dir: dir}
}

var (
	defaultOnce sync.Once
	defaultSt   *Store
)

// Default is the process-wide store used by the agent tools.
func Default() *Store {
	defaultOnce.Do(func() { defaultSt = New(configDir()) })
	return defaultSt
}

// configDir is <user config>/nc.ai/project-memory, or "" when unavailable.
func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(base, "nc.ai", "project-memory")
}

// Get returns the memory of root, loading it from disk on first use.
func (s *Store) Get(root string) *Memory {
	root = cleanRoot(root)
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.byRoot[root]; m != nil {
		return m
	}
	m := s.loadDisk(root)
	s.byRoot[root] = m
	return m
}

// Remember stores a fact. An equivalent note (same normalised text) is updated
// instead of duplicated, so repeated runs do not pile up copies.
func (s *Store) Remember(root, text string, refs []string) (Note, error) {
	root = cleanRoot(root)
	text = oneLine(text)
	if text == "" {
		return Note{}, fmt.Errorf("text is empty")
	}
	if len(text) > maxTextLen {
		text = strings.TrimSpace(text[:maxTextLen]) + "…"
	}
	refs = cleanRefs(refs)
	now := time.Now().Unix()

	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.byRoot[root]
	if m == nil {
		m = s.loadDisk(root)
		s.byRoot[root] = m
	}

	key := noteKey(text)
	for i := range m.Notes {
		if m.Notes[i].ID == key {
			m.Notes[i].Text = text
			m.Notes[i].Refs = mergeRefs(m.Notes[i].Refs, refs)
			m.Notes[i].Updated = now
			s.saveDisk(m)
			return m.Notes[i], nil
		}
	}

	n := Note{ID: key, Text: text, Refs: refs, Created: now, Updated: now}
	m.Notes = append(m.Notes, n)
	if len(m.Notes) > maxNotes {
		sort.SliceStable(m.Notes, func(i, j int) bool { return m.Notes[i].Updated < m.Notes[j].Updated })
		m.Notes = m.Notes[len(m.Notes)-maxNotes:]
	}
	s.saveDisk(m)
	return n, nil
}

// Forget deletes one note by id (or by exact text).
func (s *Store) Forget(root, idOrText string) bool {
	root = cleanRoot(root)
	want := strings.TrimSpace(idOrText)
	if want == "" {
		return false
	}
	key := noteKey(want)
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.byRoot[root]
	if m == nil {
		m = s.loadDisk(root)
		s.byRoot[root] = m
	}
	kept := make([]Note, 0, len(m.Notes))
	removed := false
	for _, n := range m.Notes {
		if n.ID == want || n.ID == key {
			removed = true
			continue
		}
		kept = append(kept, n)
	}
	if !removed {
		return false
	}
	m.Notes = kept
	s.saveDisk(m)
	return true
}

// Recall returns notes matching query (empty query = all, newest first).
func (s *Store) Recall(root, query string, limit int) []Note {
	root = cleanRoot(root)
	if limit <= 0 {
		limit = 20
	}
	m := s.Get(root)
	notes := append([]Note(nil), m.Notes...)
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].Updated > notes[j].Updated })

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		if len(notes) > limit {
			notes = notes[:limit]
		}
		return notes
	}
	type scored struct {
		n  Note
		sc int
	}
	toks := tokens(q)
	hits := make([]scored, 0, len(notes))
	for _, n := range notes {
		text := strings.ToLower(n.Text)
		refs := strings.ToLower(strings.Join(n.Refs, " "))
		sc := 0
		if strings.Contains(text, q) {
			sc += 100
		}
		for _, t := range toks {
			if strings.Contains(text, t) {
				sc += 20
			}
			if strings.Contains(refs, t) {
				sc += 15
			}
		}
		if sc > 0 {
			hits = append(hits, scored{n, sc})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].sc != hits[j].sc {
			return hits[i].sc > hits[j].sc
		}
		return hits[i].n.Updated > hits[j].n.Updated
	})
	out := make([]Note, 0, limit)
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		out = append(out, h.n)
	}
	return out
}

// Stale reports whether a file mentioned by the note changed after the note was
// written. That is the honest way to use memory: trust it until the code moves.
func Stale(root string, n Note) bool {
	root = cleanRoot(root)
	if root == "" || len(n.Refs) == 0 || n.Updated == 0 {
		return false
	}
	for _, ref := range n.Refs {
		st, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref)))
		if err != nil {
			continue
		}
		if st.ModTime().Unix() > n.Updated {
			return true
		}
	}
	return false
}

// Brief renders the memory block for the agent prompt, capped at maxBytes.
func Brief(root string, maxBytes int) string {
	return Default().Brief(root, maxBytes)
}

// Brief renders this store's memory block for root.
func (s *Store) Brief(root string, maxBytes int) string {
	root = cleanRoot(root)
	m := s.Get(root)
	if len(m.Notes) == 0 {
		return ""
	}
	if maxBytes <= 0 {
		maxBytes = 1200
	}
	notes := append([]Note(nil), m.Notes...)
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].Updated > notes[j].Updated })

	var b strings.Builder
	for _, n := range notes {
		line := "- " + n.Text
		if len(n.Refs) > 0 {
			line += " (" + strings.Join(n.Refs, ", ") + ")"
		}
		if Stale(root, n) {
			line += " — файл изменён после записи, проверь"
		}
		if b.Len()+len(line)+1 > maxBytes {
			b.WriteString("…(memory truncated)")
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatNotes renders notes for the recall tool output.
func FormatNotes(root string, notes []Note) string {
	if len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, n := range notes {
		line := n.ID + " · " + n.Text
		if len(n.Refs) > 0 {
			line += " (" + strings.Join(n.Refs, ", ") + ")"
		}
		if Stale(root, n) {
			line += " [stale: файл изменён]"
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func noteKey(text string) string {
	sum := sha1.Sum([]byte(normalize(text)))
	return hex.EncodeToString(sum[:6])
}

// normalize makes two phrasings of the same one-liner compare equal.
func normalize(text string) string {
	t := strings.ToLower(oneLine(text))
	t = strings.Trim(t, " .;:—-")
	return t
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func cleanRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, r := range refs {
		r = strings.Trim(strings.TrimSpace(strings.ReplaceAll(r, "\\", "/")), "/")
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
		if len(out) == maxRefs {
			break
		}
	}
	return out
}

func mergeRefs(old, add []string) []string {
	return cleanRefs(append(append([]string{}, old...), add...))
}

func tokens(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r >= 'а' && r <= 'я' || r == 'ё' || r == '_' || r == '-' || r == '.')
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.Trim(f, "._-")
		if len(f) < 2 || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func cleanRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

func (s *Store) pathFor(root string) string {
	sum := sha1.Sum([]byte(root))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:8])+".json")
}

func (s *Store) loadDisk(root string) *Memory {
	m := &Memory{Version: version, Root: root, Notes: []Note{}}
	if s.dir == "" || root == "" {
		return m
	}
	raw, err := os.ReadFile(s.pathFor(root))
	if err != nil {
		return m
	}
	var loaded Memory
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return m
	}
	if loaded.Version != version || loaded.Root != root {
		return m
	}
	if loaded.Notes == nil {
		loaded.Notes = []Note{}
	}
	return &loaded
}

func (s *Store) saveDisk(m *Memory) {
	if s.dir == "" || m == nil || m.Root == "" {
		return
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = fsx.WriteFileAtomic(s.pathFor(m.Root), raw, 0o600)
}
