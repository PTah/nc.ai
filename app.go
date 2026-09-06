package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"notcursor.ai/app/internal/agent"
	"notcursor.ai/app/internal/chatstore"
	"notcursor.ai/app/internal/config"
	"notcursor.ai/app/internal/costing"
	"notcursor.ai/app/internal/dockicon"
	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/llm/providers/deepseek"
	"notcursor.ai/app/internal/shell"
	"notcursor.ai/app/internal/sshx"
	"notcursor.ai/app/internal/tools"
	"notcursor.ai/app/internal/workspace"
)

// App is the Wails-bound application façade.
type App struct {
	ctx context.Context

	cfg   *config.Store
	ws    *workspace.Manager
	tools *tools.Registry
	llm   llm.Provider
	chats *chatstore.Store

	mu        sync.Mutex
	cancels   map[string]context.CancelFunc
	sessionID string
	history   []llm.Message
	term      *shell.Session
	sshDir    string
}

func NewApp() *App {
	return &App{
		cfg:     config.NewStore(),
		ws:      workspace.NewManager(),
		history: []llm.Message{},
		cancels: map[string]context.CancelFunc{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.cfg.Load()
	sshDir, err := a.cfg.SSHDir()
	if err == nil {
		a.sshDir = sshDir
	}
	if base, err := a.cfg.AppDataDir(); err == nil {
		a.chats, _ = chatstore.New(base)
	}
	a.tools = tools.NewRegistry(a.ws, a.sshDir)
	a.applyGitAuth()
	a.refreshProvider()

	for _, p := range a.cfg.Get().RecentProjects {
		_, _ = a.ws.Open(p)
	}
}

func (a *App) applyGitAuth() {
	if a.tools == nil || a.tools.Git == nil {
		return
	}
	s := a.cfg.Get()
	a.tools.Git.Username = s.GitUsername
	a.tools.Git.Password = s.GitPassword
}

func (a *App) refreshProvider() {
	key := a.cfg.Get().DeepSeekAPIKey
	model := a.cfg.Get().DeepSeekModel
	if model == "" {
		model = deepseek.DefaultModel
	}
	a.llm = deepseek.New(key, model)
}

func (a *App) emit(evt agent.Event) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "agent:event", evt)
}

func (a *App) emitFor(sessionID string, evt agent.Event) {
	evt.SessionID = sessionID
	a.emit(evt)
}

func (a *App) emitTerm(data string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "terminal:data", data)
}

// --- meta / settings ---

func (a *App) AppInfo() map[string]string {
	return map[string]string{
		"name":    "NotCursor.ai",
		"version": "0.1.6",
		"stage":   "1-deepseek-agent",
	}
}

func (a *App) GetSettings() map[string]any {
	s := a.cfg.Get()
	return map[string]any{
		"deepseekModel":  s.DeepSeekModel,
		"deepseekKeySet": s.DeepSeekAPIKey != "",
		"shell":          s.Shell,
		"recentProjects": s.RecentProjects,
		"gitUsername":    s.GitUsername,
		"gitPasswordSet": s.GitPassword != "",
		"showTerminal":   s.ShowTerminal,
		"showFiles":      a.cfg.FilesVisible(),
		"visionModel":    deepseek.VisionModel,
	}
}

// GetUsageStats returns the all-time API spend counters shown in the top bar.
func (a *App) GetUsageStats() map[string]any {
	s := a.cfg.Get()
	return map[string]any{
		"costUsd":      s.TotalCostUSD,
		"inputTokens":  s.TotalInputTokens,
		"outputTokens": s.TotalOutputTokens,
	}
}

func (a *App) SaveShowTerminal(show bool) error {
	if err := a.cfg.SetShowTerminal(show); err != nil {
		return err
	}
	if !show {
		a.StopTerminal()
	}
	return nil
}

func (a *App) SaveShowFiles(show bool) error {
	return a.cfg.SetShowFiles(show)
}

func (a *App) SaveDeepSeekKey(apiKey string) error {
	if err := a.cfg.SetDeepSeekAPIKey(apiKey); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

func (a *App) SaveDeepSeekModel(model string) error {
	if err := a.cfg.SetDeepSeekModel(model); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

func (a *App) SaveGitAuth(username, password string) error {
	if err := a.cfg.SetGitAuth(username, password); err != nil {
		return err
	}
	a.applyGitAuth()
	return nil
}

// --- workspace ---

func (a *App) ListProjects() []workspace.Project {
	return a.ws.List()
}

func (a *App) PickProjectDir() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open Project",
	})
}

