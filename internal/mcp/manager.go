// Edited by nc.ai 1.2.13 on Papasha 2026-11-14 22:55:00
// Edited by nc.ai 0.8.5.1 on new-admin-pc 2026-10-12 17:49:52
// Edited by nc.ai 0.8.6.0 on macbook 2026-10-13 22:31:30
// Edited by nc.ai 1.2.12 on Papasha 2026-11-14 21:56:49
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"notcursor.ai/app/internal/llm"
)

// Backoff для серверов, которые падают сразу после запуска. Авто-рестарт
// (EnsureStarted / EnsureStartedBackground / Call) ждёт после каждой неудачи всё
// дольше, а после mcpMaxAutoRestarts провалов подряд сдаётся совсем: дальше
// сервер поднимает только человек кнопкой «Start» (этот путь порог не уважает и
// сбрасывает счётчик). Без этого упавший на старте сервер перезапускался на
// каждом шаге агента — отсюда и «процессов не прибавилось» в логах, и вечный
// спавн-убийство node.exe.
const (
	mcpMinRestartInterval = 30 * time.Second
	mcpMaxRestartInterval = 10 * time.Minute
	mcpMaxAutoRestarts    = 5
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
	sessions map[string]liveSession // id → live (stdio-процесс или HTTP-эндпоинт)
	creds    CredentialSource       // OAuth-токены hosted-серверов (nil = только статические заголовки)
	states   map[string]string      // id → state text
	errors   map[string]string
	// lastFailed / failures track the last failed Start and the number of
	// consecutive failures for a server. The auto-restart paths (EnsureStarted /
	// EnsureStartedBackground / Call) use them to back off instead of hammering a
	// crash-looping server on every agent step.
	lastFailed map[string]time.Time
	failures   map[string]int
	// lifetime is the base context for MCP child processes. It must NOT be a
	// per-run/per-call context: otherwise Stop (or any run cancellation) kills
	// the server while the manager still thinks it is running.
	lifetime       context.Context
	lifetimeCancel context.CancelFunc
	// startMu serializes Start so the background starter and lazy tool-call
	// start cannot race and kill each other's session.
	startMu sync.Mutex
	// OnEvent, если задан, вызывается для журнала: "started id=… tools=N",
	// "start failed id=… err=…", "stopped id=…". Нужно, чтобы в agent.log было
	// видно, когда и почему MCP-сервер поднялся или упал.
	OnEvent func(msg string)
}

func (m *Manager) notify(msg string) {
	if m.OnEvent != nil {
		m.OnEvent(msg)
	}
}

// HasEnabled сообщает, есть ли хоть один включённый сервер с непустым
// allowlist инструментов (когда надо писать подсказку/заметку при 0 tool(s)).
func (m *Manager) HasEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.configs {
		if c.Enabled && len(c.EnabledTools) > 0 {
			return true
		}
	}
	return false
}

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		sessions:       map[string]liveSession{},
		states:         map[string]string{},
		errors:         map[string]string{},
		lastFailed:     map[string]time.Time{},
		failures:       map[string]int{},
		lifetime:       ctx,
		lifetimeCancel: cancel,
	}
}

// SetCredentialSource задаёт хранилище OAuth-токенов для hosted-серверов.
// Вызывается один раз при старте приложения, до первого Start.
func (m *Manager) SetCredentialSource(src CredentialSource) {
	m.mu.Lock()
	m.creds = src
	m.mu.Unlock()
}

// SetConfigs replaces the config list (does not auto-start). A server whose
// command/args/url changed gets its restart backoff cleared: the user fixed the
// config, so the next attempt must be immediate instead of waiting for the old
// crash-loop penalty.
func (m *Manager) SetConfigs(cfgs []ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := make(map[string]ServerConfig, len(m.configs))
	for _, c := range m.configs {
		prev[c.ID] = c
	}
	cp := make([]ServerConfig, len(cfgs))
	for i := range cfgs {
		c := cfgs[i]
		c.Normalize()
		cp[i] = c
		if old, ok := prev[c.ID]; ok && sameLaunch(old, c) {
			continue
		}
		delete(m.lastFailed, c.ID)
		delete(m.failures, c.ID)
	}
	m.configs = cp
}

