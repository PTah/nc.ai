package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Project struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Roots   []string `json:"roots,omitempty"` // folders merged into this workspace; [0] == Path
	Opened  string   `json:"opened"`
	IconURL string   `json:"iconUrl,omitempty"` // data URL from project folder icon, if any
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
	// roots — roots of the active workspace for a scoped (per-run) manager.
	// Empty means "derive from the project list" (the shared manager).
	roots []string
}

func NewManager() *Manager {
	return &Manager{projects: []Project{}}
}

func (m *Manager) List() []Project {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Project, len(m.projects))
	copy(out, m.projects)
	// Resolve icons outside the lock copy: cheap reads, keeps List payload fresh
	// if the user drops an icon into the project folder while the app is open.
	for i := range out {
		out[i].IconURL = FindIconDataURL(out[i].Path)
	}
	// В панели проектов список должен идти по алфавиту, а не в порядке
	// добавления. Порядок в настройках не меняем — сортируем только выдачу.
	sort.SliceStable(out, func(i, j int) bool {
		an, bn := strings.ToLower(strings.TrimSpace(out[i].Name)), strings.ToLower(strings.TrimSpace(out[j].Name))
		if an == bn {
			return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
		}
		return an < bn
	})
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
		Name:    filepath.Base(abs),
		Path:    abs,
		Roots:   []string{abs},
		Opened:  time.Now().Format(time.RFC3339),
		IconURL: FindIconDataURL(abs),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for i, existing := range m.projects {
		if existing.Path == abs {
			// Keep roots added to this workspace earlier; only refresh metadata.
			if len(existing.Roots) > 0 {
				p.Roots = append([]string{}, existing.Roots...)
			}
			m.projects[i] = p
			found = true
			break
		}
	}
	if !found {
		m.projects = append([]Project{p}, m.projects...)
	}
	m.active = abs
	m.roots = nil
	return &p, nil
}

// NewFolder creates a project folder named name directly inside parent and opens
// it. The name is validated (ValidateFolderName), parent must be an existing
// directory and the target must not exist yet — so "Create project" never
// silently lands in, or reopens, a folder the user did not intend.
func (m *Manager) NewFolder(parent, name string) (*Project, error) {
	name = strings.TrimSpace(name)
	if err := ValidateFolderName(name); err != nil {
		return nil, err
	}
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return nil, fmt.Errorf("выберите папку, где создать проект")
	}
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(absParent)
	if err != nil {
		return nil, fmt.Errorf("папка недоступна: %s", absParent)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("не папка: %s", absParent)
	}
	full := filepath.Join(absParent, name)
	if _, err := os.Stat(full); err == nil {
		return nil, fmt.Errorf("папка уже существует: %s — её можно открыть как существующий проект", full)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		return nil, err
	}
	return m.Open(full)
}

// ValidateFolderName rejects names that would climb out of the parent folder or
// break on Windows (path separators, reserved characters, reserved device
// names, trailing dot/space). Only the folder name is validated; the parent
// always comes from a directory picker.
func ValidateFolderName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("введите имя проекта")
	case name == "." || name == "..":
		return fmt.Errorf("недопустимое имя проекта: %q", name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("имя проекта не должно содержать разделители пути — папку выберите отдельно")
	case strings.ContainsAny(name, `<>:"|?*`):
		return fmt.Errorf(`имя проекта не должно содержать символы < > : " | ? *`)
	case strings.HasSuffix(name, "."), strings.HasSuffix(name, " "):
		return fmt.Errorf("имя проекта не должно заканчиваться точкой или пробелом")
	}
	for _, r := range name {
		if r < 0x20 {
			return fmt.Errorf("имя проекта не должно содержать управляющие символы")
		}
	}
	if isWindowsReservedName(name) {
		return fmt.Errorf("имя %q зарезервировано Windows — выберите другое", name)
	}
	return nil
}

// isWindowsReservedName reports whether the base name (before the extension)
// is a legacy DOS device name that Windows refuses to use as a folder.
func isWindowsReservedName(name string) bool {
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch base {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

func (m *Manager) ActiveRoot() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == "" {
		return "", fmt.Errorf("no active project")
	}
	return m.active, nil
}

// Scoped returns an independent manager pinned to one project root. The agent
// uses it so a run that started in project A keeps reading and writing A even
// after the user switches to project B while the agent is still working.
// The view owns its own lock and never touches the open-project list.
func (m *Manager) Scoped(root string) *Manager {
	root = strings.TrimSpace(root)
	if root == "" && m != nil {
		root, _ = m.ActiveRoot()
	}
	roots := []string(nil)
	if m != nil {
		roots = m.RootsOf(root)
	}
	return &Manager{active: root, projects: []Project{}, roots: roots}
}

// RootsOf returns the folders merged into the workspace identified by path.
// If path is empty or unknown it returns the active workspace roots.
func (m *Manager) RootsOf(path string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := strings.TrimSpace(path)
	if key == "" {
		key = m.active
	}
	for i := range m.projects {
		if m.projects[i].Path == key {
			if len(m.projects[i].Roots) > 0 {
				return append([]string{}, m.projects[i].Roots...)
			}
			return []string{m.projects[i].Path}
		}
	}
	if key != "" {
		return []string{key}
	}
	return nil
}

// rootEntry pairs a physical root with its virtual name inside the merged tree.
type rootEntry struct {
	root string
	virt string
}

func (m *Manager) rootsFor() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rootsForLocked()
}

