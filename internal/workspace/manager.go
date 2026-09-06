package workspace

import (
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
	clean := filepath.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))
	clean = strings.TrimPrefix(clean, "/")
	full := filepath.Clean(filepath.Join(root, clean))
	relToRoot, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(relToRoot, "..") {
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

func (m *Manager) ReadFile(rel string) (string, error) {
	full, err := m.Resolve(rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if len(data) > maxReadBytes {
		return string(data[:maxReadBytes]) + "\n\n/* truncated */", nil
	}
	return string(data), nil
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
				return fmt.Errorf("done")
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
				return fmt.Errorf("done")
			}
		}
		return nil
	})
	if err != nil && err.Error() != "done" {
		return hits, err
	}
	return hits, nil
}
