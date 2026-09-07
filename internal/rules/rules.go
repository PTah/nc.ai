package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxRuleBytes = 512 * 1024

// Rule is a single Cursor rule file loaded from disk.
type Rule struct {
	Source      string `json:"source"`      // "global" | "project"
	Path        string `json:"path"`        // absolute path (slashes)
	Name        string `json:"name"`        // base filename
	Description string `json:"description"` // frontmatter description
	AlwaysApply bool   `json:"alwaysApply"` // frontmatter alwaysApply
	Globs       string `json:"globs"`       // frontmatter globs
	Content     string `json:"content"`     // rule body without frontmatter
}

// Bundle contains all Cursor rules discovered for the current context.
type Bundle struct {
	GlobalDir  string `json:"globalDir"`
	ProjectDir string `json:"projectDir"`
	Global     []Rule `json:"global"`
	Project    []Rule `json:"project"`
}

// Load discovers Cursor rules:
//   - global: ~/.cursor/rules/*.mdc|*.md and legacy ~/.cursorrules
//   - project: <project>/.cursor/rules/*.mdc|*.md and legacy <project>/.cursorrules
func Load(projectRoot string) Bundle {
	b := Bundle{
		Global:  []Rule{},
		Project: []Rule{},
	}
	home, err := os.UserHomeDir()
	if err == nil {
		b.GlobalDir = filepath.Join(home, ".cursor", "rules")
		b.Global = append(b.Global, loadFromDir(b.GlobalDir, "global", home, "")...)
		if r, ok := loadFile(filepath.Join(home, ".cursorrules"), "global", home, ""); ok {
			b.Global = append(b.Global, r)
		}
		sort.Slice(b.Global, func(i, j int) bool { return b.Global[i].Path < b.Global[j].Path })
	}
	if projectRoot != "" {
		b.ProjectDir = filepath.Join(projectRoot, ".cursor", "rules")
		b.Project = append(b.Project, loadFromDir(b.ProjectDir, "project", home, projectRoot)...)
		if r, ok := loadFile(filepath.Join(projectRoot, ".cursorrules"), "project", home, projectRoot); ok {
			b.Project = append(b.Project, r)
		}
		sort.Slice(b.Project, func(i, j int) bool { return b.Project[i].Path < b.Project[j].Path })
	}
	return b
}

// CombinedText renders all discovered rules into a compact block for the
// agent system prompt. Empty when no rules were found.
func (b Bundle) CombinedText() string {
	var parts []string
	if len(b.Global) > 0 {
		parts = append(parts, "## Global Cursor rules")
		for _, r := range b.Global {
			parts = append(parts, formatRule(r))
		}
	}
	if len(b.Project) > 0 {
		parts = append(parts, "## Project Cursor rules")
		for _, r := range b.Project {
			parts = append(parts, formatRule(r))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

func loadFromDir(dir, source, home, root string) []Rule {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Rule{}
	}
	out := make([]Rule, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".mdc" && ext != ".md" {
			continue
		}
		if r, ok := loadFile(filepath.Join(dir, e.Name()), source, home, root); ok {
			out = append(out, r)
		}
	}
	return out
}

func loadFile(path, source, home, root string) (Rule, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Rule{}, false
	}
	if len(data) > maxRuleBytes {
		data = data[:maxRuleBytes]
	}
	content, meta := parseFrontmatter(string(data))
	r := Rule{
		Source:      source,
		Path:        filepath.ToSlash(path),
		Name:        filepath.Base(path),
		Description: meta["description"],
		AlwaysApply: strings.EqualFold(meta["alwaysapply"], "true"),
		Globs:       meta["globs"],
		Content:     content,
	}
	return r, true
}

// parseFrontmatter strips a leading YAML-ish block:
//
//	---
//	description: ...
//	alwaysApply: true
//	---
//	body
func parseFrontmatter(s string) (body string, meta map[string]string) {
	meta = map[string]string{}
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "---") {
		return s, meta
	}
	rest := trimmed[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return s, meta
	}
	fm := rest[:idx]
	body = strings.TrimSpace(rest[idx+len("\n---"):])
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		meta[k] = v
	}
	return body, meta
}

func formatRule(r Rule) string {
	header := "### " + r.Path
	if r.Description != "" {
		header += " — " + r.Description
	}
	if r.AlwaysApply {
		header += " (alwaysApply)"
	}
	if r.Globs != "" {
		header += "\nApplies to files: " + r.Globs
	}
	return header + "\n" + strings.TrimSpace(r.Content)
}