func (a *App) OpenProject(path string) (*workspace.Project, error) {
	p, err := a.ws.Open(path)
	if err != nil {
		return nil, err
	}
	_ = a.cfg.AddRecentProject(p.Path)
	return p, nil
}

func (a *App) ListDir(rel string) ([]workspace.Entry, error) {
	return a.ws.ListDir(rel)
}

func (a *App) ReadFile(rel string) (string, error) {
	return a.ws.ReadFile(rel)
}

func (a *App) WriteFile(rel, content string) error {
	return a.ws.WriteFile(rel, content)
}

// --- chat sessions (multitasking) ---

func (a *App) projectKey() string {
	root, err := a.ws.ActiveRoot()
	if err != nil {
		return ""
	}
	return root
}

func (a *App) loadActiveIntoMemory() error {
	if a.chats == nil {
		a.mu.Lock()
		a.history = nil
		a.sessionID = ""
		a.mu.Unlock()
		return nil
	}
	b, err := a.chats.List(a.projectKey())
	if err != nil {
		return err
	}
	sess, err := a.chats.Get(a.projectKey(), b.ActiveID)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.sessionID = sess.ID
	a.history = append([]llm.Message{}, sess.History...)
	a.mu.Unlock()
	return nil
}

// ListChatSessions returns all chat tabs for the active project.
func (a *App) ListChatSessions() (*chatstore.ProjectBundle, error) {
	if a.chats == nil {
		return &chatstore.ProjectBundle{Sessions: nil}, nil
	}
	b, err := a.chats.List(a.projectKey())
	if err != nil {
		return nil, err
	}
	_ = a.loadActiveIntoMemory()
	return b, nil
}

// NewChatSession creates a new empty chat tab and makes it active.
func (a *App) NewChatSession(title string) (*chatstore.Session, error) {
	if a.chats == nil {
		return nil, fmt.Errorf("chat store unavailable")
	}
	sess, err := a.chats.NewSession(a.projectKey(), title)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.sessionID = sess.ID
	a.history = nil
	a.mu.Unlock()
	return sess, nil
}

// SwitchChatSession activates a tab and returns its UI transcript JSON.
func (a *App) SwitchChatSession(sessionID string) (string, error) {
	if a.chats == nil {
		return "[]", nil
	}
	if err := a.chats.SetActive(a.projectKey(), sessionID); err != nil {
		return "[]", err
	}
	sess, err := a.chats.Get(a.projectKey(), sessionID)
	if err != nil {
		return "[]", err
	}
	a.mu.Lock()
	a.sessionID = sess.ID
	a.history = append([]llm.Message{}, sess.History...)
	a.mu.Unlock()
	if sess.ItemsJSON == "" {
		return "[]", nil
	}
	return sess.ItemsJSON, nil
}

// DeleteChatSession removes a tab; returns the new active transcript JSON.
func (a *App) DeleteChatSession(sessionID string) (string, error) {
	if a.chats == nil {
		return "[]", nil
	}
	a.StopAgentSession(sessionID)
	if err := a.chats.DeleteSession(a.projectKey(), sessionID); err != nil {
		return "[]", err
	}
	b, err := a.chats.List(a.projectKey())
	if err != nil {
		return "[]", err
	}
	return a.SwitchChatSession(b.ActiveID)
}

func (a *App) ClearChat() {
	a.mu.Lock()
	sid := a.sessionID
	a.history = nil
	a.mu.Unlock()
	a.StopAgentSession(sid)
	if a.chats != nil {
		_ = a.chats.Clear(a.projectKey())
		_ = a.loadActiveIntoMemory()
	}
}

// LoadChat returns persisted UI transcript JSON for the active (or given) project.
func (a *App) LoadChat(projectPath string) (string, error) {
	if a.chats == nil {
		return "[]", nil
	}
	if projectPath == "" {
		projectPath = a.projectKey()
	}
	b, err := a.chats.List(projectPath)
	if err != nil {
		return "[]", err
	}
	sess, err := a.chats.Get(projectPath, b.ActiveID)
	if err != nil {
		return "[]", err
	}
	a.mu.Lock()
	a.sessionID = sess.ID
	a.history = append([]llm.Message{}, sess.History...)
	a.mu.Unlock()
	if sess.ItemsJSON == "" {
		return "[]", nil
	}
	return sess.ItemsJSON, nil
}

