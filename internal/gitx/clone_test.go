package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeSSHConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write ssh config: %v", err)
	}
	prev := sshConfigPath
	sshConfigPath = path
	t.Cleanup(func() { sshConfigPath = prev })
}

func TestParseRemote(t *testing.T) {
	ok := []struct {
		raw, kind, host, user string
		port                  int
		name                  string
	}{
		{"https://git.example.com/team/Pentest", "https", "git.example.com", "", 0, "Pentest"},
		{"https://git.example.com/team/Pentest.git", "https", "git.example.com", "", 0, "Pentest"},
		{"ssh://git@git.example.com:2222/team/Pentest.git", "ssh", "git.example.com", "git", 2222, "Pentest"},
		{"git@github.com:PTah/nc.ai.git", "ssh", "github.com", "git", 0, "nc.ai"},
		{"http://host/team/repo", "https", "host", "", 0, "repo"},
		{"file:///D:/repos/origin", "other", "", "", 0, "origin"},
		{`D:\repos\origin`, "other", "", "", 0, "origin"},
	}
	for _, c := range ok {
		info, err := parseRemote(c.raw)
		if err != nil {
			t.Errorf("parseRemote(%q): %v", c.raw, err)
			continue
		}
		if info.kind != c.kind || info.host != c.host || info.user != c.user || info.port != c.port || info.repoName() != c.name {
			t.Errorf("parseRemote(%q) = %+v (name %q), ожидали kind=%s host=%s user=%s port=%d name=%s",
				c.raw, info, info.repoName(), c.kind, c.host, c.user, c.port, c.name)
		}
	}
	for _, bad := range []string{"", "просто текст", "https://git.example.com/"} {
		if _, err := parseRemote(bad); err == nil {
			t.Errorf("parseRemote(%q) должен был вернуть ошибку", bad)
		}
	}
}

// Порт SSH для web-ссылки берём из ~/.ssh/config — иначе push/pull ушли бы на 22.
func TestRemoteDerivedURLs(t *testing.T) {
	writeSSHConfig(t, "Host git.example.com\n  HostName git.example.com\n  Port 2222\n  User git\n")

	web, err := parseRemote("https://git.example.com/team/Pentest")
	if err != nil {
		t.Fatalf("parse https: %v", err)
	}
	if got := web.sshURL(); got != "ssh://git@git.example.com:2222/team/Pentest.git" {
		t.Errorf("sshURL=%q", got)
	}
	if got := web.httpsURL(); got != "https://git.example.com/team/Pentest.git" {
		t.Errorf("httpsURL=%q", got)
	}

	ssh, err := parseRemote("ssh://git@git.example.com:2222/team/Pentest.git")
	if err != nil {
		t.Fatalf("parse ssh: %v", err)
	}
	if got := ssh.httpsURL(); got != "https://git.example.com/team/Pentest.git" {
		t.Errorf("httpsURL из ssh=%q", got)
	}
}

func TestCandidatesOrder(t *testing.T) {
	web, _ := parseRemote("https://git.example.com/team/Pentest")
	webFirst := web.candidates(false)
	if webFirst[0].kind != "https" || webFirst[1].kind != "ssh" {
		t.Fatalf("без рабочего SSH первым должен идти https: %+v", webFirst)
	}
	sshFirst := web.candidates(true)
	if sshFirst[0].kind != "ssh" || sshFirst[1].kind != "https" {
		t.Fatalf("с рабочим SSH первым должен идти ssh: %+v", sshFirst)
	}
	ssh, _ := parseRemote("git@github.com:PTah/nc.ai.git")
	got := ssh.candidates(false)
	if got[0].kind != "ssh" || got[1].kind != "https" {
		t.Fatalf("для ssh-ссылки порядок: %+v", got)
	}
	local, _ := parseRemote("file:///D:/repos/origin")
	if got := local.candidates(true); len(got) != 1 || got[0].kind != "other" {
		t.Fatalf("локальный путь — одна попытка: %+v", got)
	}
}

func TestLookupSSHConfig(t *testing.T) {
	writeSSHConfig(t, "# comment\nHost other\n  Port 1234\n\nHost git.example.com\n  Port 2222\n  User git\n")
	cfg, ok := lookupSSHConfig("git.example.com")
	if !ok || cfg.port != 2222 || cfg.user != "git" {
		t.Fatalf("cfg=%+v ok=%v", cfg, ok)
	}
	if _, ok := lookupSSHConfig("nope.example"); ok {
		t.Fatal("неизвестный хост не должен находиться")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// Полный цикл на локальном репозитории: без сети и без ssh/https.
func TestCloneLocalRepository(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	gitRun(t, origin, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitRun(t, origin, "add", ".")
	gitRun(t, origin, "commit", "-q", "-m", "init")

	parent := filepath.Join(base, "work")
	s := New(nil)
	res, err := s.Clone(context.Background(), origin, parent)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if res.Method != "other" || res.Path != filepath.Join(parent, "origin") {
		t.Fatalf("res=%+v", res)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "README.md")); err != nil {
		t.Fatalf("файл не склонировался: %v", err)
	}

	// Второй раз в ту же папку — понятная ошибка, а не тихая перезапись.
	if _, err := s.Clone(context.Background(), origin, parent); err == nil ||
		!strings.Contains(err.Error(), "уже существует") {
		t.Fatalf("ожидали ошибку про существующую папку, получили %v", err)
	}

	target, err := s.CloneTarget("https://git.example.com/team/Pentest", "D:/repos")
	if err != nil {
		t.Fatalf("CloneTarget: %v", err)
	}
	if filepath.Base(target) != "Pentest" {
		t.Fatalf("CloneTarget=%q", target)
	}
}

func TestCloneFailureText(t *testing.T) {
	remote, _ := parseRemote("https://git.example.com/team/Pentest")
	text := cloneFailureText(remote, []CloneAttempt{
		{Kind: "ssh", URL: "ssh://git@git.example.com:2222/team/Pentest.git", Err: "Permission denied (publickey)"},
		{Kind: "https", URL: "https://git.example.com/team/Pentest.git", Err: "Authentication failed"},
	})
	for _, want := range []string{"Pentest", "SSH", "HTTPS", "Permission denied", "Authentication failed", "SSH keys"} {
		if !strings.Contains(text, want) {
			t.Errorf("в тексте ошибки нет %q:\n%s", want, text)
		}
	}
}
