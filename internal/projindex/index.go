// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:24:44
// Package projindex builds a deterministic map of the open project: the file
// list, a symbol index (Go AST, regex fallback for other languages) and a
// compact "project brief" that goes into the agent prompt.
//
// Why it exists: without it the agent re-discovers the same layout on every run
// (grep → read_file → grep → …), because a language model keeps nothing between
// runs except what is inside the current prompt. The brief answers the usual
// "where does X live" without touching the transcript, and the tools
// find_symbol / project_search answer it in one step.
package projindex

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// cacheVersion invalidates every persisted index after an extraction change.
	cacheVersion = 1
	// maxFileBytes: bigger files are listed but never parsed (minified bundles, data).
	maxFileBytes = 1 << 20
	// maxFiles guards against pointing the agent at a whole disk.
	maxFiles = 20000
	// maxSymsPerFile keeps a generated file from flooding the index.
	maxSymsPerFile = 400
	// maxParseFiles caps first-build work on huge repos (cache hits do not count).
	maxParseFiles = 5000
	// docLimit trims a doc comment to one readable line.
	docLimit = 160
)

// Symbol is one named declaration found in a project file.
type Symbol struct {
	Name string `json:"n"`
	Kind string `json:"k"`           // func | method | type | struct | interface | class | const | var | def | function
	Recv string `json:"r,omitempty"` // method receiver type, e.g. *Manager
	Line int    `json:"l"`
	Doc  string `json:"d,omitempty"` // first doc/comment line, trimmed
}

// File is one indexed file. Syms is nil for files that are not parsed.
type File struct {
	Path  string   `json:"p"`           // slash-separated, relative to the root
	Lang  string   `json:"g,omitempty"` // go | ts | py | cs | ...
	Pkg   string   `json:"k,omitempty"` // Go package name
	Lines int      `json:"ln,omitempty"`
	Bytes int64    `json:"b,omitempty"`
	ModNS int64    `json:"m,omitempty"` // mtime in ns — cheap change detection
	Entry bool     `json:"e,omitempty"` // package main with func main
	Syms  []Symbol `json:"s,omitempty"`
}

// Index is the whole project map. Root is absolute, every path inside is relative.
type Index struct {
	Root      string `json:"root"`
	Module    string `json:"mod,omitempty"` // go.mod module path
	FP        string `json:"fp"`            // fingerprint of paths+sizes+mtimes
	Built     int64  `json:"at"`
	Truncated bool   `json:"tr,omitempty"`
	Files     []File `json:"files"`
}

// SymbolCount is the number of indexed declarations.
func (idx *Index) SymbolCount() int {
	if idx == nil {
		return 0
	}
	n := 0
	for _, f := range idx.Files {
		n += len(f.Syms)
	}
	return n
}

// TotalLines is the summed line count of the indexed files.
func (idx *Index) TotalLines() int {
	if idx == nil {
		return 0
	}
	n := 0
	for _, f := range idx.Files {
		n += f.Lines
	}
	return n
}

// scanned is the stat-only pass: file list plus a change fingerprint.
type scanned struct {
	files     []File
	fp        string
	truncated bool
}

// scan walks root and records every project file with its size and mtime.
// No file content is read, so this stays cheap enough to run on each turn.
func scan(root string) (scanned, error) {
	h := sha1.New()
	fmt.Fprintf(h, "v%d\n", cacheVersion)
	out := scanned{files: make([]File, 0, 512)}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil // unreadable entry: keep walking
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if skipDir(name) {
				return fs.SkipDir
			}
			return nil
		}
		if skipFile(name) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		fmt.Fprintf(h, "%s|%d|%d\n", rel, info.Size(), info.ModTime().UnixNano())
		if len(out.files) >= maxFiles {
			out.truncated = true
			return nil
		}
		out.files = append(out.files, File{
			Path:  rel,
			Lang:  langOf(name),
			Bytes: info.Size(),
			ModNS: info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil {
		return out, err
	}
	out.fp = hex.EncodeToString(h.Sum(nil))
	return out, nil
}

// Build indexes root from scratch (no reuse of a previous build).
func Build(root string) (*Index, error) {
	sc, err := scan(root)
	if err != nil {
		return nil, err
	}
	return build(root, sc, nil), nil
}

// build turns a scan into an index, reusing symbols of files that did not
// change (same size + mtime) so an edit costs one file, not the whole repo.
func build(root string, sc scanned, prev *Index) *Index {
	idx := &Index{
		Root:      root,
		FP:        sc.fp,
		Built:     time.Now().Unix(),
		Truncated: sc.truncated,
		Files:     sc.files,
	}
	idx.Module = readModule(root)

	var cached map[string]File
	if prev != nil && prev.Root == root {
		cached = make(map[string]File, len(prev.Files))
		for _, f := range prev.Files {
			cached[f.Path] = f
		}
	}
	parsed := 0
	for i := range idx.Files {
		f := &idx.Files[i]
		if c, ok := cached[f.Path]; ok && c.Bytes == f.Bytes && c.ModNS == f.ModNS && c.Lang == f.Lang {
			f.Lines, f.Pkg, f.Entry, f.Syms = c.Lines, c.Pkg, c.Entry, c.Syms
			continue
		}
		if f.Lang == "" || f.Bytes > maxFileBytes || parsed >= maxParseFiles {
			continue
		}
		parsed++
		src, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
		if rerr != nil {
			continue
		}
		f.Lines = countLines(src)
		if f.Lang == "go" {
			f.Pkg, f.Syms, f.Entry = extractGo(f.Path, src)
			continue
		}
		f.Syms = extractRegex(f.Lang, src)
	}
	return idx
}

// readModule returns the module path from <root>/go.mod (empty when absent).
func readModule(root string) string {
	src, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func countLines(src []byte) int {
	if len(src) == 0 {
		return 0
	}
	n := bytes.Count(src, []byte("\n"))
	if src[len(src)-1] != '\n' {
		n++
	}
	return n
}

// skipDir reports directories that never hold project knowledge.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "dist", "build", "out", "target", "obj", "bin",
		"coverage", "__pycache__", "venv", "tmp", "temp", "logs", "log":
		return true
	}
	return false
}

// skipFile reports binaries and archives that cannot carry symbols.
func skipFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".exe", ".dll", ".so", ".dylib", ".a", ".o", ".obj", ".lib", ".pyc", ".class", ".jar",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".icns", ".bmp", ".tiff",
		".wav", ".mp3", ".ogg", ".mp4", ".mov", ".webm",
		".zip", ".gz", ".tgz", ".7z", ".rar", ".pdf", ".woff", ".woff2", ".ttf", ".otf",
		".db", ".sqlite", ".zst", ".bin", ".dat", ".wasm", ".msi", ".dmg", ".nupkg":
		return true
	}
	return false
}
