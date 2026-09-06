package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"notcursor.ai/app/internal/agent"
	"notcursor.ai/app/internal/config"
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

	mu       sync.Mutex
	cancel   context.CancelFunc
	history  []llm.Message
	term     *shell.Session
	sshDir   string
}

func NewApp() *App {
	return &App{
		cfg:     config.NewStore(),
		ws:      workspace.NewManager(),
		history: []llm.Message{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.cfg.Load()
	sshDir, err := a.cfg.SSHDir()
	if err == nil {
		a.sshDir = sshDir
	}
	a.tools = tools.NewRegistry(a.ws, a.sshDir)
	a.applyGitAuth()
	a.refreshProvider()

	// restore recent projects into workspace list (not auto-active)
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
		"version": "0.1.1",
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
	}
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
	a.ensureTerminal(p.Path)
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

// --- agent ---

func (a *App) ClearChat() {
	a.mu.Lock()
	a.history = nil
	a.mu.Unlock()
}

func (a *App) StopAgent() {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.mu.Unlock()
}

// RunAgent starts the DeepSeek tool-using agent loop. Progress via event "agent:event".
func (a *App) RunAgent(userMessage string) error {
	if a.cfg.Get().DeepSeekAPIKey == "" {
		return fmt.Errorf("DeepSeek API key is not set")
	}
	if a.llm == nil {
		return fmt.Errorf("LLM provider is not configured")
	}
	if _, err := a.ws.ActiveRoot(); err != nil {
		// allow chat without project, but tools needing workspace will fail
	}

	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	hist := append([]llm.Message{}, a.history...)
	a.mu.Unlock()

	runner := &agent.Runner{
		Provider: a.llm,
		Tools:    a.tools,
		MaxSteps: agent.DefaultMaxSteps,
	}

	go func() {
		newHist, err := runner.Run(ctx, hist, userMessage, a.emit)
		a.mu.Lock()
		if err == nil {
			a.history = newHist
		} else if len(newHist) > 0 {
			a.history = newHist
		}
		a.cancel = nil
		a.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			a.emit(agent.Event{Type: "error", Content: err.Error()})
		}
	}()
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
