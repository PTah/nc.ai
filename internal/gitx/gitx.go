package gitx

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"notcursor.ai/app/internal/workspace"
)

type Service struct {
	WS       *workspace.Manager
	Username string
	Password string
}

func New(ws *workspace.Manager) *Service {
	return &Service{WS: ws}
}

func (s *Service) open() (*git.Repository, error) {
	root, err := s.WS.ActiveRoot()
	if err != nil {
		return nil, err
	}
	repo, err := git.PlainOpen(root)
	if err != nil {
		return nil, fmt.Errorf("not a git repo (%s): %w", root, err)
	}
	return repo, nil
}

func (s *Service) Status() (string, error) {
	repo, err := s.open()
	if err != nil {
		return "", err
	}
	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	st, err := w.Status()
	if err != nil {
		return "", err
	}
	head, herr := repo.Head()
	var ref string
	if herr == nil {
		ref = head.Name().Short() + " " + head.Hash().String()[:7]
	}
	if st.IsClean() {
		return fmt.Sprintf("%s\nclean", ref), nil
	}
	return fmt.Sprintf("%s\n%s", ref, st.String()), nil
}

func (s *Service) Diff(path string, staged bool) (string, error) {
	_ = staged
	st, err := s.Status()
	if err != nil {
		return "", err
	}
	if path == "" {
		return st, nil
	}
	var b strings.Builder
	for _, line := range strings.Split(st, "\n") {
		if strings.Contains(line, path) || strings.HasPrefix(line, path) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return st, nil
	}
	return b.String(), nil
}

func (s *Service) Commit(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("empty commit message")
	}
	repo, err := s.open()
	if err != nil {
		return "", err
	}
	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	if err := w.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", err
	}
	hash, err := w.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "NotCursor",
			Email: "notcursor@local",
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", err
	}
	return hash.String(), nil
}

func (s *Service) Push(remote, branch string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	_ = branch
	repo, err := s.open()
	if err != nil {
		return "", err
	}
	opts := &git.PushOptions{RemoteName: remote}
	if s.Username != "" || s.Password != "" {
		user := s.Username
		if user == "" {
			user = "git"
		}
		opts.Auth = &http.BasicAuth{Username: user, Password: s.Password}
	}
	err = repo.Push(opts)
	if err == git.NoErrAlreadyUpToDate {
		return "already up-to-date", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pushed to %s", remote), nil
}
