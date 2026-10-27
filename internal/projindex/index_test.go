// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:25:38
package projindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testGoSrc = `// Package demo is a fixture.
package demo

// Widget is a demo type.
type Widget struct {
	Name string
}

// Do runs the widget.
func (w *Widget) Do() {}

// New makes a widget.
func New() *Widget { return &Widget{} }

const Limit = 10
`

const testTSSrc = `// Renders the widget.
export function renderWidget() {}

export class WidgetPanel {}

const helper = () => 1
`

// writeTree creates a small mixed-language project under a temp root.
func writeTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":            "module example.com/demo\n\ngo 1.22\n",
		"demo.go":           testGoSrc,
		"cmd/app/main.go":   "package main\n\nfunc main() {}\n",
		"web/src/panel.ts":  testTSSrc,
		"README.md":         "# demo\n",
		"node_modules/x.js": "export function ignored() {}\n",
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func TestBuildIndexesGoAndTS(t *testing.T) {
	root := writeTree(t)
	idx, err := Build(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if idx.Module != "example.com/demo" {
		t.Fatalf("module = %q", idx.Module)
	}
	if idx.FP == "" {
		t.Fatal("empty fingerprint")
	}

	goFile := idx.FileByPathForTest("demo.go")
	if goFile == nil {
		t.Fatal("demo.go not indexed")
	}
	if goFile.Pkg != "demo" {
		t.Fatalf("pkg = %q", goFile.Pkg)
	}
	if !hasSym(goFile.Syms, "Widget", "struct") {
		t.Fatalf("Widget struct missing: %+v", goFile.Syms)
	}
	if !hasSym(goFile.Syms, "Do", "method") {
		t.Fatalf("Do method missing: %+v", goFile.Syms)
	}
	if !hasSym(goFile.Syms, "New", "func") {
		t.Fatalf("New func missing: %+v", goFile.Syms)
	}
	if !hasSym(goFile.Syms, "Limit", "const") {
		t.Fatalf("Limit const missing: %+v", goFile.Syms)
	}
	for _, s := range goFile.Syms {
		if s.Name == "Widget" && !strings.Contains(s.Doc, "demo type") {
			t.Fatalf("Widget doc = %q", s.Doc)
		}
		if s.Name == "Do" && s.Recv != "*Widget" {
			t.Fatalf("Do recv = %q", s.Recv)
		}
	}

	ts := idx.FileByPathForTest("web/src/panel.ts")
	if ts == nil {
		t.Fatal("panel.ts not indexed")
	}
	if !hasSym(ts.Syms, "renderWidget", "func") {
		t.Fatalf("renderWidget missing: %+v", ts.Syms)
	}
	if !hasSym(ts.Syms, "WidgetPanel", "type") {
		t.Fatalf("WidgetPanel missing: %+v", ts.Syms)
	}
	if !hasSym(ts.Syms, "helper", "func") {
		t.Fatalf("helper missing: %+v", ts.Syms)
	}
	for _, s := range ts.Syms {
		if s.Name == "renderWidget" && !strings.Contains(s.Doc, "Renders the widget") {
			t.Fatalf("renderWidget doc = %q", s.Doc)
		}
	}

	main := idx.FileByPathForTest("cmd/app/main.go")
	if main == nil || !main.Entry {
		t.Fatal("cmd/app/main.go not marked as entry point")
	}
	if idx.FileByPathForTest("node_modules/x.js") != nil {
		t.Fatal("node_modules must be skipped")
	}
}

func TestBriefMentionsLayout(t *testing.T) {
	root := writeTree(t)
	idx, err := Build(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	brief := Brief(idx, 4000)
	for _, want := range []string{"go example.com/demo", "root(demo,1f)", "cmd/app/main.go", "web/src 1", "index: find_symbol"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief misses %q:\n%s", want, brief)
		}
	}
	small := Brief(idx, 120)
	if len(small) > 200 {
		t.Fatalf("brief not capped: %d bytes", len(small))
	}
	if Brief(nil, 500) != "" {
		t.Fatal("nil index must render empty brief")
	}
}

func TestFindSymbolAndSearch(t *testing.T) {
	root := writeTree(t)
	idx, err := Build(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	hits := idx.FindSymbol("widget", 10)
	if len(hits) < 3 {
		t.Fatalf("expected Widget/Do/WidgetPanel, got %+v", hits)
	}
	if hits[0].Name != "Widget" {
		t.Fatalf("exact name must rank first, got %q", hits[0].Name)
	}
	if out := FormatHits(hits); !strings.Contains(out, "demo.go:5") {
		t.Fatalf("format misses file:line:\n%s", out)
	}

	// Regex fallback.
	if got := idx.FindSymbol("^render", 5); len(got) != 1 || got[0].Name != "renderWidget" {
		t.Fatalf("regex find = %+v", got)
	}

	// Meaning-based search hits the doc comment.
	found := idx.Search("makes", 5)
	if len(found) == 0 || found[0].Name != "New" {
		t.Fatalf("search did not surface New: %+v", found)
	}
	if got := idx.FindSymbol("nonexistentSymbol", 5); len(got) != 0 {
		t.Fatalf("unexpected hits: %+v", got)
	}
}

func TestStoreReusesUnchangedFiles(t *testing.T) {
	root := writeTree(t)
	st := New("")

	first, err := st.Get(root)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	second, err := st.Get(root)
	if err != nil {
		t.Fatalf("get again: %v", err)
	}
	if first != second {
		t.Fatal("unchanged project must reuse the cached index")
	}

	// Editing one file must refresh only that file.
	edited := testGoSrc + "\n// Extra is new.\nfunc Extra() {}\n"
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(edited), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	third, err := st.Get(root)
	if err != nil {
		t.Fatalf("get after edit: %v", err)
	}
	f := third.FileByPathForTest("demo.go")
	if f == nil || !hasSym(f.Syms, "Extra", "func") {
		t.Fatalf("edit not picked up: %+v", f)
	}
	if !hasSym(f.Syms, "Widget", "struct") {
		t.Fatal("cached symbols lost after incremental rebuild")
	}
}

func TestStorePersistsToDisk(t *testing.T) {
	root := writeTree(t)
	dir := t.TempDir()

	if _, err := New(dir).Get(root); err != nil {
		t.Fatalf("get: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no cache file written: %v", err)
	}

	// A fresh store must load the index from disk.
	idx, err := New(dir).Get(root)
	if err != nil {
		t.Fatalf("get from disk: %v", err)
	}
	if idx.SymbolCount() == 0 {
		t.Fatal("disk cache lost symbols")
	}
}

func TestStoreRejectsEmptyRoot(t *testing.T) {
	if _, err := New("").Get(""); err == nil {
		t.Fatal("empty root must fail")
	}
}

// FileByPathForTest is a helper for tests (kept out of the production API).
func (idx *Index) FileByPathForTest(p string) *File {
	for i := range idx.Files {
		if idx.Files[i].Path == p {
			return &idx.Files[i]
		}
	}
	return nil
}

func hasSym(syms []Symbol, name, kind string) bool {
	for _, s := range syms {
		if s.Name == name && s.Kind == kind {
			return true
		}
	}
	return false
}
