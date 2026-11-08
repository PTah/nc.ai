// Edited by nc.ai 1.1.8 on macbook 2026-11-08 20:52:33
package gitx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sshConfigPath — путь к ~/.ssh/config. Переменная, чтобы тесты не трогали
// реальный файл пользователя.
var sshConfigPath = defaultSSHConfigPath()

// remoteProbeTimeout — сколько ждём ответ хоста при выборе способа доступа.
const remoteProbeTimeout = 8 * time.Second

// CloneAttempt — одна попытка доступа и её результат (для сообщения об ошибке).
type CloneAttempt struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
	Err  string `json:"err"`
}

// CloneResult — итог клонирования.
type CloneResult struct {
	Path     string         `json:"path"`
	UsedURL  string         `json:"usedUrl"`
	Method   string         `json:"method"`
	Attempts []CloneAttempt `json:"attempts"`
}

type remoteInfo struct {
	raw  string
	kind string // ssh | https | other
	user string
	host string
	port int
	path string
}

type cloneCandidate struct {
	kind string
	url  string
}

// ErrCloneCanceled — пользователь нажал «Отмена» (или закрыл диалог) во время clone.
var ErrCloneCanceled = errors.New("клонирование отменено")

// Progress — этап клонирования для интерфейса.
type Progress struct {
	Phase   string `json:"phase"`   // download | unpack | git | open
	Percent int    `json:"percent"` // -1 — процент неизвестен (показываем «бегущую» полосу)
	Detail  string `json:"detail"`  // «3.2 МБ из 7.4 МБ», «Receiving objects: 45%», «1200 из 3400 файлов»
}

// report отдаёт этап в интерфейс, если колбэк установлен.
func (s *Service) report(p Progress) {
	if s != nil && s.OnProgress != nil {
		s.OnProgress(p)
	}
}

// humanBytes — «3.2 МБ» без лишних знаков после запятой.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d Б", n)
	}
	units := []string{"КБ", "МБ", "ГБ", "ТБ"}
	val := float64(n)
	for _, u := range units {
		val /= unit
		if val < unit {
			if val < 10 {
				return fmt.Sprintf("%.1f %s", val, u)
			}
			return fmt.Sprintf("%.0f %s", val, u)
		}
	}
	return fmt.Sprintf("%.0f ТБ", val/unit)
}

// parseGitProgress вытаскивает процент из строк git clone --progress:
//
//	remote: Enumerating objects: 1200, done.
//	Receiving objects:  45% (1234/2743), 3.20 MiB | 1.10 MiB/s
//	Resolving deltas:  10% (100/1000)
//
// git печатает прогресс через \r без перевода строки, поэтому строки приходят
// «склеенными» — берём последний сегмент после \r.
func parseGitProgress(line string) (Progress, bool) {
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	text := strings.TrimSpace(line)
	text = strings.TrimPrefix(text, "remote: ")
	lower := strings.ToLower(text)
	if text == "" || !strings.Contains(text, "%") {
		return Progress{}, false
	}
	label := ""
	switch {
	case strings.HasPrefix(lower, "counting objects"):
		label = "подсчёт объектов"
	case strings.HasPrefix(lower, "compressing objects"):
		label = "сжатие объектов"
	case strings.HasPrefix(lower, "receiving objects"):
		label = "получение объектов"
	case strings.HasPrefix(lower, "resolving deltas"):
		label = "разрешение дельт"
	case strings.HasPrefix(lower, "enumerating objects"):
		label = "перечисление объектов"
	default:
		return Progress{}, false
	}
	// «Receiving objects:  45%» — процент после двоеточия.
	pct := -1
	if idx := strings.Index(text, ":"); idx >= 0 {
		rest := strings.TrimSpace(text[idx+1:])
		if p := strings.IndexByte(rest, '%'); p > 0 {
			if v, err := strconv.Atoi(strings.TrimSpace(rest[:p])); err == nil && v >= 0 && v <= 100 {
				pct = v
			}
		}
	}
	detail := label
	if pct >= 0 {
		detail = fmt.Sprintf("%s: %d%%", label, pct)
	}
	return Progress{Phase: "git", Percent: pct, Detail: detail}, true
}

