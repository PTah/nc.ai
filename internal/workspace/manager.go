package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Project struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Opened string `json:"opened"`
}

type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

type Manager struct {
	mu       sync.RWMutex
	projects []Project
	active   string
}

func NewManager() *Manager {
	return &Manager{projects: []Project{}}
}

func (m *Manager) List() []Project {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Project, len(m.projects))
	copy(out, m.projects)
	return out
}

func (m *Manager) Open(path string) (*Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", abs)
	}
	p := Project{
		Name:   filepath.Base(abs),
		Path:   abs,
		Opened: time.Now().Format(time.RFC3339),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for i, existing := range m.projects {
		if existing.Path == abs {
			m.projects[i] = p
			found = true
			break
		}
	}
	if !found {
		m.projects = append([]Project{p}, m.projects...)
	}
	m.active = abs
	return &p, nil
}

func (m *Manager) ActiveRoot() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == "" {
		return "", fmt.Errorf("no active project")
	}
	return m.active, nil
}

func (m *Manager) SetActive(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	m.mu.Lock()
	m.active = abs
	m.mu.Unlock()
	return nil
}

func (m *Manager) Resolve(rel string) (string, error) {
	root, err := m.ActiveRoot()
	if err != nil {
		return "", err
	}
	if rel == "" || rel == "." {
		return root, nil
	}
	clean := filepath.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") ||
		filepath.IsAbs(clean) || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	full := filepath.Clean(filepath.Join(root, clean))
	relToRoot, err := filepath.Rel(root, full)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return full, nil
}

func ignoredName(name string) bool {
	switch name {
	case ".git", "node_modules", "frontend/dist", "dist", ".wails", "build/bin":
		return true
	default:
		return false
	}
}

func (m *Manager) ListDir(rel string) ([]Entry, error) {
	full, err := m.Resolve(rel)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		name := it.Name()
		if ignoredName(name) {
			continue
		}
		child := name
		if rel != "" && rel != "." {
			child = filepath.ToSlash(filepath.Join(rel, name))
		}
		out = append(out, Entry{Name: name, Path: child, IsDir: it.IsDir()})
	}
	return out, nil
}

const maxReadBytes = 512 * 1024

// errSearchLimit stops the file walk early once enough results are collected.
var errSearchLimit = errors.New("search limit reached")

func (m *Manager) ReadFile(rel string) (string, error) {
	full, err := m.Resolve(rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", m.notFoundHint(rel, full)
		}
		return "", err
	}
	if len(data) > maxReadBytes {
		data = append(data[:maxReadBytes:maxReadBytes], []byte("\n\n/* truncated */")...)
	}
	return m.guardRead(rel, string(data))
}

func (m *Manager) notFoundHint(rel, full string) error {
	dir := filepath.Dir(full)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("file not found: %s (and directory listing failed: %v)", rel, err)
	}
	want := strings.ToLower(filepath.Base(rel))
	var names []string
	var similar []string
	for _, e := range entries {
		n := e.Name()
		names = append(names, n)
		ln := strings.ToLower(n)
		if strings.Contains(ln, strings.TrimSuffix(want, filepath.Ext(want))) ||
			strings.Contains(want, strings.TrimSuffix(ln, filepath.Ext(ln))) {
			similar = append(similar, n)
		}
	}
	msg := fmt.Sprintf("file not found: %s", rel)
	if len(similar) > 0 {
		msg += fmt.Sprintf("\nDid you mean: %s", strings.Join(similar, ", "))
	}
	if len(names) > 0 {
		msg += fmt.Sprintf("\nFiles in %s: %s", filepath.ToSlash(filepath.Dir(rel)), strings.Join(names, ", "))
	} else {
		msg += "\nDirectory is empty or missing."
	}
	msg += "\nUse list_dir or search_files — do not invent paths."
	return fmt.Errorf("%s", msg)
}

func (m *Manager) WriteFile(rel, content string) error {
	full, err := m.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

// ReadFileRaw returns file bytes without secret masking (for patch tools).
func (m *Manager) ReadFileRaw(rel string) (string, error) {
	full, err := m.Resolve(rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", m.notFoundHint(rel, full)
		}
		return "", err
	}
	return string(data), nil
}

func (m *Manager) DeletePath(rel string) error {
	full, err := m.Resolve(rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(full)
}

func (m *Manager) Mkdir(rel string) error {
	full, err := m.Resolve(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(full, 0o755)
}

// SearchFiles finds files whose relative path or content contains query (simple scan).
func (m *Manager) SearchFiles(query string, limit int) ([]string, error) {
	root, err := m.ActiveRoot()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	q := strings.ToLower(query)
	var hits []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if ignoredName(name) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.Contains(strings.ToLower(rel), q) {
			hits = append(hits, rel)
			if len(hits) >= limit {
				return errSearchLimit
			}
			return nil
		}
		// light content search for text-ish files under 100KB
		info, e := d.Info()
		if e != nil || info.Size() > 100*1024 {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(string(data)), q) {
			hits = append(hits, rel)
			if len(hits) >= limit {
				return errSearchLimit
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSearchLimit) {
		return hits, err
	}
	return hits, nil
}
