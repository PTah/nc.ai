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
	// Mutating — команда не просто читает, а меняет данные (Set-Content,
	// Remove-Item, git pull, перенаправление вывода и т. п.).
	Mutating bool `json:"mutating,omitempty"`
}

// Text — человекочитаемое пояснение для диалога подтверждения.
func (i *ScopeIssue) Text() string {
	if i == nil {
		return ""
	}
	switch {
	case i.Reason == "другой проект" && i.Mutating:
		return "команда изменяет другой проект: " + i.What
	case i.Reason == "другой проект":
		return "команда заходит в другой проект: " + i.What
	case i.Mutating:
		return "команда изменяет данные вне проекта: " + i.What
	default:
		return "команда выходит за пределы проекта: " + i.What
	}
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
	var roots []string
	if r.WS != nil {
		roots = r.ws().Roots()
	}
	if len(roots) == 0 {
		if root := strings.TrimSpace(r.WSRoot); root != "" {
			roots = []string{root}
		} else if wd, err := os.Getwd(); err == nil {
			roots = []string{wd}
		}
	}
	if len(roots) == 0 {
		return nil
	}
	cwd := roots[0]
	if rel := strings.TrimSpace(args.Cwd); rel != "" {
		cwd = filepath.Join(roots[0], rel)
		if r.WS != nil {
			// Имя папки рабочего пространства («api/src») разворачивается в её путь.
			if full, err := r.ws().Resolve(rel); err == nil {
				cwd = full
			}
		}
	}
	return checkCommandScopeRoots(roots, r.otherProjectRoots(roots), cwd, args.Command)
}

// otherProjectRoots — папки других проектов, известных приложению (без папок
// текущего рабочего пространства).
func (r *Registry) otherProjectRoots(own []string) []string {
	if r.WS == nil {
		return nil
	}
	cur := strings.TrimSpace(r.WSRoot)
	var out []string
	add := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" {
			return
		}
		for _, o := range own {
			if samePath(root, o) {
				return
			}
		}
		out = append(out, root)
	}
	for _, p := range r.WS.List() {
		if samePath(p.Path, cur) {
			continue
		}
		if p.Workspace {
			// Путь рабочего пространства — служебный ключ; настоящие папки — Roots.
			for _, root := range p.Roots {
				add(root)
			}
			continue
		}
		add(p.Path)
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
	return checkCommandScopeRoots([]string{root}, others, cwd, command)
}

// insideAny — путь лежит хотя бы в одной из папок рабочего пространства.
func insideAny(path string, roots []string) bool {
	for _, r := range roots {
		if inside(path, r) {
			return true
		}
	}
	return false
}

// checkCommandScopeRoots — то же для рабочего пространства из нескольких папок:
// все его папки считаются «внутри проекта». Первая папка — база по умолчанию.
func checkCommandScopeRoots(rawRoots []string, others []string, cwd, command string) *ScopeIssue {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	mutating := looksMutating(command)
	roots := make([]string, 0, len(rawRoots))
	for _, r := range rawRoots {
		if r = absClean(r); r != "" {
			roots = append(roots, r)
		}
	}
	root := ""
	if len(roots) > 0 {
		root = roots[0]
	}
	if cwd = strings.TrimSpace(cwd); cwd == "" {
		cwd = root
	}
	base := absClean(cwd)
	// Рабочий каталог сам может быть вне проекта (например, агент передал cwd
	// другого проекта) — это тоже повод спросить.
	if root != "" && base != "" && !insideAny(base, roots) && (mutating || !systemOrTemp(base)) {
		for _, other := range others {
			if other = absClean(other); other != "" && inside(base, other) {
				return &ScopeIssue{Reason: "другой проект", What: other, Path: base, Mutating: mutating}
			}
		}
		return &ScopeIssue{Reason: "вне проекта", What: base, Path: base, Mutating: mutating}
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
		if resolved == "" || insideAny(resolved, roots) {
			continue
		}
		for _, other := range others {
			if other = absClean(other); other != "" && inside(resolved, other) {
				return &ScopeIssue{Reason: "другой проект", What: other, Path: resolved, Mutating: mutating}
			}
		}
		// Системные и временные каталоги можно читать, но не менять.
		if systemOrTemp(resolved) && !mutating {
			continue
		}
		if outside == nil {
			outside = &ScopeIssue{Reason: "вне проекта", What: resolved, Path: resolved, Mutating: mutating}
		}
	}
	return outside
}

// mutatingPatterns — признаки того, что команда меняет данные: файловые
// командлеты, реестр/сервисы/планировщик, установка пакетов, git-мутации и
// перенаправление вывода в файл.
var mutatingPatterns = []string{
	// PowerShell / файлы
	"set-content", "add-content", "clear-content", "out-file", "new-item", "remove-item",
	"move-item", "copy-item", "rename-item", "set-item", "set-itemproperty", "new-itemproperty",
	"remove-itemproperty", "set-acl", "icacls", "takeown", "attrib", "expand-archive",
	"compress-archive", "tee-object", "robocopy", "xcopy", "mklink", "chmod", "chown",
	// cmd
	"del ", "erase ", "rmdir", "rd /s", "copy ", "move ", "mkdir", "md /", "ren ",
	// реестр, службы, планировщик, пакеты
	"reg add", "reg delete", "reg import", "sc create", "sc delete", "sc config",
	"schtasks", "netsh", "diskpart", "format ", "winget install", "choco install",
	"scoop install", "pip install", "pip3 install", "npm install", "npm ci", "npm i ",
	// git-мутации (чтение — status/log/diff/show — сюда не входит)
	"git pull", "git fetch", "git checkout", "git reset", "git clean", "git merge",
	"git rebase", "git stash", "git apply", "git commit", "git push", "git clone",
	"git rm", "git mv", "git switch",
}

// redirectRe — перенаправление вывода в файл. `2>&1` и `>&` — не запись в файл.
var redirectRe = regexp.MustCompile(`(^|[^0-9&])>>?(?:[^&=>]|$)`)

// looksMutating — меняет ли команда данные (а не только читает).
func looksMutating(command string) bool {
	text := strings.ToLower(command)
	for _, p := range mutatingPatterns {
		if strings.Contains(text, p) {
			return true
		}
	}
	return redirectRe.MatchString(command)
}

// systemOrTemp — системные и временные каталоги: их можно читать без
// подтверждения, а менять — нет.
func systemOrTemp(p string) bool {
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
