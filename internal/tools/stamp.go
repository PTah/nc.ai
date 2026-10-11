package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"notcursor.ai/app/internal/appmeta"
)

// Provenance header: every file the agent creates or edits gets a comment line
// at the top recording what happened, by which app build, on which machine and
// when. Later edits append their own line, so a project keeps the full history:
//
//	// Created by nc.ai 0.8.3.3 on DESKTOP-ABC 2026-10-11 15:04:22
//	# Edited by nc.ai 0.8.3.3 on DESKTOP-ABC 2026-10-11 15:10:05
//
// The marker is a real comment for the file type (never a literal "###", which
// would break Go/JS/TS/C). Files where a top comment is unsafe (JSON, binary,
// unknown formats) are left untouched.

// stampLineRe recognises a provenance line: it always carries "by nc.ai … on …"
// followed by a "YYYY-MM-DD HH:MM:SS" timestamp.
var stampLineRe = regexp.MustCompile(`by nc\.ai \S+ on \S+ \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

// stampStyle is how a single-line comment is written for a file type.
type stampStyle struct {
	line  string // line-comment token, e.g. "//" or "#"
	open  string // block-comment opener, e.g. "<!--"
	close string // block-comment closer, e.g. "-->"
}

// stampStyleFor returns the comment style for a path. ok=false means "do not
// touch" — JSON, binary and unknown formats, where a header comment would break
// the file.
func stampStyleFor(path string) (stampStyle, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".c", ".h", ".cc",
		".cpp", ".hpp", ".cs", ".java", ".kt", ".kts", ".rs", ".swift", ".scala",
		".dart", ".proto", ".groovy":
		return stampStyle{line: "//"}, true
	case ".py", ".pyi", ".sh", ".bash", ".zsh", ".ksh", ".ps1", ".psm1", ".yaml",
		".yml", ".toml", ".ini", ".cfg", ".conf", ".properties", ".rb", ".pl",
		".r", ".jl", ".env", ".tf", ".hcl", ".nim", ".mk", ".graphql", ".gql",
		".make":
		return stampStyle{line: "#"}, true
	case ".sql", ".lua", ".hs", ".elm", ".adb", ".ads":
		return stampStyle{line: "--"}, true
	case ".clj", ".cljs", ".edn", ".lisp", ".el":
		return stampStyle{line: ";;"}, true
	case ".html", ".htm", ".xml", ".svg", ".xhtml", ".vue", ".svelte", ".md", ".markdown":
		return stampStyle{open: "<!--", close: "-->"}, true
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "dockerfile", "makefile", "rakefile", "gemfile", "procfile", "justfile":
		return stampStyle{line: "#"}, true
	}
	return stampStyle{}, false
}

// stampFileHeader adds (or refreshes) our provenance line at the top of a text
// file the agent just touched. It is best-effort: any problem is ignored so the
// tool result is never failed because of the stamp.
func (r *Registry) stampFileHeader(path, op string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	style, ok := stampStyleFor(path)
	if !ok {
		return
	}
	raw, err := r.ws().ReadFileRaw(path)
	if err != nil || len(raw) > 2<<20 || looksBinaryText(raw) {
		return
	}
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "unknown-host"
	}
	when := time.Now().Format("2006-01-02 15:04:05")
	body := fmt.Sprintf("%s by nc.ai %s on %s", op, appmeta.Version, host)
	full := stampWrap(style, body+" "+when)

	next := mergeStampBlock(raw, full, body)
	if next == raw {
		return
	}
	_ = r.ws().WriteFile(path, next)
}

func stampWrap(s stampStyle, text string) string {
	if s.open != "" {
		return s.open + " " + text + " " + s.close
	}
	return s.line + " " + text
}

// mergeStampBlock inserts `full` as the first line, or appends it to the existing
// provenance block; if a line for the same build+op+host already exists, its
// timestamp is refreshed instead of adding a duplicate.
func mergeStampBlock(src, full, bodyPrefix string) string {
	crlf := strings.Contains(src, "\r\n")
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")

	start := 0
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
		start = 1 // keep a shebang first, otherwise the script breaks
	}
	end := start
	for end < len(lines) && stampLineRe.MatchString(lines[end]) {
		end++
	}

	sameLine := regexp.MustCompile(regexp.QuoteMeta(bodyPrefix) + ` \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
	for i := start; i < end; i++ {
		if sameLine.MatchString(lines[i]) {
			lines[i] = full
			return joinStampLines(lines, crlf)
		}
	}
	lines = insertLine(lines, end, full)
	return joinStampLines(lines, crlf)
}

func insertLine(lines []string, at int, line string) []string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, line)
	out = append(out, lines[at:]...)
	return out
}

func joinStampLines(lines []string, crlf bool) string {
	out := strings.Join(lines, "\n")
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return out
}

// looksBinaryText reports whether the head of s contains a NUL byte.
func looksBinaryText(s string) bool {
	n := len(s)
	if n > 8192 {
		n = 8192
	}
	return strings.IndexByte(s[:n], 0) >= 0
}
