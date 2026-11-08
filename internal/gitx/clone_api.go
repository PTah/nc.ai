package gitx

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// githubAPIHost — хосты, для которых есть zipball API (быстрее битого git-pack).
func githubAPIHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "github.com", "www.github.com":
		return true
	default:
		return false
	}
}

// githubOwnerRepo — owner/repo из пути GitHub-ссылки.
func (i *remoteInfo) githubOwnerRepo() (owner, repo string, ok bool) {
	if i == nil || !githubAPIHost(i.host) {
		return "", "", false
	}
	p := strings.Trim(strings.TrimSuffix(i.path, ".git"), "/")
	parts := strings.Split(p, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (i *remoteInfo) githubZipballURL() string {
	owner, repo, ok := i.githubOwnerRepo()
	if !ok {
		return ""
	}
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/zipball", owner, repo)
}

// cloneGitHubAPI качает zipball через GitHub REST API и распаковывает в target.
// История git не приезжает — делаем `git init` + remote origin, чтобы дальше
// можно было fetch/pull обычным путём.
func (s *Service) cloneGitHubAPI(ctx context.Context, remote *remoteInfo, target string) error {
	apiURL := remote.githubZipballURL()
	if apiURL == "" {
		return fmt.Errorf("не GitHub — API zipball недоступен")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	tmpZip, err := os.CreateTemp("", "nc-gh-zipball-*.zip")
	if err != nil {
		return fmt.Errorf("временный файл: %w", err)
	}
	zipPath := tmpZip.Name()
	defer func() {
		_ = tmpZip.Close()
		_ = os.Remove(zipPath)
	}()

	if err := downloadGitHubZipball(ctx, apiURL, tmpZip); err != nil {
		return err
	}
	if err := tmpZip.Close(); err != nil {
		return err
	}

	staging, err := os.MkdirTemp(filepath.Dir(target), ".nc-clone-staging-*")
	if err != nil {
		return fmt.Errorf("временная папка: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	top, err := unzipFlat(zipPath, staging)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	_ = os.RemoveAll(target)
	if err := os.Rename(top, target); err != nil {
		// Rename через разные тома — копируем.
		if err := copyDir(top, target); err != nil {
			return fmt.Errorf("не удалось положить файлы в %s: %w", target, err)
		}
	}

	origin := remote.httpsURL()
	if origin == "" {
		origin = remote.raw
	}
	if err := initGitWithRemote(ctx, target, origin); err != nil {
		// Файлы уже на месте — remote не критичен для открытия проекта.
		return fmt.Errorf("архив скачан, но git init не удался: %w", err)
	}
	return nil
}

func downloadGitHubZipball(ctx context.Context, apiURL string, dest *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NotCursor.ai")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{
		// Без общего Timeout: отмена через ctx (CancelClone).
		Timeout: 0,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("слишком много редиректов")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("GitHub API %s: %s", resp.Status, firstLine(msg))
	}
	if _, err := io.Copy(dest, resp.Body); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("скачивание zipball: %w", err)
	}
	return nil
}

// unzipFlat распаковывает zip; GitHub кладёт всё в одну top-level папку —
// возвращаем путь к ней (или к dest, если корня нет).
func unzipFlat(zipPath, dest string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("открыть zip: %w", err)
	}
	defer r.Close()
	if len(r.File) == 0 {
		return "", fmt.Errorf("пустой архив")
	}

	var topName string
	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		if name == "." || strings.HasPrefix(name, "..") {
			continue
		}
		// Запрет path traversal.
		if strings.Contains(name, ".."+string(os.PathSeparator)) || strings.HasPrefix(name, "..") {
			return "", fmt.Errorf("подозрительный путь в архиве: %s", f.Name)
		}
		out := filepath.Join(dest, name)
		if !strings.HasPrefix(out, filepath.Clean(dest)+string(os.PathSeparator)) && out != filepath.Clean(dest) {
			return "", fmt.Errorf("подозрительный путь в архиве: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0o755); err != nil {
				return "", err
			}
			if topName == "" {
				topName = strings.Split(name, string(os.PathSeparator))[0]
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return "", err
		}
		if err := writeZipFile(f, out); err != nil {
			return "", err
		}
		if topName == "" {
			parts := strings.Split(name, string(os.PathSeparator))
			if len(parts) > 0 {
				topName = parts[0]
			}
		}
	}
	if topName == "" {
		return dest, nil
	}
	top := filepath.Join(dest, topName)
	if st, err := os.Stat(top); err == nil && st.IsDir() {
		return top, nil
	}
	return dest, nil
}

func writeZipFile(f *zip.File, out string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, rc)
	return err
}

func initGitWithRemote(ctx context.Context, dir, originURL string) error {
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		configureCmd(cmd)
		cmd.Env = gitEnv(dir)
		cmd.WaitDelay = 5 * time.Second
		out, err := cmd.CombinedOutput()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git %s: %s", strings.Join(args, " "), firstLine(msg))
		}
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// В архиве редко бывает .git; если вдруг есть — только remote.
		_ = run("remote", "remove", "origin")
		return run("remote", "add", "origin", originURL)
	}
	if err := run("init"); err != nil {
		return err
	}
	return run("remote", "add", "origin", originURL)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer w.Close()
		_, err = io.Copy(w, in)
		return err
	})
}
