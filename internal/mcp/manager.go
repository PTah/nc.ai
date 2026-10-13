// Edited by nc.ai 0.8.5.1 on new-admin-pc 2026-10-12 17:49:52
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"notcursor.ai/app/internal/llm"
)

// Status of one configured server from the UI's point of view.
type Status struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Enabled      bool     `json:"enabled"`
	Transport    string   `json:"transport"`
	State        string   `json:"state"` // stopped | starting | running | error
	Error        string   `json:"error,omitempty"`
	ToolCount    int      `json:"toolCount"`
	EnabledCount int      `json:"enabledCount"`
	Tools        []string `json:"tools,omitempty"` // discovered local names
	Logs         []string `json:"logs,omitempty"`
}

// Manager owns configured MCP servers and live stdio sessions.
type Manager struct {
	mu       sync.Mutex
	configs  []ServerConfig
	sessions map[string]*Session // id → live
	states   map[string]string   // id → state text
	errors   map[string]string
	// lifetime is the base context for MCP child processes. It must NOT be a
	// per-run/per-call context: otherwise Stop (or any run cancellation) kills
	// the server while the manager still thinks it is running.
	lifetime       context.Context
	lifetimeCancel context.CancelFunc
	// startMu serializes Start so the background starter and lazy tool-call
	// start cannot race and kill each other's session.
	startMu sync.Mutex
}

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		sessions:       map[string]*Session{},
		states:         map[string]string{},
		errors:         map[string]string{},
		lifetime:       ctx,
		lifetimeCancel: cancel,
	}
}

// SetConfigs replaces the config list (does not auto-start).
func (m *Manager) SetConfigs(cfgs []ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]ServerConfig, len(cfgs))
	for i := range cfgs {
		c := cfgs[i]
		c.Normalize()
		cp[i] = c
	}
	m.configs = cp
}

// Configs returns a copy of current configs.
func (m *Manager) Configs() []ServerConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerConfig, len(m.configs))
	copy(out, m.configs)
	return out
}

func (m *Manager) configByID(id string) (ServerConfig, bool) {
	id = sanitizeID(id)
	for _, c := range m.configs {
		if c.ID == id {
			return c, true
		}
	}
	return ServerConfig{}, false
}

// EnsureStarted starts enabled servers that are not running yet.
func (m *Manager) EnsureStarted(ctx context.Context) {
	m.mu.Lock()
	cfgs := append([]ServerConfig(nil), m.configs...)
	m.mu.Unlock()
	for _, c := range cfgs {
		if !c.Enabled {
			continue
		}
		m.mu.Lock()
		s := m.sessions[c.ID]
		m.mu.Unlock()
		if s != nil && s.Alive() {
			continue
		}
		_, _ = m.Start(ctx, c.ID)
	}
}

// EnsureStartedBackground starts missing servers without blocking the caller.
// A slow or failing server (cold Node startup, npx download) must not stall the
// agent run; ToolSpecs pick the tools up on the next step once a session is up.
func (m *Manager) EnsureStartedBackground() {
	m.mu.Lock()
	var todo []string
	for _, c := range m.configs {
		if !c.Enabled {
			continue
		}
		if s := m.sessions[c.ID]; s != nil && s.Alive() {
			continue
		}
		todo = append(todo, c.ID)
	}
	m.mu.Unlock()
	for _, id := range todo {
		go func(id string) { _, _ = m.Start(m.lifetime, id) }(id)
	}
}

// RunningIDs returns ids of servers with a live session.
func (m *Manager) RunningIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.sessions))
	for id, s := range m.sessions {
		if s.Alive() {
			out = append(out, id)
		}
	}
	return out
}

// Start connects one server by id. If a healthy session already exists it is
// left as-is (no needless restart); a dead one is replaced.
func (m *Manager) Start(ctx context.Context, id string) (Status, error) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.mu.Lock()
	cfg, ok := m.configByID(id)
	if !ok {
		m.mu.Unlock()
		return Status{}, fmt.Errorf("unknown MCP server %q", id)
	}
	if s := m.sessions[cfg.ID]; s != nil && s.Alive() {
		st := m.statusLocked(cfg)
		m.mu.Unlock()
		return st, nil
	}
	if old := m.sessions[cfg.ID]; old != nil {
		old.Close()
		delete(m.sessions, cfg.ID)
	}
	m.states[cfg.ID] = "starting"
	m.errors[cfg.ID] = ""
	m.mu.Unlock()

	sess, err := StartStdio(m.lifetime, cfg)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.states[cfg.ID] = "error"
		m.errors[cfg.ID] = err.Error()
		return m.statusLocked(cfg), err
	}
	m.sessions[cfg.ID] = sess
	m.states[cfg.ID] = "running"
	m.errors[cfg.ID] = ""
	return m.statusLocked(cfg), nil
}