// SaveChat persists UI transcript + current LLM history for the active session.
func (a *App) SaveChat(itemsJSON string) error {
	if a.chats == nil {
		return nil
	}
	a.mu.Lock()
	hist := append([]llm.Message{}, a.history...)
	sid := a.sessionID
	a.mu.Unlock()
	if sid == "" {
		return a.chats.Save(&chatstore.State{
			Project:   a.projectKey(),
			ItemsJSON: itemsJSON,
			History:   hist,
		})
	}
	sess, err := a.chats.Get(a.projectKey(), sid)
	if err != nil {
		return a.chats.Save(&chatstore.State{
			Project:   a.projectKey(),
			ItemsJSON: itemsJSON,
			History:   hist,
		})
	}
	sess.ItemsJSON = itemsJSON
	sess.History = hist
	if sess.Title == "" || sess.Title == "Chat" || sess.Title == "Chat 1" {
		if t := titleFromItemsJSON(itemsJSON); t != "" {
			sess.Title = t
		}
	}
	return a.chats.SaveSession(a.projectKey(), sess)
}

// SaveChatSession persists a specific session transcript from the UI (multitasking).
func (a *App) SaveChatSession(sessionID, itemsJSON string) error {
	if a.chats == nil || sessionID == "" {
		return nil
	}
	sess, err := a.chats.Get(a.projectKey(), sessionID)
	if err != nil {
		return err
	}
	sess.ItemsJSON = itemsJSON
	a.mu.Lock()
	if a.sessionID == sessionID {
		sess.History = append([]llm.Message{}, a.history...)
	}
	a.mu.Unlock()
	if t := titleFromItemsJSON(itemsJSON); t != "" && (sess.Title == "" || sess.Title == "Chat" || sess.Title == "Chat 1") {
		sess.Title = t
	}
	return a.chats.SaveSession(a.projectKey(), sess)
}

func titleFromItemsJSON(itemsJSON string) string {
	type peek struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	var items []peek
	if err := json.Unmarshal([]byte(itemsJSON), &items); err != nil {
		return ""
	}
	for _, it := range items {
		if it.Kind == "user" && it.Content != "" {
			runes := []rune(it.Content)
			if len(runes) > 40 {
				return string(runes[:40]) + "…"
			}
			return string(runes)
		}
	}
	return ""
}

func (a *App) StopAgent() {
	a.mu.Lock()
	sid := a.sessionID
	a.mu.Unlock()
	a.StopAgentSession(sid)
}

func (a *App) StopAgentSession(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if sessionID == "" {
		for id, c := range a.cancels {
			if c != nil {
				c()
			}
			delete(a.cancels, id)
		}
		return
	}
	if c := a.cancels[sessionID]; c != nil {
		c()
		delete(a.cancels, sessionID)
	}
}

// RunAgent starts the DeepSeek tool-using agent loop. Progress via event "agent:event".
func (a *App) RunAgent(userMessage string) error {
	return a.RunAgentWithAttachments(userMessage, nil)
}

