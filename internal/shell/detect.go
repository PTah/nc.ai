package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DetectDefaultShell finds the preferred interactive shell for this OS.
// Windows: PowerShell 7 (pwsh) if installed, else Windows PowerShell 5.
// macOS/Linux: $SHELL, else /bin/zsh (darwin) or /bin/bash.
func DetectDefaultShell() string {
	if runtime.GOOS == "windows" {
		return detectWindowsShell()
	}
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		if st, err := os.Stat("/bin/zsh"); err == nil && !st.IsDir() {
			return "/bin/zsh"
		}
	}
	if st, err := os.Stat("/bin/bash"); err == nil && !st.IsDir() {
		return "/bin/bash"
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}

func detectWindowsShell() string {
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p
	}
	for _, base := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), "PowerShell", "7", "pwsh.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "PowerShell", "7-preview", "pwsh.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "PowerShell", "7", "pwsh.exe"),
	} {
		if !filepath.IsAbs(base) {
			continue
		}
		if st, err := os.Stat(base); err == nil && !st.IsDir() {
			return base
		}
	}
	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	ps5 := filepath.Join(sys, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if st, err := os.Stat(ps5); err == nil && !st.IsDir() {
		return ps5
	}
	if p, err := exec.LookPath("powershell"); err == nil {
		return p
	}
	return "powershell.exe"
}

// InteractiveArgs returns argv (without the executable) for an interactive session.
func InteractiveArgs(shellPath string) []string {
	base := strings.ToLower(filepath.Base(shellPath))
	base = strings.TrimSuffix(base, ".exe")
	switch {
	case base == "pwsh" || base == "powershell":
		return []string{"-NoLogo"}
	case base == "cmd":
		return nil
	default:
		// bash/zsh/fish: interactive login (macOS Terminal-like)
		return []string{"-il"}
	}
}

// IsPowerShell reports whether path looks like Windows PowerShell / pwsh.
func IsPowerShell(shellPath string) bool {
	base := strings.ToLower(filepath.Base(shellPath))
	base = strings.TrimSuffix(base, ".exe")
	return base == "pwsh" || base == "powershell"
}

// ResolveShell returns configured path, or DetectDefaultShell when empty/"auto".
func ResolveShell(configured string) string {
	c := strings.TrimSpace(configured)
	if c == "" || strings.EqualFold(c, "auto") {
		return DetectDefaultShell()
	}
	return c
}
