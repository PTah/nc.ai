// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:24:54
package projindex

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"strings"
)

// extractGo parses one Go file and returns its package name, declarations and
// whether it is an entry point (package main with func main).
func extractGo(rel string, src []byte) (pkg string, syms []Symbol, entry bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil || f == nil {
		return "", nil, false
	}
	pkg = f.Name.Name
	line := func(p token.Pos) int { return fset.Position(p).Line }

	for _, decl := range f.Decls {
		if len(syms) >= maxSymsPerFile {
			break
		}
		switch d := decl.(type) {
		case *ast.FuncDecl:
			s := Symbol{Name: d.Name.Name, Kind: "func", Line: line(d.Pos()), Doc: firstDoc(d.Doc)}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				s.Kind = "method"
				s.Recv = types.ExprString(d.Recv.List[0].Type)
			}
			if d.Recv == nil && pkg == "main" && s.Name == "main" {
				entry = true
			}
			syms = append(syms, s)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if len(syms) >= maxSymsPerFile {
					break
				}
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					switch sp.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					doc := sp.Doc
					if doc == nil {
						doc = d.Doc
					}
					syms = append(syms, Symbol{Name: sp.Name.Name, Kind: kind, Line: line(sp.Pos()), Doc: firstDoc(doc)})
				case *ast.ValueSpec:
					kind := d.Tok.String() // const | var
					doc := sp.Doc
					if doc == nil {
						doc = d.Doc
					}
					for _, n := range sp.Names {
						if n == nil || n.Name == "_" {
							continue
						}
						syms = append(syms, Symbol{Name: n.Name, Kind: kind, Line: line(n.Pos()), Doc: firstDoc(doc)})
					}
				}
			}
		}
	}
	return pkg, syms, entry
}

// firstDoc renders a Go doc comment as one trimmed line.
func firstDoc(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	return oneLine(doc.Text())
}

// rule is one declaration pattern for a non-Go language; group 1 is the name.
type rule struct {
	re   *regexp.Regexp
	kind string
}