// RunAgentWithAttachments accepts pasted/dropped images and files (multimodal + text inline).
func (a *App) RunAgentWithAttachments(userMessage string, attachments []agent.Attachment) error {
	if a.cfg.Get().DeepSeekAPIKey == "" {
		return fmt.Errorf("DeepSeek API key is not set")
	}
	if a.llm == nil {
		return fmt.Errorf("LLM provider is not configured")
	}
	_, _ = a.ws.ActiveRoot()

	a.mu.Lock()
	sid := a.sessionID
	if sid == "" && a.chats != nil {
		_ = a.loadActiveIntoMemoryUnlocked()
		sid = a.sessionID
	}
	if sid == "" {
		sid = "default"
		a.sessionID = sid
	}
	if old := a.cancels[sid]; old != nil {
		old()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancels[sid] = cancel
	hist := append([]llm.Message{}, a.history...)
	a.mu.Unlock()

	runner := &agent.Runner{
		Provider: a.llm,
		Tools:    a.tools,
		MaxSteps: agent.DefaultMaxSteps,
	}

	var costUSD float64
	var inTokens, outTokens int
	runner.OnUsage = func(model string, u *llm.Usage) {
		if u == nil {
			return
		}
		costUSD += costing.Cost(model, u)
		inTokens += u.PromptTokens
		outTokens += u.CompletionTokens
	}

	go func() {
		dockicon.BeginAgent()
		defer dockicon.EndAgent()

		emit := func(evt agent.Event) {
			a.emitFor(sid, evt)
		}
		var newHist []llm.Message
		var err error
		if len(attachments) > 0 {
			newHist, err = runner.RunWithAttachments(ctx, hist, userMessage, attachments, emit)
		} else {
			newHist, err = runner.Run(ctx, hist, userMessage, emit)
		}
		a.mu.Lock()
		if a.sessionID == sid {
			if err == nil || len(newHist) > 0 {
				a.history = newHist
			}
		}
		delete(a.cancels, sid)
		a.mu.Unlock()

		// Persist LLM history into the session even if UI switched away.
		if a.chats != nil && (err == nil || len(newHist) > 0) {
			if sess, gerr := a.chats.Get(a.projectKey(), sid); gerr == nil {
				sess.History = newHist
				_ = a.chats.SaveSession(a.projectKey(), sess)
			}
		}
		if inTokens+outTokens > 0 {
			if a.cfg.AddUsage(costUSD, inTokens, outTokens) == nil {
				a.emitFor(sid, agent.Event{Type: "usage", Content: usagePayload(a.cfg.Get())})
			}
		}
		a.emitFor(sid, agent.Event{Type: "persist", Content: "1"})
	}()
	return nil
}

func usagePayload(s config.Settings) string {
	b, _ := json.Marshal(map[string]any{
		"costUsd":      s.TotalCostUSD,
		"inputTokens":  s.TotalInputTokens,
		"outputTokens": s.TotalOutputTokens,
	})
	return string(b)
}

// loadActiveIntoMemoryUnlocked assumes caller may or may not hold lock — only used carefully.
func (a *App) loadActiveIntoMemoryUnlocked() error {
	if a.chats == nil {
		return nil
	}
	b, err := a.chats.List(a.projectKey())
	if err != nil {
		return err
	}
	sess, err := a.chats.Get(a.projectKey(), b.ActiveID)
	if err != nil {
		return err
	}
	a.sessionID = sess.ID
	a.history = append([]llm.Message{}, sess.History...)
	return nil
}

// ChatOnce is a simple non-agent ping (no tools) for connectivity checks.
func (a *App) ChatOnce(userMessage string) (string, error) {
	if a.llm == nil {
		return "", fmt.Errorf("LLM provider is not configured")
	}
	if a.cfg.Get().DeepSeekAPIKey == "" {
		return "", fmt.Errorf("DeepSeek API key is not set")
	}
	maxTokens := 64
	resp, err := a.llm.ChatCompletion(a.ctx, &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: userMessage},
		},
		MaxTokens: &maxTokens,
		Thinking:  map[string]any{"type": "disabled"},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return resp.Choices[0].Message.Content, nil
}

// --- terminal ---

func (a *App) ensureTerminal(cwd string) {
	if a.term != nil {
		a.term.Stop()
	}
	a.term = shell.NewSession(cwd, a.emitTerm)
	_ = a.term.Start()
}

func (a *App) StartTerminal() error {
	root, err := a.ws.ActiveRoot()
	if err != nil {
		root = ""
	}
	a.ensureTerminal(root)
	return nil
}

func (a *App) StopTerminal() {
	if a.term != nil {
		a.term.Stop()
		a.term = nil
	}
}

func (a *App) TerminalWrite(data string) error {
	if a.term == nil {
		if err := a.StartTerminal(); err != nil {
			return err
		}
	}
	return a.term.Write(data)
}

func (a *App) RunShell(command string) (map[string]any, error) {
	root, _ := a.ws.ActiveRoot()
	res, err := shell.Run(a.ctx, command, root, 0)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"stdout":   res.Stdout,
		"stderr":   res.Stderr,
		"exitCode": res.ExitCode,
	}, nil
}

// --- ssh helpers for UI ---

func (a *App) SSHKeygen(name string) (string, error) {
	svc := sshx.New(a.sshDir)
	return svc.Keygen(name)
}

func (a *App) SSHListKeys() ([]string, error) {
	svc := sshx.New(a.sshDir)
	return svc.ListKeys()
}
