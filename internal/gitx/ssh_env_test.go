package gitx

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	return ""
}

// Свой GIT_SSH_COMMAND пользователя не перетираем: иначе push в remote с
// конкретным ключом падает с «Permission denied (publickey)».
func TestGitEnvKeepsOwnSSHCommand(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "my-ssh -i ~/.ssh/special")
	got := envValue(gitEnv(t.TempDir()), "GIT_SSH_COMMAND")
	if got != "my-ssh -i ~/.ssh/special" {
		t.Fatalf("GIT_SSH_COMMAND=%q — чужую команду нельзя подменять", got)
	}
}

// Настроенный core.sshCommand дополняем BatchMode, а не выбрасываем.
func TestGitEnvAppendsBatchModeToConfiguredSSH(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := exec.Command("git", "init", repo).Run(); err != nil {
		t.Skipf("git недоступен: %v", err)
	}
	sshCmd := "C:/Windows/System32/OpenSSH/ssh.exe"
	if err := exec.Command("git", "-C", repo, "config", "core.sshCommand", sshCmd).Run(); err != nil {
		t.Fatalf("git config: %v", err)
	}
	got := envValue(gitEnv(repo), "GIT_SSH_COMMAND")
	if got != sshCmd+" -oBatchMode=yes" {
		t.Fatalf("GIT_SSH_COMMAND=%q, ожидали %q + BatchMode", got, sshCmd)
	}
}

func TestGitEnvNeverPrompts(t *testing.T) {
	env := gitEnv(t.TempDir())
	if envValue(env, "GIT_TERMINAL_PROMPT") != "0" {
		t.Error("GIT_TERMINAL_PROMPT не выставлен — git может спросить пароль")
	}
	if envValue(env, "GCM_INTERACTIVE") != "never" {
		t.Error("GCM_INTERACTIVE не выставлен — GCM может открыть своё окно")
	}
}