// Clone клонирует репозиторий в <parentDir>/<имя репозитория>.
// Для github.com сначала пробуем REST zipball (API часто живёт, когда git-pack
// ползёт), затем SSH/HTTPS. ctx отменяется из CancelClone — убивает git и обрыв HTTP.
func (s *Service) Clone(ctx context.Context, rawURL, parentDir string) (*CloneResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	remote, err := parseRemote(rawURL)
	if err != nil {
		return nil, err
	}
	parent := strings.TrimSpace(parentDir)
	if parent == "" {
		return nil, errors.New("не выбрана папка, куда клонировать проект")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("не удалось подготовить папку %s: %w", parent, err)
	}
	target := filepath.Join(parent, remote.repoName())
	if st, err := os.Stat(target); err == nil {
		if !st.IsDir() {
			return nil, fmt.Errorf("%s — это файл, а не папка; выберите другую папку", target)
		}
		if entries, rerr := os.ReadDir(target); rerr == nil && len(entries) > 0 {
			return nil, fmt.Errorf("папка %s уже существует и не пуста — выберите другую папку или удалите её", target)
		}
	}

	res := &CloneResult{Path: target}
	// GitHub: API первым, без 8с SSH-probe — zipball часто жив, пока git-pack ползёт.
	sshReady := false
	if remote.githubZipballURL() == "" {
		sshReady = remote.sshReachable(ctx)
	}
	for _, c := range remote.candidates(sshReady) {
		if err := ctx.Err(); err != nil {
			_ = os.RemoveAll(target)
			return nil, ErrCloneCanceled
		}
		var attemptErr error
		switch c.kind {
		case "api":
			attemptErr = s.cloneGitHubAPI(ctx, remote, target)
		default:
			attemptErr = s.cloneOne(ctx, c.url, target)
		}
		if attemptErr == nil {
			res.UsedURL, res.Method = c.url, c.kind
			return res, nil
		}
		if errors.Is(attemptErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			_ = os.RemoveAll(target)
			return nil, ErrCloneCanceled
		}
		res.Attempts = append(res.Attempts, CloneAttempt{Kind: c.kind, URL: c.url, Err: firstLine(attemptErr.Error())})
		// Недоклонированное убираем, чтобы следующая попытка шла в чистую папку.
		_ = os.RemoveAll(target)
	}
	return nil, fmt.Errorf("%s", cloneFailureText(remote, res.Attempts))
}

// CloneTarget — путь, куда ляжет репозиторий (для предпросмотра в интерфейсе).
func (s *Service) CloneTarget(rawURL, parentDir string) (string, error) {
	remote, err := parseRemote(rawURL)
	if err != nil {
		return "", err
	}
	parent := strings.TrimSpace(parentDir)
	if parent == "" {
		return "", nil
	}
	return filepath.Join(parent, remote.repoName()), nil
}

