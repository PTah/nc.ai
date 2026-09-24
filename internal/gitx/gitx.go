package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"notcursor.ai/app/internal/workspace"
)

// Лимиты на git-команды. Без них push в недоступный remote висит бесконечно,
// а запрос пароля у Git Credential Manager может вытащить GUI-окно поверх чата.
const (
	gitCommandTimeout = 90 * time.Second
	gitNetworkTimeout = 300 * time.Second
)

// Service runs the OS `git` binary in the active workspace so auth comes from
// the user's credential helper / ssh-agent / ~/.ssh — same model as Cursor.
type Service struct {
	WS *workspace.Manager
	// Root, when set, pins git commands to one project for the whole agent run.
	Root string
}

func New(ws *workspace.Manager) *Service {
	return &Service{WS: ws}
}

// WithRoot returns a copy pinned to root (safe: Service holds no lock).
func (s *Service) WithRoot(root string) *Service {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Root = root
	if strings.TrimSpace(root) != "" {
		cp.WS = s.WS.Scoped(root)
	}
	return &cp
}

// root returns the project this service is pinned to (the run's project), or
// the project the user has open right now.
func (s *Service) root() (string, error) {
	if s == nil {
		return "", fmt.Errorf("no git service")
	}
	if r := strings.TrimSpace(s.Root); r != "" {
		return r, nil
	}
	return s.WS.ActiveRoot()
}

func (s *Service) run(args ...string) (string, error) {
	root, err := s.root()
	if err != nil {
		return "", err
	}
	timeout := gitCommandTimeout
	if isNetworkCommand(args) {
		timeout = gitNetworkTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	configureCmd(cmd)
	cmd.Env = gitEnv()
	// Не ждём потомков бесконечно, если гасим git по таймауту.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git %s: превышен лимит ожидания %s — проверьте доступность remote и ключи (пароли git приложение не спрашивает)",
				strings.Join(args, " "), timeout)
		}
		msg := errText
		if msg == "" {
			msg = out
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	if out == "" {
		return errText, nil
	}
	if errText != "" {
		return out + "\n" + errText, nil
	}
	return out, nil
}

// isNetworkCommand: команды, которые ходят в remote — им нужен запас времени.
func isNetworkCommand(args []string) bool {
	for _, a := range args {
		switch a {
		case "push", "pull", "fetch", "clone", "ls-remote", "submodule":
			return true
		}
	}
	return false
}

// gitEnv — окружение, в котором git никогда не ждёт ввода: иначе push в
// репозиторий с несохранёнными креденшелами подвисает (а GCM ещё и открывает
// своё окно поверх приложения).
func gitEnv() []string {
	env := os.Environ()
	env = append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_PAGER=cat",
		"PAGER=cat",
	)
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		// Ключ с паролем и без ssh-agent: падаем сразу с внятной ошибкой.
		env = append(env, "GIT_SSH_COMMAND=ssh -oBatchMode=yes -oStrictHostKeyChecking=accept-new")
	}
	return env
}

func (s *Service) Status() (string, error) {
	return s.run("status", "--short", "--branch")
}

func (s *Service) Diff(path string, staged bool) (string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	if path != "" {
		args = append(args, "--", path)
	}
	out, err := s.run(args...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "(no diff)", nil
	}
	return out, nil
}

func (s *Service) Log(n int, path string) (string, error) {
	if n <= 0 {
		n = 15
	}
	if n > 50 {
		n = 50
	}
	args := []string{"log", "--format=fuller", "-n", fmt.Sprintf("%d", n)}
	if strings.TrimSpace(path) != "" {
		args = append(args, "--", path)
	}
	out, err := s.run(args...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "(no commits)", nil
	}
	return out, nil
}

func (s *Service) Mv(from, to string) (string, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return "", fmt.Errorf("from and to are required")
	}
	dir := filepath.ToSlash(filepath.Dir(to))
	if dir != "." && dir != "" {
		if err := s.WS.Mkdir(dir); err != nil {
			return "", err
		}
	}
	return s.run("mv", "--", from, to)
}

func (s *Service) Commit(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("empty commit message")
	}
	// Stage tracked changes only (add -u): avoids sweeping in untracked files
	// the agent never touched (build artifacts, dumps, secrets).
	if _, err := s.run("add", "-u"); err != nil {
		return "", err
	}
	return s.run("commit", "-m", message)
}

// CommitPaths stages the given paths explicitly and commits them.
func (s *Service) CommitPaths(message string, paths []string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("empty commit message")
	}
	args := append([]string{"add", "--"}, paths...)
	if _, err := s.run(args...); err != nil {
		return "", err
	}
	return s.run("commit", "-m", message)
}

func (s *Service) Push(remote, branch string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	args := []string{"push", remote}
	if branch != "" {
		args = append(args, branch)
	}
	out, err := s.run(args...)
	if err != nil {
		return "", fmt.Errorf("%w\n(hint: настройте OS git credentials / SSH keys — NotCursor пароли Git не хранит и не спрашивает)", err)
	}
	if out == "" {
		return fmt.Sprintf("pushed to %s", remote), nil
	}
	return out, nil
}
