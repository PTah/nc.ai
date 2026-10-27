// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:25:11
package projindex

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Brief renders the compact project brief that goes into the agent prompt.
// It answers "what is this project and where does the code live" in a few
// hundred bytes, so the model does not have to spend tool calls rediscovering
// the layout. maxBytes caps the result (it is paid on every request).
func Brief(idx *Index, maxBytes int) string {
	if idx == nil || len(idx.Files) == 0 {
		return ""
	}
	if maxBytes <= 0 {
		maxBytes = 1600
	}
	lines := make([]string, 0, 10)

	// 1. Root summary.
	kind := "no go.mod"
	if idx.Module != "" {
		kind = "go " + idx.Module
	}
	lines = append(lines, fmt.Sprintf("root: %s · %s · %d files · %s lines · %d symbols",
		path.Base(strings.ReplaceAll(idx.Root, "\\", "/")), kind,
		len(idx.Files), humanLines(idx.TotalLines()), idx.SymbolCount()))

	// 2. Go packages, biggest first.
	if pkgs := goPackages(idx); len(pkgs) > 0 {
		var parts []string
		for i, p := range pkgs {
			if i == 8 {
				parts = append(parts, fmt.Sprintf("+%d more", len(pkgs)-8))
				break
			}
			if p.pkg != "" {
				parts = append(parts, fmt.Sprintf("%s(%s,%df)", p.dir, p.pkg, p.files))
			} else {
				parts = append(parts, fmt.Sprintf("%s(%df)", p.dir, p.files))
			}
		}
		lines = append(lines, fmt.Sprintf("go: %d packages · %s", len(pkgs), strings.Join(parts, ", ")))
	}

	// 3. Entry points.
	if ent := entryPoints(idx); len(ent) > 0 {
		lines = append(lines, "entry: "+strings.Join(ent, ", "))
	}

	// 4. Largest files — «where the meat is».
	if big := biggestFiles(idx, 6); len(big) > 0 {
		lines = append(lines, "big: "+strings.Join(big, ", "))
	}

	// 5. Non-Go code and docs.
	if other := langSummary(idx); other != "" {
		lines = append(lines, other)
	}
	if docs := docsSummary(idx); docs != "" {
		lines = append(lines, docs)
	}
	if idx.Truncated {
		lines = append(lines, fmt.Sprintf("note: index truncated at %d files (huge repo)", maxFiles))
	}
	lines = append(lines, "index: find_symbol(name) / project_search(query) search these symbols — one call instead of grep→read_file loops")

	out := strings.Join(lines, "\n")
	if len(out) <= maxBytes {
		return out
	}
	// Cut on a line boundary: a half-line confuses more than it helps.
	cut := strings.LastIndexByte(out[:maxBytes], '\n')
	if cut <= 0 {
		return out[:maxBytes]
	}
	return out[:cut] + "\n…(brief truncated)"
}

type pkgInfo struct {
	dir   string
	pkg   string
	files int
	lines int
}

// goPackages groups Go files by directory, largest first.
func goPackages(idx *Index) []pkgInfo {
	byDir := map[string]*pkgInfo{}
	for _, f := range idx.Files {
		if f.Lang != "go" {
			continue
		}
		dir := path.Dir(f.Path)
		if dir == "." {
			dir = "root"
		}
		p := byDir[dir]
		if p == nil {
			p = &pkgInfo{dir: dir, pkg: f.Pkg}
			byDir[dir] = p
		}
		p.files++
		p.lines += f.Lines
	}
	out := make([]pkgInfo, 0, len(byDir))
	for _, p := range byDir {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].lines != out[j].lines {
			return out[i].lines > out[j].lines
		}
		return out[i].dir < out[j].dir
	})
	return out
}