func (s *Service) cloneOne(ctx context.Context, remoteURL, target string) error {
	ctx, cancel := context.WithTimeout(ctx, gitNetworkTimeout)
	defer cancel()
	// --progress: без него git молчит, и в диалоге нечего показать. Прогресс идёт
	// в stderr строками через \r, поэтому читаем его сканером и отдаём в UI.
	cmd := exec.CommandContext(ctx, "git", "clone", "--progress", remoteURL, target)
	cmd.Dir = filepath.Dir(target)
	configureCmd(cmd)
	cmd.Env = gitEnv(filepath.Dir(target))
	cmd.WaitDelay = 5 * time.Second
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		scan := bufio.NewScanner(stderrPipe)
		scan.Buffer(make([]byte, 0, 64*1024), 256*1024)
		scan.Split(splitProgressLines)
		for scan.Scan() {
			line := scan.Text()
			stderr.WriteString(line + "\n")
			if p, ok := parseGitProgress(line); ok {
				s.report(p)
			}
		}
	}()
	err = cmd.Run()
	<-done
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("превышен лимит ожидания %s", gitNetworkTimeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// splitProgressLines режет поток по \n и \r: git clone --progress перерисовывает
// строку через возврат каретки, и без этого весь прогресс пришёл бы одной строкой.
func splitProgressLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// parseRemote понимает ssh://user@host:port/path, git@host:path и http(s)://host/path.
// isWindowsAbsPath — «D:\repos\origin» или «D:/repos/origin». filepath.IsAbs знает
// только текущую ОС, поэтому путь из Windows, вставленный на macOS, иначе не
// распознавался как локальный и падал в «не понимаю ссылку».
func isWindowsAbsPath(p string) bool {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return false
	}
	c := p[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func parseRemote(raw string) (*remoteInfo, error) {
	r := strings.TrimSpace(raw)
	if r == "" {
		return nil, errors.New("укажите ссылку на репозиторий")
	}
	lower := strings.ToLower(r)
	// Локальный путь или file:// — клонируем как есть, без ssh/https.
	if strings.HasPrefix(lower, "file://") || (!strings.Contains(r, "://") && !strings.Contains(r, "@") &&
		(filepath.IsAbs(r) || isWindowsAbsPath(r) || strings.HasPrefix(r, "./") || strings.HasPrefix(r, "../"))) {
		info := &remoteInfo{raw: r, kind: "other", path: r}
		if info.repoName() == "" {
			return nil, fmt.Errorf("в ссылке %q нет имени репозитория", raw)
		}
		return info, nil
	}
	if !strings.Contains(r, "://") {
		// scp-подобный вид: git@host:path
		at := strings.Index(r, "@")
		colon := strings.Index(r, ":")
		if at > 0 && colon > at {
			return &remoteInfo{
				raw: r, kind: "ssh",
				user: r[:at], host: r[at+1 : colon],
				path: "/" + strings.TrimPrefix(r[colon+1:], "/"),
			}, nil
		}
		return nil, fmt.Errorf("не понимаю ссылку %q — ожидаю ssh://…, git@host:path или https://…", raw)
	}
	u, err := url.Parse(r)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("не понимаю ссылку %q — ожидаю ssh://…, git@host:path или https://…", raw)
	}
	info := &remoteInfo{raw: r, host: u.Hostname(), user: u.User.Username(), path: u.Path}
	if p := u.Port(); p != "" {
		info.port, _ = strconv.Atoi(p)
	}
	switch strings.ToLower(u.Scheme) {
	case "ssh", "git+ssh":
		info.kind = "ssh"
	case "http", "https":
		info.kind = "https"
	default:
		info.kind = "other"
	}
	if info.kind == "ssh" && info.user == "" {
		info.user = "git"
	}
	if info.path == "" || info.repoName() == "" {
		return nil, fmt.Errorf("в ссылке %q нет имени репозитория", raw)
	}
	return info, nil
}

// sshURL — SSH-вариант той же ссылки. Порт берём из URL, иначе из ~/.ssh/config.
func (i *remoteInfo) sshURL() string {
	user := i.user
	if user == "" {
		user = "git"
	}
	port := i.port
	if port == 0 {
		if cfg, ok := lookupSSHConfig(i.host); ok {
			port = cfg.port
			if user == "git" && cfg.user != "" {
				user = cfg.user
			}
		}
	}
	host := i.host
	if port > 0 && port != 22 {
		host = fmt.Sprintf("%s:%d", host, port)
	}
	return fmt.Sprintf("ssh://%s@%s%s", user, host, ensureGitSuffix(i.path))
}

// httpsURL — веб-вариант той же ссылки (порт SSH к нему не относится).
func (i *remoteInfo) httpsURL() string {
	return fmt.Sprintf("https://%s%s", i.host, ensureGitSuffix(i.path))
}

// candidates: для GitHub — сначала REST zipball (API), затем git SSH/HTTPS.
// Если SSH настроен и хост отвечает — SSH раньше HTTPS.
func (i *remoteInfo) candidates(sshReady bool) []cloneCandidate {
	primary := cloneCandidate{kind: i.kind, url: i.raw}
	if i.kind == "other" {
		return []cloneCandidate{primary}
	}
	var out []cloneCandidate
	if api := i.githubZipballURL(); api != "" {
		out = append(out, cloneCandidate{kind: "api", url: api})
	}
	alt := cloneCandidate{kind: "https", url: i.httpsURL()}
	if i.kind == "https" {
		alt = cloneCandidate{kind: "ssh", url: i.sshURL()}
	}
	if alt.url == primary.url {
		return append(out, primary)
	}
	if i.kind == "https" && sshReady {
		return append(out, alt, primary)
	}
	return append(out, primary, alt)
}

func (i *remoteInfo) repoName() string {
	p := i.path
	if i.kind == "other" {
		p = i.raw
	}
	p = strings.TrimSuffix(strings.TrimRight(p, "/\\"), ".git")
	if idx := strings.LastIndexAny(p, "/\\"); idx >= 0 {
		p = p[idx+1:]
	}
	return strings.TrimSpace(p)
}

// sshReachable проверяет, что по SSH реально есть доступ к репозиторию: идём
// через git ls-remote, потому что он использует ровно тот ssh, который потом
// применит clone (core.sshCommand, ключи, ~/.ssh/config).
func (i *remoteInfo) sshReachable(ctx context.Context) bool {
	if i.kind != "https" && i.kind != "ssh" {
		return false
	}
	key := i.sshURL()
	sshProbeMu.Lock()
	if v, ok := sshProbeCache[key]; ok {
		sshProbeMu.Unlock()
		return v
	}
	sshProbeMu.Unlock()

	probeCtx, cancel := context.WithTimeout(ctx, remoteProbeTimeout)
	defer cancel()
	// Без --exit-code: пустой репозиторий (нет ссылок) — это тоже рабочий доступ.
	cmd := exec.CommandContext(probeCtx, "git", "ls-remote", key)
	configureCmd(cmd)
	cmd.Env = gitEnv("")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	ok := err == nil && probeCtx.Err() == nil

	sshProbeMu.Lock()
	sshProbeCache[key] = ok
	sshProbeMu.Unlock()
	return ok
}

var (
	sshProbeMu    sync.Mutex
	sshProbeCache = map[string]bool{}
)

func cloneFailureText(remote *remoteInfo, attempts []CloneAttempt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "не удалось склонировать %s с %s.\n", remote.repoName(), remote.host)
	for _, a := range attempts {
		fmt.Fprintf(&b, "• %s (%s): %s\n", strings.ToUpper(a.Kind), a.URL, a.Err)
	}
	if remote != nil && githubAPIHost(remote.host) {
		b.WriteString("Не сработали GitHub API (zipball), SSH и HTTPS. ")
	} else {
		b.WriteString("Доступа нет ни по SSH, ни по HTTPS. ")
	}
	b.WriteString("Проверьте: ключ добавлен в аккаунт (Settings → SSH keys) и лежит в ~/.ssh; " +
		"либо есть логин/токен для HTTPS (git credential manager), а у ключа/токена есть права на этот репозиторий.")
	return b.String()
}

// --- ~/.ssh/config ---

type sshHostConfig struct {
	port int
	user string
}

func defaultSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// lookupSSHConfig ищет Host-блок для конкретного хоста: Port и User.
func lookupSSHConfig(host string) (sshHostConfig, bool) {
	path := sshConfigPath
	if path == "" {
		return sshHostConfig{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return sshHostConfig{}, false
	}
	cfg := sshHostConfig{}
	found := false
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			inBlock = false
			for _, pattern := range fields[1:] {
				if strings.EqualFold(pattern, host) {
					inBlock, found = true, true
					break
				}
			}
		case "port":
			if inBlock && len(fields) > 1 {
				cfg.port, _ = strconv.Atoi(fields[1])
			}
		case "user":
			if inBlock && len(fields) > 1 {
				cfg.user = fields[1]
			}
		}
	}
	return cfg, found
}

func ensureGitSuffix(p string) string {
	if p == "" {
		return p
	}
	if strings.HasSuffix(p, ".git") {
		return p
	}
	return p + ".git"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	if s == "" {
		s = "без подробностей"
	}
	return s
}