// sameLaunch reports whether two configs describe the same process to spawn or
// the same HTTP endpoint (label/tool allowlist changes do not count).
func sameLaunch(a, b ServerConfig) bool {
	if a.Transport != b.Transport || a.Command != b.Command || a.URL != b.URL || a.CWD != b.CWD {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	return true
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
		if ok, _ := m.restartAllowed(c.ID); !ok {
			continue // crash-looping server: back off, retry later
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
		if ok, _ := m.restartDecisionLocked(c.ID); !ok {
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

// restartAllowed reports whether an automatic restart of id is currently
// allowed, plus the reason when it is not (shown to the user / written to the
// agent log).
func (m *Manager) restartAllowed(id string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restartDecisionLocked(id)
}

// restartDecisionLocked is restartAllowed for callers that already hold m.mu
// (sync.Mutex is not reentrant, so the locked variant must not be nested twice).
func (m *Manager) restartDecisionLocked(id string) (bool, string) {
	t, failed := m.lastFailed[id]
	if !failed {
		return true, ""
	}
	if n := m.failures[id]; n >= mcpMaxAutoRestarts {
		return false, fmt.Sprintf(
			"MCP server %q падал %d раз подряд — авто-перезапуск выключен, запустите его вручную (Settings → MCP)",
			id, n)
	}
	wait := m.restartDelayLocked(id)
	if elapsed := time.Since(t); elapsed < wait {
		return false, fmt.Sprintf("MCP server %q перезапускался недавно и упал — следующая попытка через %s",
			id, roundDuration(wait-elapsed))
	}
	return true, ""
}

// restartDelayLocked is the backoff before the next automatic attempt:
// 30s, 1m, 2m, 4m, capped at mcpMaxRestartInterval.
func (m *Manager) restartDelayLocked(id string) time.Duration {
	n := m.failures[id]
	if n <= 0 {
		return 0
	}
	d := mcpMinRestartInterval
	for i := 1; i < n; i++ {
		if d >= mcpMaxRestartInterval {
			return mcpMaxRestartInterval
		}
		d *= 2
	}
	if d > mcpMaxRestartInterval {
		d = mcpMaxRestartInterval
	}
	return d
}

// ResetBackoff forgets the failure history of one server. Called when a human
// explicitly starts the server from the UI: their decision beats our backoff.
func (m *Manager) ResetBackoff(id string) {
	id = sanitizeID(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.lastFailed, id)
	delete(m.failures, id)
}

// roundDuration trims a wait to whole seconds (30s, 1m15s) for user-facing text.
func roundDuration(d time.Duration) string {
	if d < time.Second {
		return "1s"
	}
	return d.Round(time.Second).String()
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

	var (
		sess liveSession
		err  error
	)
	switch cfg.Transport {
	case "http":
		sess, err = StartHTTP(m.lifetime, cfg, m.creds)
	default:
		sess, err = StartStdio(m.lifetime, cfg)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.states[cfg.ID] = "error"
		m.errors[cfg.ID] = err.Error()
		m.failures[cfg.ID]++
		m.lastFailed[cfg.ID] = time.Now()
		if m.failures[cfg.ID] >= mcpMaxAutoRestarts {
			m.notify(fmt.Sprintf("start failed id=%s failures=%d auto_restart=disabled err=%s",
				cfg.ID, m.failures[cfg.ID], err))
		} else {
			m.notify(fmt.Sprintf("start failed id=%s failures=%d next_auto_restart_in=%s err=%s",
				cfg.ID, m.failures[cfg.ID], m.restartDelayLocked(cfg.ID), err))
		}
		return m.statusLocked(cfg), err
	}
	m.sessions[cfg.ID] = sess
	m.states[cfg.ID] = "running"
	m.errors[cfg.ID] = ""
	delete(m.lastFailed, cfg.ID)
	delete(m.failures, cfg.ID)
	m.notify(fmt.Sprintf("started id=%s tools=%d", cfg.ID, len(sess.Tools())))
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
	m.notify("stopped id=" + id)
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
					Name:        PrefixedName(cfg.ID, t.Name),
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
		if ok, reason := m.restartAllowed(serverID); !ok {
			return "", fmt.Errorf("%s", reason)
		}
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
