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
	states   map[string]string  // id → state text
	errors   map[string]string
}

func NewManager() *Manager {
	return &Manager{
		sessions: map[string]*Session{},
		states:   map[string]string{},
		errors:   map[string]string{},
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
		_, live := m.sessions[c.ID]
		m.mu.Unlock()
		if live {
			continue
		}
		_, _ = m.Start(ctx, c.ID)
	}
}

// Start connects one server by id.
func (m *Manager) Start(ctx context.Context, id string) (Status, error) {
	m.mu.Lock()
	cfg, ok := m.configByID(id)
	if !ok {
		m.mu.Unlock()
		return Status{}, fmt.Errorf("unknown MCP server %q", id)
	}
	if old := m.sessions[cfg.ID]; old != nil {
		old.Close()
		delete(m.sessions, cfg.ID)
	}
	m.states[cfg.ID] = "starting"
	m.errors[cfg.ID] = ""
	m.mu.Unlock()

	sess, err := StartStdio(ctx, cfg)
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
	allow := map[string]bool{}
	for _, t := range cfg.EnabledTools {
		allow[strings.TrimSpace(t)] = true
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
		if st.State != "error" {
			st.State = "running"
		}
	}
	_ = allow
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
		if s == nil {
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

// Call routes mcp:<server>:<tool> to the live session.
func (m *Manager) Call(ctx context.Context, prefixedName string, argsJSON string) (string, error) {
	serverID, toolName, ok := SplitPrefixed(prefixedName)
	if !ok {
		return "", fmt.Errorf("not an MCP tool: %s", prefixedName)
	}
	args := map[string]any{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid MCP args: %w", err)
		}
	}

	m.mu.Lock()
	cfg, has := m.configByID(serverID)
	s := m.sessions[serverID]
	m.mu.Unlock()
	if !has {
		return "", fmt.Errorf("unknown MCP server %q", serverID)
	}
	if !cfg.Enabled {
		return "", fmt.Errorf("MCP server %q is disabled", serverID)
	}
	allowed := false
	for _, t := range cfg.EnabledTools {
		if t == toolName {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("MCP tool %q is not enabled for server %q", toolName, serverID)
	}
	if s == nil {
		st, err := m.Start(ctx, serverID)
		if err != nil {
			return "", err
		}
		_ = st
		m.mu.Lock()
		s = m.sessions[serverID]
		m.mu.Unlock()
		if s == nil {
			return "", fmt.Errorf("MCP server %q failed to start", serverID)
		}
	}
	return s.CallTool(ctx, toolName, args)
}
