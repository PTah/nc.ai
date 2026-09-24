package gitx

import (
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

// Clone клонирует репозиторий в <parentDir>/<имя репозитория>. Способ доступа
// выбирается сам: если SSH настроен и хост отвечает — клонируем по SSH, иначе
// по HTTPS. Если не сработало ни то, ни другое — возвращаем ошибку со всеми
// попытками, чтобы пользователь понял, чего не хватает.
func (s *Service) Clone(ctx context.Context, rawURL, parentDir string) (*CloneResult, error) {
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
	if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("папка %s уже существует и не пуста — выберите другую папку или удалите её", target)
	}

	res := &CloneResult{Path: target}
	for _, c := range remote.candidates(remote.sshReachable(ctx)) {
		err := s.cloneOne(ctx, c.url, target)
		if err == nil {
			res.UsedURL, res.Method = c.url, c.kind
			return res, nil
		}
		res.Attempts = append(res.Attempts, CloneAttempt{Kind: c.kind, URL: c.url, Err: firstLine(err.Error())})
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
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", remoteURL, target)
	cmd.Dir = filepath.Dir(target)
	configureCmd(cmd)
	cmd.Env = gitEnv(filepath.Dir(target))
	cmd.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("превышен лимит ожидания %s", gitNetworkTimeout)
	}
	if err != nil {
		return errors.New(strings.TrimSpace(out.String()))
	}
	return nil
}

// parseRemote понимает ssh://user@host:port/path, git@host:path и http(s)://host/path.
func parseRemote(raw string) (*remoteInfo, error) {
	r := strings.TrimSpace(raw)
	if r == "" {
		return nil, errors.New("укажите ссылку на репозиторий")
	}
	lower := strings.ToLower(r)
	// Локальный путь или file:// — клонируем как есть, без ssh/https.
	if strings.HasPrefix(lower, "file://") || (!strings.Contains(r, "://") && !strings.Contains(r, "@") &&
		(filepath.IsAbs(r) || strings.HasPrefix(r, "./") || strings.HasPrefix(r, "../"))) {
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

// candidates: своя ссылка пользователя первой, затем альтернативный способ.
// Если SSH настроен и хост отвечает — SSH идёт первым.
func (i *remoteInfo) candidates(sshReady bool) []cloneCandidate {
	primary := cloneCandidate{kind: i.kind, url: i.raw}
	if i.kind == "other" {
		return []cloneCandidate{primary}
	}
	alt := cloneCandidate{kind: "https", url: i.httpsURL()}
	if i.kind == "https" {
		alt = cloneCandidate{kind: "ssh", url: i.sshURL()}
	}
	if alt.url == primary.url {
		return []cloneCandidate{primary}
	}
	if i.kind == "https" && sshReady {
		return []cloneCandidate{alt, primary}
	}
	return []cloneCandidate{primary, alt}
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
	b.WriteString("Доступа нет ни по SSH, ни по HTTPS. Проверьте: ключ добавлен в аккаунт (Settings → SSH keys) и лежит в ~/.ssh; " +
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
