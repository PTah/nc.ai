// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:25:24
package projindex

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Hit is one search result: a symbol, or (for path matches) a whole file.
type Hit struct {
	File  string
	Line  int
	Kind  string // func | method | struct | class | def | file
	Name  string
	Recv  string
	Doc   string
	Lines int // file size, file hits only
	Syms  int // symbol count, file hits only
}

// FindSymbol searches declaration names: case-insensitive substring, or a
// regex when the pattern contains regex metacharacters. Regex is tried when the
// substring pass finds nothing, so a plain word never fails on accents.
func (idx *Index) FindSymbol(pattern string, limit int) []Hit {
	pattern = strings.TrimSpace(pattern)
	if idx == nil || pattern == "" {
		return nil
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 200 {
		limit = 200
	}
	lower := strings.ToLower(pattern)
	var re *regexp.Regexp
	if !plainPattern(lower) {
		if c, err := regexp.Compile("(?i)" + pattern); err == nil {
			re = c
		}
	}

	out := make([]Hit, 0, 32)
	for _, f := range idx.Files {
		for _, s := range f.Syms {
			name := strings.ToLower(s.Name)
			ok := strings.Contains(name, lower)
			if !ok && re != nil {
				ok = re.MatchString(s.Name)
			}
			if !ok {
				continue
			}
			out = append(out, Hit{
				File: f.Path, Line: s.Line, Kind: s.Kind,
				Name: s.Name, Recv: s.Recv, Doc: s.Doc,
			})
		}
	}
	sortHits(out, lower)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Search ranks symbols and file paths against a free-form query.
// Tokens are AND-free: every token adds score, so "online offline events" still
// surfaces a symbol whose name carries only one of the words but whose doc has
// the rest.
func (idx *Index) Search(query string, limit int) []Hit {
	if idx == nil {
		return nil
	}
	tokens := tokens(query)
	if len(tokens) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 25
	}

	type scored struct {
		hit Hit
		sc  int
	}
	symHits := make([]scored, 0, 64)
	for _, f := range idx.Files {
		fileLower := strings.ToLower(f.Path)
		for _, s := range f.Syms {
			name := strings.ToLower(s.Name)
			doc := strings.ToLower(s.Doc)
			recv := strings.ToLower(s.Recv)
			sc := 0
			for _, t := range tokens {
				switch {
				case name == t:
					sc += 80
				case strings.Contains(name, t):
					sc += 40
				}
				if strings.Contains(doc, t) {
					sc += 12
				}
				if strings.Contains(recv, t) {
					sc += 10
				}
				if strings.Contains(fileLower, t) {
					sc += 10
				}
			}
			if sc == 0 {
				continue
			}
			// A one-line helper is a worse answer than a documented type.
			if len(s.Doc) > 0 {
				sc += 4
			}
			symHits = append(symHits, scored{Hit{
				File: f.Path, Line: s.Line, Kind: s.Kind,
				Name: s.Name, Recv: s.Recv, Doc: s.Doc,
			}, sc})
		}
	}
	sort.SliceStable(symHits, func(i, j int) bool {
		if symHits[i].sc != symHits[j].sc {
			return symHits[i].sc > symHits[j].sc
		}
		if symHits[i].hit.File != symHits[j].hit.File {
			return symHits[i].hit.File < symHits[j].hit.File
		}
		return symHits[i].hit.Line < symHits[j].hit.Line
	})

	fileHits := make([]scored, 0, 32)
	for _, f := range idx.Files {
		fileLower := strings.ToLower(f.Path)
		base := strings.ToLower(path.Base(f.Path))
		sc := 0
		for _, t := range tokens {
			if base == t {
				sc += 60
			}
			if strings.Contains(base, t) {
				sc += 30
			}
			if strings.Contains(fileLower, t) {
				sc += 18
			}
		}
		if sc == 0 || len(f.Syms) == 0 {
			continue
		}
		fileHits = append(fileHits, scored{Hit{
			File: f.Path, Kind: "file", Name: path.Base(f.Path),
			Lines: f.Lines, Syms: len(f.Syms),
		}, sc})
	}
	sort.SliceStable(fileHits, func(i, j int) bool {
		if fileHits[i].sc != fileHits[j].sc {
			return fileHits[i].sc > fileHits[j].sc
		}
		return fileHits[i].hit.File < fileHits[j].hit.File
	})

	out := make([]Hit, 0, limit)
	for _, s := range symHits {
		if len(out) >= limit {
			return out
		}
		out = append(out, s.hit)
	}
	for _, s := range fileHits {
		if len(out) >= limit {
			return out
		}
		out = append(out, s.hit)
	}
	return out
}

// FormatHits renders hits as compact one-line entries.
func FormatHits(hits []Hit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	for _, h := range hits {
		if h.Kind == "file" {
			b.WriteString("file " + h.File)
			if h.Lines > 0 {
				b.WriteString(" · " + strconv.Itoa(h.Lines) + "L")
			}
			if h.Syms > 0 {
				b.WriteString(" · " + strconv.Itoa(h.Syms) + " symbols")
			}
			b.WriteString("\n")
			continue
		}
		b.WriteString(h.Kind + " ")
		if h.Recv != "" {
			b.WriteString("(" + h.Recv + ") ")
		}
		b.WriteString(h.Name + " — " + h.File + ":" + strconv.Itoa(h.Line))
		if h.Doc != "" {
			b.WriteString(" · " + h.Doc)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// sortHits orders matches: exact name, prefix, substring, then shorter name.
func sortHits(hits []Hit, lower string) {
	sort.SliceStable(hits, func(i, j int) bool {
		ri, rj := hitRank(hits[i].Name, lower), hitRank(hits[j].Name, lower)
		if ri != rj {
			return ri < rj
		}
		if hits[i].Name != hits[j].Name {
			return len(hits[i].Name) < len(hits[j].Name)
		}
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		return hits[i].Line < hits[j].Line
	})
}

func hitRank(name, lower string) int {
	n := strings.ToLower(name)
	switch {
	case n == lower:
		return 0
	case strings.HasPrefix(n, lower):
		return 1
	default:
		return 2
	}
}

// plainPattern reports whether a query has no regex metacharacters.
func plainPattern(q string) bool {
	return !strings.ContainsAny(q, `\.+*?()[]{}|^$`)
}

// tokens splits a query into lowercase word tokens.
func tokens(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r >= 'а' && r <= 'я' || r == 'ё' || r >= 'A' && r <= 'Z' ||
			r == '_' || r == '-' || r == '.' || r >= 0x400 && r <= 0x4ff)
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
