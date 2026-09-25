package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Границы проекта. Агент работает внутри папки открытого проекта, и любое
// обращение к чужому проекту или просто за пределы рабочей папки — это то же
// самое, что лезть в другой репозиторий: такие команды требуют явного
// подтверждения пользователя, даже если автоподтверждение инструментов выключено.

// ScopeIssue — что именно команда задела вне текущего проекта.
type ScopeIssue struct {
	Path   string `json:"path"`
	Reason string `json:"reason"` // «другой проект» | «вне проекта»
	What   string `json:"what"`   // путь, который сработал
}

// Text — человекочитаемое пояснение для диалога подтверждения.
func (i *ScopeIssue) Text() string {
	if i == nil {
		return ""
	}
	if i.Reason == "другой проект" {
		return "команда заходит в другой проект: " + i.What
	}
	return "команда выходит за пределы проекта: " + i.What
}

// CommandScopeIssue проверяет аргументы инструмента на выход за границы проекта.
// nil — всё в пределах рабочей папки, подтверждение не нужно.
func (r *Registry) CommandScopeIssue(name, argsJSON string) *ScopeIssue {
	if r == nil {
		return nil
	}
	switch name {
	case "run_terminal", "command_status":
	default:
		return nil
	}
	var args struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil
	}
	root := strings.TrimSpace(r.WSRoot)
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	cwd := root
	if strings.TrimSpace(args.Cwd) != "" {
		cwd = filepath.Join(root, args.Cwd)
	}
	return checkCommandScope(root, r.otherProjectRoots(), cwd, args.Command)
}

// otherProjectRoots — папки других проектов, известных приложению.
func (r *Registry) otherProjectRoots() []string {
	if r.WS == nil {
		return nil
	}
	cur := strings.TrimSpace(r.WSRoot)
	var out []string
	for _, p := range r.WS.List() {
		root := strings.TrimSpace(p.Path)
		if root == "" || samePath(root, cur) {
			continue
		}
		out = append(out, root)
	}
	return out
}

var (
	// Windows drive paths: D:\x, C:/y (без кавычек и разделителей команд).
	drivePathRe = regexp.MustCompile(`(?i)\b[a-z]:[\\/][^\s"'` + "`" + `|;&)<>,]*`)
	// UNC: \\server\share\...
	uncPathRe = regexp.MustCompile(`\\\\[^\s"'` + "`" + `|;&)<>,]+`)
	// Относительные выходы: ..\x, ../x
	dotdotPathRe = regexp.MustCompile(`(?:^|[\s"'(=])((?:\.\.[\\/])+[^\s"'` + "`" + `|;&)<>,]*)`)
	// Домашний каталог: ~\x, ~/.ssh/...
	homePathRe = regexp.MustCompile(`(?:^|[\s"'(=])(~[\\/][^\s"'` + "`" + `|;&)<>,]*)`)
	// Переменные окружения, которые ведут за пределы проекта.
	envPathRe = regexp.MustCompile(`(?i)(\$env:(userprofile|home|homedrive|homepath|appdata|localappdata|programfiles|programdata)|%(userprofile|home|homedrive|homepath|appdata|localappdata|programfiles|programdata)%)`)
	// POSIX-абсолютные пути (/home/..., /etc/...) — только вне Windows, чтобы не
	// путать флаги вида `/F` c путями.
	posixPathRe = regexp.MustCompile(`(?:^|[\s"'(=])(/[^\s"'` + "`" + `|;&)<>,]+)`)
)

// checkCommandScope — ядро проверки (без Registry, чтобы легко тестировать).
func checkCommandScope(root string, others []string, cwd, command string) *ScopeIssue {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	root = absClean(root)
	if cwd = strings.TrimSpace(cwd); cwd == "" {
		cwd = root
	}
	base := absClean(cwd)
	// Рабочий каталог сам может быть вне проекта (например, агент передал cwd
	// другого проекта) — это тоже повод спросить.
	if root != "" && base != "" && !inside(base, root) && !allowedOutside(base) {
		for _, other := range others {
			if other = absClean(other); other != "" && inside(base, other) {
				return &ScopeIssue{Reason: "другой проект", What: other, Path: base}
			}
		}
		return &ScopeIssue{Reason: "вне проекта", What: base, Path: base}
	}

	var paths []string
	paths = append(paths, drivePathRe.FindAllString(command, -1)...)
	paths = append(paths, uncPathRe.FindAllString(command, -1)...)
	for _, m := range dotdotPathRe.FindAllStringSubmatch(command, -1) {
		paths = append(paths, m[1])
	}
	for _, m := range homePathRe.FindAllStringSubmatch(command, -1) {
		paths = append(paths, m[1])
	}
	if envPathRe.MatchString(command) {
		paths = append(paths, "~") // любое обращение к профилю — за пределами проекта
	}
	if runtime.GOOS != "windows" {
		paths = append(paths, posixPathRe.FindAllString(command, -1)...)
	}

	var outside *ScopeIssue
	for _, raw := range paths {
		raw = strings.Trim(strings.TrimSpace(raw), `"'`)
		if raw == "" || strings.Contains(raw, "://") {
			continue
		}
		resolved := absClean(resolvePath(raw, base))
		if resolved == "" || inside(resolved, root) {
			continue
		}
		for _, other := range others {
			if other = absClean(other); other != "" && inside(resolved, other) {
				return &ScopeIssue{Reason: "другой проект", What: other, Path: resolved}
			}
		}
		if allowedOutside(resolved) {
			continue
		}
		if outside == nil {
			outside = &ScopeIssue{Reason: "вне проекта", What: resolved, Path: resolved}
		}
	}
	return outside
}

// allowedOutside — системные и временные каталоги: команды вида `python -m venv
// $env:TEMP\venv` или обращение к установленному инструменту не должны требовать
// подтверждения на каждом шаге.
func allowedOutside(p string) bool {
	for _, dir := range []string{os.TempDir(), os.Getenv("TEMP"), os.Getenv("TMP")} {
		if dir != "" && inside(p, dir) {
			return true
		}
	}
	lower := strings.ToLower(filepath.ToSlash(p))
	for _, prefix := range []string{
		"c:/windows/", "c:/program files/", "c:/program files (x86)/",
		"c:/programdata/", "c:/$recycle.bin",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func resolvePath(raw, base string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if len(raw) == 1 {
				return home
			}
			return filepath.Join(home, strings.TrimLeft(raw[1:], `\/`))
		}
		return raw
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	return filepath.Join(base, raw)
}

func absClean(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// inside — путь лежит внутри папки (сама папка тоже считается).
func inside(path, dir string) bool {
	path, dir = absClean(path), absClean(dir)
	if path == "" || dir == "" {
		return false
	}
	if samePath(path, dir) {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(strings.ToLower(path), strings.ToLower(strings.TrimRight(dir, sep))+sep)
}

func samePath(a, b string) bool {
	return strings.EqualFold(absClean(a), absClean(b))
}
