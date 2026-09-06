package shell

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Result is the output of a one-shot shell command.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// Run executes a command in cwd with timeout.
func Run(ctx context.Context, command, cwd string, timeout time.Duration) (*Result, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
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
	return &Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}, nil
}

// Session is a long-lived interactive shell for the Xterm panel.
type Session struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cwd    string
	onOut  func(string)
}

func NewSession(cwd string, onOut func(string)) *Session {
	return &Session{cwd: cwd, onOut: onOut}
}

func (s *Session) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return nil
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoExit", "-NoProfile")
	} else {
		cmd = exec.Command("bash", "-i")
	}
	cmd.Dir = s.cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd = cmd
	s.stdin = stdin
	s.stdout = stdout
	go s.readLoop()
	return nil
}

func (s *Session) readLoop() {
	reader := bufio.NewReader(s.stdout)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 && s.onOut != nil {
			s.onOut(string(buf[:n]))
		}
		if err != nil {
			if s.onOut != nil {
				s.onOut("\r\n[shell exited]\r\n")
			}
			return
		}
	}
}

func (s *Session) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin == nil {
		return fmt.Errorf("shell not started")
	}
	_, err := io.WriteString(s.stdin, data)
	return err
}

func (s *Session) SetCwd(cwd string) {
	s.mu.Lock()
	s.cwd = cwd
	s.mu.Unlock()
}

func (s *Session) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.cmd = nil
	s.stdin = nil
	s.stdout = nil
}
