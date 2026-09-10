package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// FindFiles returns relative paths whose names/paths fuzzy-match query.
// Prefer this over Grep when looking for a file by name.
func (m *Manager) FindFiles(query string, limit int) ([]string, error) {
	root, err := m.ActiveRoot()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 40
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, fmt.Errorf("query is empty")
	}

	type scored struct {
		path  string
		score int
	}
	var ranked []scored
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
		sc := fuzzyPathScore(q, rel, strings.ToLower(name))
		if sc < 0 {
			return nil
		}
		ranked = append(ranked, scored{path: rel, score: sc})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].path < ranked[j].path
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]string, len(ranked))
	for i, s := range ranked {
		out[i] = s.path
	}
	return out, nil
}

// GrepOptions controls content search (ripgrep-like).
type GrepOptions struct {
	Query         string
	PathGlob      string // optional filepath-style glob against relative path
	Context       int    // lines before/after match (like -C)
	Before        int    // -B; overrides Context for before when > 0
	After         int    // -A
	CaseSensitive bool
	Multiline     bool
	OutputMode    string // content | files_with_matches | count
	Limit         int    // max match lines (not files)
}

// GrepHit is one content match with optional context.
type GrepHit struct {
	Path    string
	Line    int // 1-based
	Content string
	Before  []string
	After   []string
}

