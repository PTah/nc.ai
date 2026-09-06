package main

import (
	"context"
	"fmt"

	"notcursor.ai/app/internal/config"
	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/llm/providers/deepseek"
	"notcursor.ai/app/internal/workspace"
)

// App is the Wails-bound application façade.
type App struct {
	ctx    context.Context
	cfg    *config.Store
	ws     *workspace.Manager
	llm    llm.Provider
}

func NewApp() *App {
	return &App{
		cfg: config.NewStore(),
		ws:  workspace.NewManager(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.cfg.Load()
	a.refreshProvider()
}

func (a *App) refreshProvider() {
	key := a.cfg.Get().DeepSeekAPIKey
	model := a.cfg.Get().DeepSeekModel
	if model == "" {
		model = deepseek.DefaultModel
	}
	a.llm = deepseek.New(key, model)
}

// AppInfo returns basic product metadata for the UI.
func (a *App) AppInfo() map[string]string {
	return map[string]string{
		"name":    "NotCursor.ai",
		"version": "0.1.0",
		"stage":   "1-deepseek",
	}
}

// GetSettings returns non-secret settings plus whether API key is set.
func (a *App) GetSettings() map[string]any {
	s := a.cfg.Get()
	return map[string]any{
		"deepseekModel":   s.DeepSeekModel,
		"deepseekKeySet":  s.DeepSeekAPIKey != "",
		"shell":           s.Shell,
		"defaultProjects": s.RecentProjects,
	}
}

// SaveDeepSeekKey stores the DeepSeek API key locally.
func (a *App) SaveDeepSeekKey(apiKey string) error {
	if err := a.cfg.SetDeepSeekAPIKey(apiKey); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

// SaveDeepSeekModel updates the default DeepSeek model id.
func (a *App) SaveDeepSeekModel(model string) error {
	if err := a.cfg.SetDeepSeekModel(model); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

// ListProjects returns recent/open projects.
func (a *App) ListProjects() []workspace.Project {
	return a.ws.List()
}

// OpenProject registers a project root and makes it active.
func (a *App) OpenProject(path string) (*workspace.Project, error) {
	p, err := a.ws.Open(path)
	if err != nil {
		return nil, err
	}
	_ = a.cfg.AddRecentProject(path)
	return p, nil
}

// ListDir lists files under a workspace-relative path.
func (a *App) ListDir(rel string) ([]workspace.Entry, error) {
	return a.ws.ListDir(rel)
}

// ReadFile reads a workspace file (bounded size for UI preview).
func (a *App) ReadFile(rel string) (string, error) {
	return a.ws.ReadFile(rel)
}

// ChatOnce sends a non-streaming chat request to the configured provider (DeepSeek).
func (a *App) ChatOnce(userMessage string) (string, error) {
	if a.llm == nil {
		return "", fmt.Errorf("LLM provider is not configured")
	}
	if a.cfg.Get().DeepSeekAPIKey == "" {
		return "", fmt.Errorf("DeepSeek API key is not set")
	}
	resp, err := a.llm.ChatCompletion(a.ctx, &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are NotCursor.ai coding agent. Be concise."},
			{Role: "user", Content: userMessage},
		},
		Stream: false,
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from provider")
	}
	return resp.Choices[0].Message.Content, nil
}