// entryPoints lists binaries: package main with func main, plus cmd/*/main.*.
func entryPoints(idx *Index) []string {
	var out []string
	for _, f := range idx.Files {
		if f.Entry {
			out = append(out, f.Path)
			continue
		}
		base := path.Base(f.Path)
		if strings.HasPrefix(f.Path, "cmd/") && (base == "main.go" || base == "main.py" || base == "main.rs") {
			out = append(out, f.Path)
		}
	}
	sort.Strings(out)
	if len(out) > 6 {
		out = append(out[:6], "…")
	}
	return out
}

// biggestFiles lists the longest indexed files (minified bundles excluded by size cap).
func biggestFiles(idx *Index, limit int) []string {
	files := make([]File, 0, len(idx.Files))
	for _, f := range idx.Files {
		if f.Lines > 40 {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Lines != files[j].Lines {
			return files[i].Lines > files[j].Lines
		}
		return files[i].Path < files[j].Path
	})
	out := make([]string, 0, limit)
	for i, f := range files {
		if i == limit {
			break
		}
		out = append(out, fmt.Sprintf("%s %dL", f.Path, f.Lines))
	}
	return out
}

// langSummary counts non-Go languages by top-level directory.
func langSummary(idx *Index) string {
	counts := map[string]int{}
	dirs := map[string]int{}
	for _, f := range idx.Files {
		if f.Lang == "" || f.Lang == "go" {
			continue
		}
		counts[f.Lang]++
		dirs[path.Dir(f.Path)]++
	}
	if len(counts) == 0 {
		return ""
	}
	langs := make([]string, 0, len(counts))
	for l, n := range counts {
		langs = append(langs, l+"×"+strconv.Itoa(n))
	}
	sort.Slice(langs, func(i, j int) bool { return langLess(langs[i], langs[j]) })

	type dirCount struct {
		dir string
		n   int
	}
	ds := make([]dirCount, 0, len(dirs))
	for d, n := range dirs {
		ds = append(ds, dirCount{d, n})
	}
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].n != ds[j].n {
			return ds[i].n > ds[j].n
		}
		return ds[i].dir < ds[j].dir
	})
	top := make([]string, 0, 4)
	for i, d := range ds {
		if i == 4 {
			break
		}
		top = append(top, fmt.Sprintf("%s %d", d.dir, d.n))
	}
	return "code: " + strings.Join(top, ", ") + " · " + strings.Join(langs, " ")
}

// langLess orders "ts×21" style counters by count desc then language.
func langLess(a, b string) bool {
	na, la := splitCount(a)
	nb, lb := splitCount(b)
	if na != nb {
		return na > nb
	}
	return la < lb
}

func splitCount(s string) (int, string) {
	i := strings.LastIndex(s, "×")
	if i < 0 {
		return 0, s
	}
	n, _ := strconv.Atoi(s[i+len("×"):])
	return n, s[:i]
}

// docsSummary counts markdown files, useful for docs-heavy projects.
func docsSummary(idx *Index) string {
	total := 0
	topDirs := map[string]int{}
	topFiles := []string{}
	for _, f := range idx.Files {
		if !strings.HasSuffix(strings.ToLower(f.Path), ".md") {
			continue
		}
		total++
		d := topDir(f.Path)
		if d == "." {
			if len(topFiles) < 5 {
				topFiles = append(topFiles, f.Path)
			}
			continue
		}
		topDirs[d]++
	}
	if total == 0 {
		return ""
	}
	parts := make([]string, 0, 4)
	type dirCount struct {
		dir string
		n   int
	}
	ds := make([]dirCount, 0, len(topDirs))
	for d, n := range topDirs {
		ds = append(ds, dirCount{d, n})
	}
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].n != ds[j].n {
			return ds[i].n > ds[j].n
		}
		return ds[i].dir < ds[j].dir
	})
	for i, d := range ds {
		if i == 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %d", d.dir, d.n))
	}
	if len(topFiles) > 0 {
		parts = append(parts, strings.Join(topFiles, " "))
	}
	return fmt.Sprintf("docs: %d md · %s", total, strings.Join(parts, ", "))
}

func topDir(p string) string {
	if i := strings.IndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return "."
}

func humanLines(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}