func (m *Manager) rootsForLocked() []string {
	if len(m.roots) > 0 {
		return append([]string{}, m.roots...)
	}
	if m.active == "" {
		return nil
	}
	for i := range m.projects {
		if m.projects[i].Path == m.active {
			if len(m.projects[i].Roots) > 0 {
				return append([]string{}, m.projects[i].Roots...)
			}
			return []string{m.projects[i].Path}
		}
	}
	return []string{m.active}
}

// rootEntries computes virtual names for each workspace root. With a single
// root the virtual name is unused (paths stay workspace-relative for backward
// compatibility). With several roots each one becomes a top-level folder named
// after its directory (disambiguated with -2, -3 on name collisions).
func (m *Manager) rootEntries() []rootEntry {
	roots := m.rootsFor()
	if len(roots) == 0 {
		return nil
	}
	// Count collisions so duplicate base names get deterministic suffixes.
	base := make([]string, len(roots))
	seen := map[string]int{}
	for i, r := range roots {
		base[i] = filepath.Base(r)
		seen[strings.ToLower(base[i])]++
	}
	used := map[string]int{}
	out := make([]rootEntry, 0, len(roots))
	for i, r := range roots {
		virt := base[i]
		if seen[strings.ToLower(virt)] > 1 {
			used[strings.ToLower(virt)]++
			virt = fmt.Sprintf("%s-%d", base[i], used[strings.ToLower(virt)])
		}
		out = append(out, rootEntry{root: r, virt: virt})
	}
	return out
}

// multiRoot reports whether the active workspace merges more than one folder.
func (m *Manager) multiRoot() bool {
	return len(m.rootEntries()) > 1
}

// primaryRoot returns the anchor folder of the active workspace (the one the
// workspace is named after and where chats live).
func (m *Manager) primaryRoot() string {
	if m.active != "" {
		return m.active
	}
	root, err := m.ActiveRoot()
	if err != nil {
		return ""
	}
	return root
}

// resolveRoot maps a workspace-relative path to (physical root, remainder).
// rel may start with a root's virtual name; otherwise the first root that
// contains rel wins, and unknown paths default to the primary root.
func (m *Manager) resolveRoot(rel string) (rootEntry, string, error) {
	entries := m.rootEntries()
	empty := rootEntry{}
	if len(entries) == 0 {
		return empty, "", fmt.Errorf("no active project")
	}
	clean := strings.TrimSpace(rel)
	if clean == "" || clean == "." {
		for _, e := range entries {
			if e.root == m.primaryRoot() {
				return e, "", nil
			}
		}
		return entries[0], "", nil
	}
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "/") {
		return empty, "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	clean = filepath.Clean(strings.ReplaceAll(clean, "\\", "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return empty, "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	first, rest := splitFirstComponent(clean)
	if first != "" && m.multiRoot() {
		for _, e := range entries {
			if strings.EqualFold(first, e.virt) {
				return e, rest, nil
			}
		}
	}
	// No virtual root matched: pick a root where rel already exists.
	for _, e := range entries {
		candidate := filepath.Join(e.root, filepath.FromSlash(clean))
		if _, err := os.Stat(candidate); err == nil {
			return e, clean, nil
		}
	}
	// Unknown path: default to the primary root (writes create new files there).
	for _, e := range entries {
		if e.root == m.primaryRoot() {
			return e, clean, nil
		}
	}
	return entries[0], clean, nil
}

func splitFirstComponent(clean string) (string, string) {
	i := strings.IndexAny(clean, "/\\")
	if i < 0 {
		return clean, ""
	}
	return clean[:i], strings.TrimLeft(clean[i:], "/\\")
}

// displayRel renders a root-relative path for the model/UI. Only multi-root
// workspaces get the virtual root prefix, so single-project behaviour is
// unchanged (paths are plain workspace-relative paths).
func (m *Manager) displayRel(e rootEntry, relWithinRoot string) string {
	rel := filepath.ToSlash(relWithinRoot)
	if !m.multiRoot() {
		return rel
	}
	if rel == "" || rel == "." {
		return e.virt
	}
	return filepath.ToSlash(filepath.Join(e.virt, rel))
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
	m.roots = nil
	m.mu.Unlock()
	return nil
}

