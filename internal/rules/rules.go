package rules

import (
	"os"
	"path/filepath"
	"regexp"
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
//   - project: <project>/.cursor/rules/*.mdc|*.md, legacy <project>/.cursorrules, AGENTS.md
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
			r.AlwaysApply = true
			b.Global = append(b.Global, r)
		}
		sort.Slice(b.Global, func(i, j int) bool { return b.Global[i].Path < b.Global[j].Path })
	}
	if projectRoot != "" {
		b.ProjectDir = filepath.Join(projectRoot, ".cursor", "rules")
		b.Project = append(b.Project, loadFromDir(b.ProjectDir, "project", home, projectRoot)...)
		if r, ok := loadFile(filepath.Join(projectRoot, ".cursorrules"), "project", home, projectRoot); ok {
			r.AlwaysApply = true
			b.Project = append(b.Project, r)
		}
		if r, ok := loadFile(filepath.Join(projectRoot, "AGENTS.md"), "project", home, projectRoot); ok {
			r.AlwaysApply = true
			b.Project = append(b.Project, r)
		}
		sort.Slice(b.Project, func(i, j int) bool { return b.Project[i].Path < b.Project[j].Path })
	}
	return b
}

// CombinedText renders all discovered rules into a compact block (full dump).
// Prefer SelectForPrompt for agent system prompts.
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

// SelectForPrompt builds a Cursor-like prompt slice:
//   - full body for always-on and glob-matched rules
//   - name + description catalog for agent-requested (unmatched) rules
func (b Bundle) SelectForPrompt(hintPaths []string) string {
	var applied []Rule
	var catalog []Rule
	seen := map[string]bool{}
	for _, r := range append(append([]Rule{}, b.Global...), b.Project...) {
		key := r.Path
		if key == "" {
			key = r.Name
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		switch {
		case shouldApplyFull(r, hintPaths):
			applied = append(applied, r)
		case strings.TrimSpace(r.Description) != "":
			catalog = append(catalog, r)
		}
	}
	var parts []string
	if len(applied) > 0 {
		parts = append(parts, "## Applied rules (follow strictly)")
		for _, r := range applied {
			parts = append(parts, formatRuleForLLM(r))
		}
	}
	if len(catalog) > 0 {
		parts = append(parts, "## Available rules (agent-requested; apply when relevant to the task)")
		for _, r := range catalog {
			line := "- " + r.Name
			if r.Description != "" {
				line += " — " + r.Description
			}
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// ExtractHintPaths finds likely file paths in free text and attachment names
// so glob-scoped rules can attach for the current turn.
func ExtractHintPaths(text string, attachmentNames ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = filepath.ToSlash(strings.TrimSpace(p))
		p = strings.Trim(p, "`\"'")
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, n := range attachmentNames {
		add(n)
	}
	for _, m := range pathHintRe.FindAllString(text, -1) {
		add(m)
	}
	for _, m := range backtickPathRe.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	return out
}

var (
	// path-like tokens with an extension (src/foo.go, .\bar\baz.ts)
	pathHintRe = regexp.MustCompile(`(?i)(?:[A-Za-z]:)?(?:[\w.-]+[/\\])+[\w.-]+\.[A-Za-z0-9]{1,12}|[\w.-]+\.[A-Za-z0-9]{1,12}`)
	backtickPathRe = regexp.MustCompile("`([^`\\n]{1,260})`")
)

func shouldApplyFull(r Rule, hintPaths []string) bool {
	if r.AlwaysApply {
		return true
	}
	name := strings.ToLower(r.Name)
	if name == "agents.md" || name == ".cursorrules" {
		return true
	}
	hasDesc := strings.TrimSpace(r.Description) != ""
	hasGlobs := strings.TrimSpace(r.Globs) != ""
	// Bare rule file (no Cursor frontmatter semantics) → always on.
	if !hasDesc && !hasGlobs {
		return true
	}
	if hasGlobs && matchGlobs(r.Globs, hintPaths) {
		return true
	}
	return false
}

func matchGlobs(globs string, paths []string) bool {
	if strings.TrimSpace(globs) == "" || len(paths) == 0 {
		return false
	}
	for _, raw := range strings.Split(globs, ",") {
		pat := strings.TrimSpace(raw)
		if pat == "" {
			continue
		}
		for _, p := range paths {
			if matchOneGlob(pat, p) {
				return true
			}
		}
	}
	return false
}

func matchOneGlob(pattern, path string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	path = filepath.ToSlash(strings.TrimSpace(path))
	if pattern == "" || path == "" {
		return false
	}
	base := filepath.Base(path)
	try := func(pat, candidate string) bool {
		ok, err := filepath.Match(pat, candidate)
		return err == nil && ok
	}
	if try(pattern, path) || try(pattern, base) {
		return true
	}
	// **/foo/*.ts → strip **/ and match against path/base
	if strings.HasPrefix(pattern, "**/") {
		rest := pattern[3:]
		if try(rest, path) || try(rest, base) {
			return true
		}
		// suffix folder match: **/internal/**/*.go roughly → contains segment
		if strings.Contains(rest, "/") {
			// match rest against any suffix of path
			parts := strings.Split(path, "/")
			for i := range parts {
				suffix := strings.Join(parts[i:], "/")
				if try(rest, suffix) {
					return true
				}
			}
		}
	}
	return false
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
	_ = home
	_ = root
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

func formatRuleForLLM(r Rule) string {
	header := "### " + r.Name
	if r.Description != "" {
		header += " — " + r.Description
	}
	return header + "\n" + strings.TrimSpace(r.Content)
}