// Grep scans text files for a regex (ripgrep-like). Invalid patterns return an error.
func (m *Manager) Grep(opts GrepOptions) ([]GrepHit, error) {
	root, err := m.ActiveRoot()
	if err != nil {
		return nil, err
	}
	q := opts.Query
	if strings.TrimSpace(q) == "" {
		return nil, fmt.Errorf("query is empty")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	before, after := grepContext(opts)
	flags := ""
	if !opts.CaseSensitive {
		flags += "i"
	}
	if opts.Multiline {
		flags += "s"
	}
	pattern := q
	if flags != "" {
		pattern = "(?" + flags + ")" + q
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex %q: %w (escape special chars, e.g. foo\\.bar\\()", q, err)
	}

	var hits []GrepHit
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(hits) >= limit {
			if len(hits) >= limit {
				return errSearchLimit
			}
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
		if opts.PathGlob != "" && !matchPathGlob(opts.PathGlob, rel) {
			return nil
		}
		info, e := d.Info()
		if e != nil || info.Size() > 512*1024 {
			return nil
		}
		if !looksTextish(name) {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil || isBinary(data) {
			return nil
		}
		text := string(data)
		lines := strings.Split(text, "\n")
		if opts.Multiline {
			if !re.MatchString(text) {
				return nil
			}
			// Report the first matching line for orientation.
			idx := re.FindStringIndex(text)
			lineNo := 1
			if idx != nil {
				lineNo = strings.Count(text[:idx[0]], "\n") + 1
			}
			hits = append(hits, GrepHit{Path: rel, Line: lineNo, Content: trimRightSpace(lineAt(lines, lineNo))})
			return nil
		}
		for i, line := range lines {
			if len(hits) >= limit {
				return errSearchLimit
			}
			if !re.MatchString(line) {
				continue
			}
			h := GrepHit{Path: rel, Line: i + 1, Content: trimRightSpace(line)}
			if before > 0 {
				start := i - before
				if start < 0 {
					start = 0
				}
				for _, l := range lines[start:i] {
					h.Before = append(h.Before, trimRightSpace(l))
				}
			}
			if after > 0 {
				end := i + 1 + after
				if end > len(lines) {
					end = len(lines)
				}
				for _, l := range lines[i+1 : end] {
					h.After = append(h.After, trimRightSpace(l))
				}
			}
			hits = append(hits, h)
		}
		return nil
	})
	if err != nil && err != errSearchLimit {
		return hits, err
	}
	return hits, nil
}

func grepContext(opts GrepOptions) (before, after int) {
	before, after = opts.Before, opts.After
	if before <= 0 && after <= 0 {
		before, after = opts.Context, opts.Context
	}
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	if before > 10 {
		before = 10
	}
	if after > 10 {
		after = 10
	}
	return before, after
}

func lineAt(lines []string, lineNo int) string {
	if lineNo < 1 || lineNo > len(lines) {
		return ""
	}
	return lines[lineNo-1]
}

// FormatGrepHits renders ripgrep-like text for the model.
func FormatGrepHits(hits []GrepHit) string {
	return FormatGrepHitsMode(hits, "content")
}

// FormatGrepHitsMode renders content, files_with_matches, or count.
func FormatGrepHitsMode(hits []GrepHit, mode string) string {
	if len(hits) == 0 {
		return "(no matches)"
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "files_with_matches", "files":
		seen := map[string]bool{}
		var b strings.Builder
		for _, h := range hits {
			if seen[h.Path] {
				continue
			}
			seen[h.Path] = true
			b.WriteString(h.Path)
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n")
	case "count":
		counts := map[string]int{}
		var order []string
		for _, h := range hits {
			if _, ok := counts[h.Path]; !ok {
				order = append(order, h.Path)
			}
			counts[h.Path]++
		}
		var b strings.Builder
		for _, p := range order {
			fmt.Fprintf(&b, "%s:%d\n", p, counts[p])
		}
		return strings.TrimRight(b.String(), "\n")
	default:
		var b strings.Builder
		for i, h := range hits {
			if i > 0 {
				b.WriteString("\n")
			}
			base := h.Line - len(h.Before)
			for j, l := range h.Before {
				fmt.Fprintf(&b, "%s:%d-%s\n", h.Path, base+j, l)
			}
			fmt.Fprintf(&b, "%s:%d:%s\n", h.Path, h.Line, h.Content)
			for j, l := range h.After {
				fmt.Fprintf(&b, "%s:%d+%s\n", h.Path, h.Line+1+j, l)
			}
		}
		return strings.TrimRight(b.String(), "\n")
	}
}

func fuzzyPathScore(query, rel, base string) int {
	if strings.Contains(rel, query) || strings.Contains(base, query) {
		score := 100
		if base == query {
			score += 50
		} else if strings.HasPrefix(base, query) {
			score += 30
		} else if strings.Contains(base, query) {
			score += 20
		}
		// Prefer shorter paths.
		score -= len(rel) / 10
		return score
	}
	if fuzzySubseq(query, base) || fuzzySubseq(query, rel) {
		return 40 - len(rel)/20
	}
	return -1
}

func fuzzySubseq(query, s string) bool {
	qi := 0
	for i := 0; i < len(s) && qi < len(query); i++ {
		if s[i] == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}

func matchPathGlob(pattern, rel string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	rel = filepath.ToSlash(rel)
	if pattern == "" {
		return true
	}
	if ok, _ := filepath.Match(pattern, rel); ok {
		return true
	}
	if ok, _ := filepath.Match(pattern, filepath.Base(rel)); ok {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		rest := pattern[3:]
		if ok, _ := filepath.Match(rest, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(rest, filepath.Base(rel)); ok {
			return true
		}
		parts := strings.Split(rel, "/")
		for i := range parts {
			suffix := strings.Join(parts[i:], "/")
			if ok, _ := filepath.Match(rest, suffix); ok {
				return true
			}
		}
	}
	// Simple contains for patterns like internal/**/*.go without full doublestar.
	if strings.Contains(pattern, "**") {
		flat := strings.ReplaceAll(pattern, "**/", "")
		flat = strings.ReplaceAll(flat, "**", "*")
		if ok, _ := filepath.Match(flat, filepath.Base(rel)); ok {
			return true
		}
		if strings.HasSuffix(flat, filepath.Ext(rel)) && strings.Contains(rel, strings.TrimSuffix(strings.Split(pattern, "**/")[0], "/")) {
			ext := filepath.Ext(flat)
			if ext != "" && strings.HasSuffix(rel, ext) {
				prefix := strings.Split(pattern, "**")[0]
				prefix = strings.Trim(prefix, "/")
				if prefix == "" || strings.Contains(rel, prefix) {
					return true
				}
			}
		}
	}
	return false
}

func looksTextish(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip", ".gz", ".exe", ".dll", ".so", ".dylib", ".bin", ".wasm", ".mp3", ".mp4", ".woff", ".woff2", ".ttf", ".eot":
		return false
	case "":
		base := strings.ToLower(name)
		return base == "makefile" || base == "dockerfile" || base == "license" || base == "readme" || strings.HasPrefix(base, "dockerfile")
	default:
		return true
	}
}

func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

func trimRightSpace(s string) string {
	return strings.TrimRightFunc(s, unicode.IsSpace)
}
