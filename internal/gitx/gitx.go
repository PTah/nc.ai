package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"notcursor.ai/app/internal/workspace"
)

// Service runs the OS `git` binary in the active workspace so auth comes from
// the user's credential helper / ssh-agent / ~/.ssh — same model as Cursor.
type Service struct {
	WS *workspace.Manager
}

func New(ws *workspace.Manager) *Service {
	return &Service{WS: ws}
}

func (s *Service) run(args ...string) (string, error) {
	root, err := s.WS.ActiveRoot()
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

func (s *Service) Commit(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("empty commit message")
	}
	if _, err := s.run("add", "-A"); err != nil {
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
