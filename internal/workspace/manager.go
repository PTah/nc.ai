package workspace

import (
	"fmt"
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

func (m *Manager) resolve(rel string) (string, error) {
	root, err := m.ActiveRoot()
	if err != nil {
		return "", err
	}
	clean := filepath.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))
	clean = strings.TrimPrefix(clean, "/")
	full := filepath.Join(root, clean)
	full = filepath.Clean(full)
	relToRoot, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(relToRoot, "..") {
		return "", fmt.Errorf("path escapes workspace")
	}
	return full, nil
}

func (m *Manager) ListDir(rel string) ([]Entry, error) {
	full, err := m.resolve(rel)
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
		if name == ".git" || name == "node_modules" || name == "frontend/dist" {
			continue
		}
		child := filepath.ToSlash(filepath.Join(rel, name))
		if rel == "" || rel == "." {
			child = name
		}
		out = append(out, Entry{
			Name:  name,
			Path:  child,
			IsDir: it.IsDir(),
		})
	}
	return out, nil
}

const maxReadBytes = 512 * 1024

func (m *Manager) ReadFile(rel string) (string, error) {
	full, err := m.resolve(rel)
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