var (
	rulesJS = []rule{
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)`), "func"},
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=]+)?=\s*(?:async\s*)?(?:function\b|\(|<[^>]*>\s*\()`), "func"},
		{regexp.MustCompile(`^\s*(?:export\s+)?(?:declare\s+)?(?:abstract\s+)?(?:class|interface|enum|type)\s+([A-Za-z_$][\w$]*)`), "type"},
	}
	rulesPy = []rule{
		{regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)`), "def"},
		{regexp.MustCompile(`^\s*class\s+([A-Za-z_]\w*)`), "class"},
	}
	rulesCSharp = []rule{
		{regexp.MustCompile(`^\s*(?:(?:public|private|internal|protected|static|sealed|abstract|partial|readonly|new)\s+)*(?:class|record|struct|interface|enum)\s+([A-Za-z_]\w*)`), "class"},
		{regexp.MustCompile(`^\s*(?:(?:public|private|internal|protected|static|virtual|override|async|extern|unsafe|partial|sealed)\s+)+[\w<>\[\],\.\?]+\s+([A-Za-z_]\w*)\s*\(`), "method"},
	}
	rulesPS = []rule{
		{regexp.MustCompile(`(?i)^\s*function\s+([\w-]+)`), "function"},
	}
	rulesSh = []rule{
		{regexp.MustCompile(`^\s*function\s+([\w.:-]+)`), "func"},
		{regexp.MustCompile(`^\s*([\w.:-]+)\s*\(\)\s*\{`), "func"},
	}
	rulesRust = []rule{
		{regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+([A-Za-z_]\w*)`), "func"},
		{regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|trait|union|mod)\s+([A-Za-z_]\w*)`), "type"},
	}
	rulesJVM = []rule{
		{regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|abstract|final|sealed|data|open|static)\s+)*(?:class|interface|enum|record|object)\s+([A-Za-z_]\w*)`), "class"},
		{regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|static|final|abstract|open|override|suspend|inline)\s+)*(?:fun)\s+(?:<[^>]*>\s*)?(?:[\w.]+\.)?([A-Za-z_]\w*)\s*\(`), "func"},
	}
	rulesRuby = []rule{
		{regexp.MustCompile(`^\s*def\s+(?:self\.)?([A-Za-z_]\w*[\?!=]?)`), "def"},
		{regexp.MustCompile(`^\s*(?:class|module)\s+([A-Z]\w*)`), "class"},
	}
)

// langRules maps a language id (see langOf) to its declaration patterns.
var langRules = map[string][]rule{
	"ts": rulesJS, "tsx": rulesJS, "js": rulesJS, "jsx": rulesJS, "mjs": rulesJS, "cjs": rulesJS,
	"vue": rulesJS, "svelte": rulesJS,
	"py":  rulesPy,
	"cs":  rulesCSharp,
	"ps1": rulesPS, "psm1": rulesPS,
	"sh": rulesSh, "bash": rulesSh, "zsh": rulesSh,
	"rs":   rulesRust,
	"java": rulesJVM, "kt": rulesJVM, "kts": rulesJVM, "scala": rulesJVM, "groovy": rulesJVM,
	"rb": rulesRuby,
}

// langExt maps a file extension to a language id.
var langExt = map[string]string{
	".go": "go",
	".ts": "ts", ".tsx": "tsx", ".js": "js", ".jsx": "jsx", ".mjs": "mjs", ".cjs": "cjs",
	".vue": "vue", ".svelte": "svelte",
	".py":  "py",
	".cs":  "cs",
	".ps1": "ps1", ".psm1": "psm1",
	".sh": "sh", ".bash": "bash", ".zsh": "zsh",
	".rs":   "rs",
	".java": "java", ".kt": "kt", ".kts": "kts", ".scala": "scala", ".groovy": "groovy",
	".rb": "rb",
}

// langOf returns the language id of a file name ("" when not indexed).
func langOf(name string) string {
	return langExt[strings.ToLower(fileExt(name))]
}

func fileExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// extractRegex finds declarations in non-Go sources line by line.
func extractRegex(lang string, src []byte) []Symbol {
	rules := langRules[lang]
	if len(rules) == 0 {
		return nil
	}
	lines := strings.Split(string(src), "\n")
	out := make([]Symbol, 0, 32)
	seen := map[string]bool{}
	for i, line := range lines {
		if len(out) >= maxSymsPerFile {
			break
		}
		for _, r := range rules {
			m := r.re.FindStringSubmatch(line)
			if m == nil || len(m) < 2 || m[1] == "" {
				continue
			}
			key := r.kind + "|" + m[1]
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Symbol{
				Name: m[1],
				Kind: r.kind,
				Line: i + 1,
				Doc:  commentAbove(lines, i),
			})
			break
		}
	}
	return out
}

// commentAbove collects up to two comment lines directly above line i.
func commentAbove(lines []string, i int) string {
	var parts []string
	for j := i - 1; j >= 0 && len(parts) < 2; j-- {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			break
		}
		trimmed, ok := trimComment(t)
		if !ok {
			break
		}
		parts = append([]string{trimmed}, parts...)
	}
	return oneLine(strings.Join(parts, " "))
}

// trimComment strips a leading comment marker; ok is false for ordinary code.
func trimComment(t string) (string, bool) {
	switch {
	case strings.HasPrefix(t, "///"):
		return strings.TrimSpace(strings.TrimPrefix(t, "///")), true
	case strings.HasPrefix(t, "//"):
		return strings.TrimSpace(strings.TrimPrefix(t, "//")), true
	case strings.HasPrefix(t, "#"):
		return strings.TrimSpace(strings.TrimPrefix(t, "#")), true
	case strings.HasPrefix(t, "/*"):
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "/*"), "*/")), true
	case strings.HasPrefix(t, "*"):
		return strings.TrimSpace(strings.TrimPrefix(t, "*")), true
	case strings.HasPrefix(t, "--"):
		return strings.TrimSpace(strings.TrimPrefix(t, "--")), true
	case strings.HasPrefix(t, "REM "), strings.HasPrefix(t, "rem "):
		return strings.TrimSpace(t[4:]), true
	}
	return "", false
}

// oneLine collapses whitespace and trims a comment to docLimit.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > docLimit {
		s = strings.TrimSpace(s[:docLimit]) + "…"
	}
	return s
}
