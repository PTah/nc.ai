package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"notcursor.ai/app/internal/workspace"
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
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	configureCmd(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	if err != nil {
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
		return "", fmt.Errorf("%w\n(hint: configure OS git credentials / SSH keys — NotCursor does not store Git passwords)", err)
	}
	if out == "" {
		return fmt.Sprintf("pushed to %s", remote), nil
	}
	return out, nil
}