// AddRoot merges an existing folder into the workspace identified by
// projectPath. The anchor folder (projectPath) always stays first.
func (m *Manager) AddRoot(projectPath, root string) (*Project, error) {
	abs, err := filepath.Abs(root)
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
	projPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i := range m.projects {
		if m.projects[i].Path == projPath {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("workspace not found: %s", projPath)
	}
	p := &m.projects[idx]
	roots := []string{p.Path}
	for _, r := range p.Roots {
		if r != "" && r != p.Path && !stringInSlice(roots, r) {
			roots = append(roots, r)
		}
	}
	if !stringInSlice(roots, abs) {
		roots = append(roots, abs)
	}
	p.Roots = roots
	if m.active == projPath {
		m.roots = nil
	}
	out := *p
	return &out, nil
}

// RemoveRoot detaches a folder from a workspace. The anchor folder cannot be
// removed (chats and the workspace identity live there).
func (m *Manager) RemoveRoot(projectPath, root string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	projPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i := range m.projects {
		if m.projects[i].Path == projPath {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("workspace not found: %s", projPath)
	}
	p := &m.projects[idx]
	if abs == p.Path {
		return nil, fmt.Errorf("нельзя убрать главную папку рабочего пространства")
	}
	roots := p.Roots[:0]
	for _, r := range p.Roots {
		if r != abs {
			roots = append(roots, r)
		}
	}
	if len(roots) == 0 {
		roots = []string{p.Path}
	}
	p.Roots = roots
	if m.active == projPath {
		m.roots = nil
	}
	out := *p
	return &out, nil
}

func stringInSlice(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Close removes a project from the open list.
// Returns the new active project (may be nil if the list is empty).
func (m *Manager) Close(path string) (*Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.projects[:0]
	for _, p := range m.projects {
		if p.Path != abs {
			out = append(out, p)
		}
	}
	m.projects = out
	if m.active == abs {
		if len(m.projects) > 0 {
			m.active = m.projects[0].Path
			p := m.projects[0]
			return &p, nil
		}
		m.active = ""
		return nil, nil
	}
	if m.active == "" {
		return nil, nil
	}
	for i := range m.projects {
		if m.projects[i].Path == m.active {
			p := m.projects[i]
			return &p, nil
		}
	}
	return nil, nil
}

func (m *Manager) Resolve(rel string) (string, error) {
	e, rest, err := m.resolveRoot(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Clean(filepath.Join(e.root, filepath.FromSlash(rest)))
	// Double-check the resolved path never escapes the chosen root.
	relToRoot, err := filepath.Rel(e.root, full)
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
	// Empty path in a multi-root workspace lists the merged top level: one
	// virtual folder per workspace root.
	if strings.TrimSpace(rel) == "" || rel == "." {
		if m.multiRoot() {
			entries := m.rootEntries()
			out := make([]Entry, 0, len(entries))
			for _, e := range entries {
				out = append(out, Entry{Name: e.virt, Path: e.virt, IsDir: true})
			}
			return out, nil
		}
	}
	e, rest, err := m.resolveRoot(rel)
	if err != nil {
		return nil, err
	}
	full := filepath.Clean(filepath.Join(e.root, filepath.FromSlash(rest)))
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
		relChild := name
		if rest != "" && rest != "." {
			relChild = filepath.ToSlash(filepath.Join(rest, name))
		}
		out = append(out, Entry{
			Name:  name,
			Path:  m.displayRel(e, relChild),
			IsDir: it.IsDir(),
		})
	}
	return out, nil
}

const maxReadBytes = 512 * 1024

// errSearchLimit stops the file walk early once enough results are collected.
var errSearchLimit = errors.New("search limit reached")

func (m *Manager) ReadFile(rel string) (string, error) {
	return m.ReadFileRange(rel, 0, 0)
}

// ReadFileRange returns file content with 1-based line numbers (`   12|text`).
// startLine/endLine are 1-based inclusive; 0 means unbounded. A range adds a
// header so the model knows the slice is partial. Line numbers are display-only
// — do not copy them into apply_patch.
func (m *Manager) ReadFileRange(rel string, startLine, endLine int) (string, error) {
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
	text := string(data)
	guarded, err := m.guardRead(rel, text)
	if err != nil {
		return "", err
	}
	if IsSecretPath(rel) {
		return guarded, nil
	}
	lines := splitFileLines(guarded)
	total := len(lines)
	if startLine <= 0 && endLine <= 0 {
		return numberFileLines(lines, 1), nil
	}
	start := startLine
	end := endLine
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > total {
		end = total
	}
	if start > total {
		return "", fmt.Errorf("start_line %d past end of file (%d lines)", startLine, total)
	}
	if end < start {
		return "", fmt.Errorf("end_line %d < start_line %d", endLine, startLine)
	}
	header := fmt.Sprintf("/* lines %d-%d of %d */\n", start, end, total)
	return header + numberFileLines(lines[start-1:end], start), nil
}

func splitFileLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func numberFileLines(lines []string, start int) string {
	if start < 1 {
		start = 1
	}
	if len(lines) == 0 {
		return fmt.Sprintf("%6d|", start)
	}
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%6d|%s", start+i, line)
	}
	return b.String()
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
	msg += "\nUse list_dir, glob, find_files or grep — do not invent paths."
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
	return os.Remove(full)
}

// MovePath renames a file or directory inside the workspace. Fails if dest exists.
func (m *Manager) MovePath(from, to string) error {
	src, err := m.Resolve(from)
	if err != nil {
		return err
	}
	dst, err := m.Resolve(to)
	if err != nil {
		return err
	}
	if src == dst {
		return fmt.Errorf("from and to are the same path")
	}
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("destination exists: %s", to)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
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
