// Edited by nc.ai 0.8.5.1 on new-admin-pc 2026-10-12 17:50:14
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Session is a live stdio MCP connection to one server process.
type Session struct {
	cfg ServerConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	mu      sync.Mutex
	pending map[uint64]chan jsonRPCResponse
	nextID  atomic.Uint64
	closed  atomic.Bool

	logMu sync.Mutex
	logs  []string

	tools []mcpTool
}

// StartStdio launches command and performs initialize + tools/list.
func StartStdio(ctx context.Context, cfg ServerConfig) (*Session, error) {
	cfg.Normalize()
	if cfg.Transport != "stdio" {
		return nil, fmt.Errorf("transport %q not supported in MVP (use stdio)", cfg.Transport)
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp %s: empty command", cfg.ID)
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if cfg.CWD != "" {
		cmd.Dir = cfg.CWD
	}
	cmd.Env = filteredEnv(cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	s := &Session{
		cfg:     cfg,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdoutPipe),
		stderr:  stderrPipe,
		pending: map[uint64]chan jsonRPCResponse{},
	}
	go s.readLoop()
	go s.drainStderr()

	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.initialize(initCtx); err != nil {
		s.Close()
		return nil, err
	}
	listCtx, cancelList := context.WithTimeout(ctx, 10*time.Second)
	defer cancelList()
	if err := s.refreshTools(listCtx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func filteredEnv(extra map[string]string) []string {
	base := []string{}
	allow := map[string]bool{
		"PATH": true, "PATHEXT": true, "SystemRoot": true, "TEMP": true, "TMP": true,
		"HOME": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true,
		"LANG": true, "LC_ALL": true, "ComSpec": true, "OS": true,
		"NUMBER_OF_PROCESSORS": true, "PROCESSOR_ARCHITECTURE": true,
	}
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		k := kv[:i]
		ku := strings.ToUpper(k)
		if allow[k] || allow[ku] {
			base = append(base, kv)
			continue
		}
		// Never leak our provider keys into MCP children.
		if isSecretEnvKey(ku) {
			continue
		}
	}
	for k, v := range extra {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		base = append(base, k+"="+v)
	}
	return base
}

// isSecretEnvKey reports whether an env var name looks like one of our provider
// credentials and must never be handed to child processes.
func isSecretEnvKey(ku string) bool {
	return strings.Contains(ku, "API_KEY") || strings.Contains(ku, "APIKEY") ||
		strings.Contains(ku, "SECRET") || strings.Contains(ku, "TOKEN") ||
		strings.HasPrefix(ku, "DEEPSEEK") || strings.HasPrefix(ku, "OPENROUTER") ||
		strings.HasPrefix(ku, "ZAI") || strings.HasPrefix(ku, "QWEN") ||
		strings.HasPrefix(ku, "YANDEX")
}

func (s *Session) initialize(ctx context.Context) error {
	params := initializeParams{
		ProtocolVersion: protocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo: map[string]any{
			"name":    "NotCursor.ai",
			"version": "mcp-stdio",
		},
	}
	raw, err := s.call(ctx, "initialize", params)
	if err != nil {
		return err
	}
	var res initializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("initialize decode: %w", err)
	}
	// notifications/initialized (no id)
	_ = s.notify("notifications/initialized", map[string]any{})
	return nil
}

func (s *Session) refreshTools(ctx context.Context) error {
	raw, err := s.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return err
	}
	var res toolListResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("tools/list decode: %w", err)
	}
	s.mu.Lock()
	s.tools = res.Tools
	s.mu.Unlock()
	return nil
}

// Tools returns a copy of discovered tools.
func (s *Session) Tools() []mcpTool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]mcpTool, len(s.tools))
	copy(out, s.tools)
	return out
}

// CallTool invokes tools/call and returns text (joined content).
func (s *Session) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	timeout := time.Duration(s.cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := s.call(cctx, "tools/call", callToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	var res callToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("tools/call decode: %w", err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" || c.Type == "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(c.Text)
		}
	}
	out := b.String()
	if res.IsError {
		if out == "" {
			out = "MCP tool error"
		}
		return out, fmt.Errorf("%s", out)
	}
	if out == "" {
		out = "(empty MCP result)"
	}
	return out, nil
}

func (s *Session) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if s.closed.Load() {
		return nil, fmt.Errorf("mcp session closed")
	}
	id := s.nextID.Add(1)
	ch := make(chan jsonRPCResponse, 1)
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	line = append(line, '\n')
	s.mu.Lock()
	_, err = s.stdin.Write(line)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (s *Session) notify(method string, params any) error {
	req := jsonRPCRequest{JSONRPC: "2.0", Method: method, Params: params}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.stdin.Write(line)
	return err
}

func (s *Session) readLoop() {
	for {
		line, err := s.stdout.ReadBytes('\n')
		if err != nil {
			if !s.closed.Load() {
				s.appendLog("stdout closed: " + err.Error())
			}
			s.failPending(err)
			return
		}
		line = bytesTrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			s.appendLog("bad json: " + string(line))
			continue
		}
		if resp.Method != "" && resp.ID == nil {
			// notification — ignore in MVP
			continue
		}
		id, ok := asUint64(resp.ID)
		if !ok {
			continue
		}
		s.mu.Lock()
		ch := s.pending[id]
		s.mu.Unlock()
		if ch != nil {
			select {
			case ch <- resp:
			default:
			}
		}
	}
}

func (s *Session) drainStderr() {
	if s.stderr == nil {
		return
	}
	sc := bufio.NewScanner(s.stderr)
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for sc.Scan() {
		s.appendLog(sc.Text())
	}
}

func (s *Session) appendLog(line string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if len(s.logs) > 200 {
		s.logs = s.logs[len(s.logs)-100:]
	}
	s.logs = append(s.logs, line)
}

// Logs returns recent stderr lines.
func (s *Session) Logs() []string {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	out := make([]string, len(s.logs))
	copy(out, s.logs)
	return out
}

func (s *Session) failPending(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ch := range s.pending {
		select {
		case ch <- jsonRPCResponse{Error: &rpcError{Code: -32000, Message: err.Error()}}:
		default:
		}
		delete(s.pending, id)
	}
}

// Close kills the server process.
func (s *Session) Close() {
	if s == nil || !s.closed.CompareAndSwap(false, true) {
		return
	}
	_ = s.stdin.Close()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
}

func asUint64(id any) (uint64, bool) {
	switch v := id.(type) {
	case float64:
		return uint64(v), true
	case int:
		return uint64(v), true
	case int64:
		return uint64(v), true
	case uint64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return uint64(n), err == nil
	default:
		return 0, false
	}
}

func bytesTrimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 {
		c := b[len(b)-1]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			b = b[:len(b)-1]
			continue
		}
		break
	}
	return b
}
