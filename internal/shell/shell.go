package shell

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	gopty "github.com/aymanbagabas/go-pty"
)

// Result is the output of a one-shot shell command.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

const psUTF8Prelude = "[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false; " +
	"[Console]::InputEncoding = New-Object System.Text.UTF8Encoding $false; " +
	"$OutputEncoding = [Console]::OutputEncoding; " +
	"chcp 65001 | Out-Null"

// Run executes a command in cwd with timeout (pipes; not a PTY).
// shellPath may be empty → auto-detect.
func Run(ctx context.Context, command, cwd, shellPath string, timeout time.Duration) (*Result, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shellPath = ResolveShell(shellPath)
	var cmd *exec.Cmd
	if IsPowerShell(shellPath) {
		script := psUTF8Prelude + "; " + command
		cmd = exec.CommandContext(ctx, shellPath, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	} else if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, shellPath, "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, shellPath, "-lc", command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	configureCmd(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return nil, fmt.Errorf("shell: %w", err)
		}
	}
	return &Result{
		Stdout:   decodeShellBytes(stdout.Bytes()),
		Stderr:   decodeShellBytes(stderr.Bytes()),
		ExitCode: code,
	}, nil
}

// Session is a long-lived interactive shell for the Xterm panel (real PTY/ConPTY).
type Session struct {
	mu      sync.Mutex
	pty     gopty.Pty
	cmd     *gopty.Cmd
	cwd     string
	shell   string
	cols    int
	rows    int
	onOut   func(string)
	started bool
}

func NewSession(cwd, shellPath string, onOut func(string)) *Session {
	return &Session{
		cwd:   cwd,
		shell: ResolveShell(shellPath),
		cols:  120,
		rows:  30,
		onOut: onOut,
	}
}

func (s *Session) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	p, err := gopty.New()
	if err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	_ = p.Resize(s.cols, s.rows)

	args := InteractiveArgs(s.shell)
	c := p.Command(s.shell, args...)
	c.Dir = s.cwd
	env := os.Environ()
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	c.Env = env

	if err := c.Start(); err != nil {
		_ = p.Close()
		return fmt.Errorf("start %s: %w", s.shell, err)
	}
	s.pty = p
	s.cmd = c
	s.started = true
	go s.readLoop(p)
	go s.waitCmd(c, p)
	return nil
}

func (s *Session) readLoop(p gopty.Pty) {
	reader := bufio.NewReader(p)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 && s.onOut != nil {
			s.onOut(decodeShellBytes(buf[:n]))
		}
		if err != nil {
			if s.onOut != nil {
				s.onOut("\r\n[shell exited]\r\n")
			}
			return
		}
	}
}

func (s *Session) waitCmd(c *gopty.Cmd, p gopty.Pty) {
	_ = c.Wait()
	s.mu.Lock()
	if s.pty == p {
		_ = p.Close()
		s.pty = nil
		s.cmd = nil
		s.started = false
	}
	s.mu.Unlock()
}

func (s *Session) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pty == nil {
		return fmt.Errorf("shell not started")
	}
	_, err := io.WriteString(s.pty, data)
	return err
}

func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cols < 20 {
		cols = 20
	}
	if rows < 5 {
		rows = 5
	}
	s.cols = cols
	s.rows = rows
	if s.pty == nil {
		return nil
	}
	return s.pty.Resize(cols, rows)
}

func (s *Session) SetCwd(cwd string) {
	s.mu.Lock()
	s.cwd = cwd
	s.mu.Unlock()
}

func (s *Session) ShellPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shell
}

func (s *Session) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if s.pty != nil {
		_ = s.pty.Close()
	}
	s.pty = nil
	s.cmd = nil
	s.started = false
}