// Stop disconnects one server.
func (m *Manager) Stop(id string) Status {
	id = sanitizeID(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil {
		s.Close()
		delete(m.sessions, id)
	}
	m.states[id] = "stopped"
	cfg, _ := m.configByID(id)
	return m.statusLocked(cfg)
}

// CloseAll stops every session (app shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.Close()
		delete(m.sessions, id)
		m.states[id] = "stopped"
	}
	if m.lifetimeCancel != nil {
		m.lifetimeCancel()
	}
}

// Statuses returns UI status for each configured server.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.configs))
	for _, c := range m.configs {
		out = append(out, m.statusLocked(c))
	}
	return out
}

func (m *Manager) statusLocked(cfg ServerConfig) Status {
	st := Status{
		ID:        cfg.ID,
		Label:     cfg.Label,
		Enabled:   cfg.Enabled,
		Transport: cfg.Transport,
		State:     m.states[cfg.ID],
		Error:     m.errors[cfg.ID],
	}
	if st.State == "" {
		st.State = "stopped"
	}
	st.EnabledCount = len(cfg.EnabledTools)
	if s := m.sessions[cfg.ID]; s != nil {
		tools := s.Tools()
		st.ToolCount = len(tools)
		st.Tools = make([]string, 0, len(tools))
		for _, t := range tools {
			st.Tools = append(st.Tools, t.Name)
		}
		st.Logs = s.Logs()
		if !s.Alive() {
			st.State = "error"
			if st.Error == "" {
				st.Error = "процесс MCP-сервера завершился — нажмите Start, чтобы перезапустить"
			}
		} else if st.State != "error" {
			st.State = "running"
		}
	}
	return st
}

// ToolSpecs returns llm tool specs for enabled+allowlisted MCP tools.
func (m *Manager) ToolSpecs() []llm.ToolSpec {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []llm.ToolSpec
	for _, cfg := range m.configs {
		if !cfg.Enabled {
			continue
		}
		s := m.sessions[cfg.ID]
		if s == nil || !s.Alive() {
			continue
		}
		allow := map[string]bool{}
		for _, t := range cfg.EnabledTools {
			allow[strings.TrimSpace(t)] = true
		}
		if len(allow) == 0 {
			continue // safe default: nothing in the prompt
		}
		for _, t := range s.Tools() {
			if !allow[t.Name] {
				continue
			}
			params := t.InputSchema
			if params == nil {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			out = append(out, llm.ToolSpec{
				Type: "function",
				Function: llm.ToolSpecFunction{
					Name: PrefixedName(cfg.ID, t.Name),
					Description: fmt.Sprintf("[MCP · %s] %s", cfg.Label, strings.TrimSpace(t.Description)),
					Parameters:  params,
				},
			})
		}
	}
	return out
}

// Call routes a provider-safe tool name (mcp__<server>__<tool>) to the live
// session that owns it. The real tool name is resolved from the enabled
// allowlist, so sanitized names still reach the server correctly.
func (m *Manager) Call(ctx context.Context, prefixedName string, argsJSON string) (string, error) {
	prefixedName = strings.TrimSpace(prefixedName)
	if !strings.HasPrefix(prefixedName, NamePrefix) {
		return "", fmt.Errorf("not an MCP tool: %s", prefixedName)
	}
	args := map[string]any{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid MCP args: %w", err)
		}
	}

	m.mu.Lock()
	var (
		serverID string
		toolName string
		found    bool
	)
	for _, c := range m.configs {
		if !c.Enabled {
			continue
		}
		for _, t := range c.EnabledTools {
			if PrefixedName(c.ID, t) == prefixedName {
				serverID, toolName, found = c.ID, t, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		m.mu.Unlock()
		return "", fmt.Errorf("MCP tool %q is not enabled for any server", prefixedName)
	}
	s := m.sessions[serverID]
	m.mu.Unlock()
	if s == nil || !s.Alive() {
		if _, err := m.Start(ctx, serverID); err != nil {
			return "", err
		}
		m.mu.Lock()
		s = m.sessions[serverID]
		m.mu.Unlock()
		if s == nil {
			return "", fmt.Errorf("MCP server %q failed to start", serverID)
		}
	}
	return s.CallTool(ctx, toolName, args)
}

// NotReadyExplanation recognises temporary "not ready yet" answers from MCP
// servers (no index / graph / branch built yet). Such answers are states, not
// failures: the agent should build the index and retry, and the user should see
// an informational note instead of a red error. Returns ok=false for real errors.
func NotReadyExplanation(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	l := strings.ToLower(err.Error())
	for _, p := range []string{
		"not indexed", "not found or not indexed", "no index", "index not",
		"not built", "must be indexed", "call index_repository", "index_repository first",
		"not initialized", "no graph", "graph not", "not ready", "not available yet",
	} {
		if strings.Contains(l, p) {
			return "ℹ️ Это не сбой, а временное состояние: у MCP-сервера ещё не готовы данные " +
				"(индекс / граф / ветки) для этого проекта. Сначала построй индекс " +
				"(инструмент index_repository), затем повтори запрос. " +
				"Ответ сервера: " + err.Error(), true
		}
	}
	return "", false
}
