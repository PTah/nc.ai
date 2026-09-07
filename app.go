package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"notcursor.ai/app/internal/agent"
	"notcursor.ai/app/internal/chatstore"
	"notcursor.ai/app/internal/config"
	"notcursor.ai/app/internal/costing"
	"notcursor.ai/app/internal/dockicon"
	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/llm/providers/deepseek"
	"notcursor.ai/app/internal/llm/providers/zai"
	"notcursor.ai/app/internal/rules"
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
	rulesMu   sync.RWMutex
	cancels   map[string]context.CancelFunc
	sessionID string
	history   []llm.Message
	term      *shell.Session
	sshDir    string

	cursorRules rules.Bundle
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
	a.refreshProvider()
	// Drop legacy in-app Git passwords (auth is OS git / ~/.ssh now).
	if s := a.cfg.Get(); s.GitUsername != "" || s.GitPassword != "" {
		_ = a.cfg.SetGitAuth("", "")
	}

	for _, p := range a.cfg.Get().RecentProjects {
		_, _ = a.ws.Open(p)
	}
	a.loadRules()
}

func (a *App) domReady(ctx context.Context) {
	s := a.cfg.Get()
	if s.WindowMaximised {
		runtime.WindowMaximise(ctx)
		return
	}
	if s.WindowPosSet {
		runtime.WindowSetPosition(ctx, s.WindowX, s.WindowY)
	}
}

func (a *App) saveWindowGeometry() {
	if a.ctx == nil || a.cfg == nil {
		return
	}
	w, h := runtime.WindowGetSize(a.ctx)
	x, y := runtime.WindowGetPosition(a.ctx)
	max := runtime.WindowIsMaximised(a.ctx)
	_ = a.cfg.SetWindowGeometry(w, h, x, y, max)
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	a.ctx = ctx
	a.saveWindowGeometry()
	return false
}

func (a *App) shutdown(ctx context.Context) {
	a.ctx = ctx
	a.saveWindowGeometry()
}

func (a *App) refreshProvider() {
	provider := a.cfg.Provider()
	key := a.cfg.ActiveAPIKey()
	model := a.cfg.ActiveModel()
	switch provider {
	case config.ProviderZAI:
		if model == "" {
			model = zai.DefaultModel
		}
		a.llm = zai.NewWithBaseURL(key, model, zai.BaseURLFor(a.cfg.ZaiEndpoint()))
	default:
		if model == "" {
			model = deepseek.DefaultModel
		}
		a.llm = deepseek.New(key, model)
	}
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
		"version": "0.4.0",
		"stage":   "2-multi-provider",
	}
}

func (a *App) GetSettings() map[string]any {
	s := a.cfg.Get()
	provider := a.cfg.Provider()
	return map[string]any{
		"activeProvider":  provider,
		"deepseekModel":   s.DeepSeekModel,
		"deepseekKeySet":  s.DeepSeekAPIKey != "",
		"zaiModel":        orDefault(s.ZaiModel, zai.DefaultModel),
		"zaiKeySet":       s.ZaiAPIKey != "",
		"zaiEndpoint":     a.cfg.ZaiEndpoint(),
		"shell":           s.Shell,
		"recentProjects":  s.RecentProjects,
		"showTerminal":    s.ShowTerminal,
		"showFiles":       a.cfg.FilesVisible(),
		"showSettings":    s.ShowSettings,
		"theme":           a.cfg.Theme(),
		"agentMaxSteps":   a.cfg.MaxAgentSteps(),
		"autoModels":      a.cfg.AutoModels(),
		"visionModel":     deepseek.VisionModel,
		"layoutProjectsW": nonzero(s.LayoutProjectsW, 200),
		"layoutTreeW":     nonzero(s.LayoutTreeW, 220),
		"layoutSettingsW": nonzero(s.LayoutSettingsW, 230),
		"layoutTerminalH": nonzero(s.LayoutTerminalH, 160),
		"layoutComposerH": nonzero(s.LayoutComposerH, 150),
	}
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func nonzero(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// loadRules re-scans Cursor rules (global + active project) and caches them.
func (a *App) loadRules() rules.Bundle {
	root, _ := a.ws.ActiveRoot()
	b := rules.Load(root)
	a.rulesMu.Lock()
	a.cursorRules = b
	a.rulesMu.Unlock()
	return b
}

// GetCursorRules returns the cached Cursor rules bundle for the UI.
func (a *App) GetCursorRules() rules.Bundle {
	a.rulesMu.RLock()
	defer a.rulesMu.RUnlock()
	return a.cursorRules
}

// ReloadCursorRules re-scans rules on demand (e.g. after editing them in Cursor).
func (a *App) ReloadCursorRules() rules.Bundle {
	return a.loadRules()
}

// UsageStats is the top-bar spend counter (all-time + active chat).
// Typed struct (not map[string]any) so Wails/WebKit on macOS reliably
// delivers numeric fields to the frontend.
type UsageStats struct {
	CostUsd          float64 `json:"costUsd"`
	InputTokens      int     `json:"inputTokens"`
	OutputTokens     int     `json:"outputTokens"`
	ChatCostUsd      float64 `json:"chatCostUsd"`
	ChatInputTokens  int     `json:"chatInputTokens"`
	ChatOutputTokens int     `json:"chatOutputTokens"`
}

// GetUsageStats returns all-time spend plus the active chat spend (if any).
func (a *App) GetUsageStats() UsageStats {
	s := a.cfg.Get()
	out := UsageStats{
		CostUsd:      s.TotalCostUSD,
		InputTokens:  s.TotalInputTokens,
		OutputTokens: s.TotalOutputTokens,
	}
	if a.chats == nil {
		return out
	}
	a.mu.Lock()
	sid := a.sessionID
	a.mu.Unlock()
	if sid == "" {
		return out
	}
	if sess, err := a.chats.Get(a.projectKey(), sid); err == nil && sess != nil {
		out.ChatCostUsd = sess.CostUSD
		out.ChatInputTokens = sess.InputTokens
		out.ChatOutputTokens = sess.OutputTokens
	}
	return out
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

func (a *App) SaveShowSettings(show bool) error {
	return a.cfg.SetShowSettings(show)
}

// SaveLayoutSizes persists inner pane widths/heights (projects/tree/settings/terminal).
func (a *App) SaveLayoutSizes(projectsW, treeW, settingsW, terminalH int) error {
	return a.cfg.SetLayoutSizes(projectsW, treeW, settingsW, terminalH)
}

// SaveComposerHeight persists the resizable chat input area height.
func (a *App) SaveComposerHeight(h int) error {
	return a.cfg.SetComposerH(h)
}

// SaveWindowGeometry flushes the OS window size/position (also on close).
func (a *App) SaveWindowGeometry() error {
	a.saveWindowGeometry()
	return nil
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

func (a *App) SaveZaiKey(apiKey string) error {
	if err := a.cfg.SetZaiAPIKey(apiKey); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

func (a *App) SaveZaiModel(model string) error {
	if err := a.cfg.SetZaiModel(model); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

func (a *App) SaveZaiEndpoint(endpoint string) error {
	if err := a.cfg.SetZaiEndpoint(endpoint); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

// ListZaiModels returns model ids from GET {base}/models for the saved Z.ai key.
// Official free-tier ids are always merged in (API catalog often omits them).
func (a *App) ListZaiModels() []string {
	key := a.cfg.Get().ZaiAPIKey
	var out []string
	if key == "" {
		out = append([]string{}, zai.FallbackModels()...)
	} else {
		client := zai.NewWithBaseURL(key, a.cfg.Get().ZaiModel, zai.BaseURLFor(a.cfg.ZaiEndpoint()))
		ctx := a.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		items, err := client.ListModels(ctx)
		if err != nil || len(items) == 0 {
			out = append([]string{}, zai.FallbackModels()...)
		} else {
			seen := map[string]bool{}
			for _, m := range items {
				id := strings.TrimSpace(m.ID)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, id)
			}
			if len(out) == 0 {
				out = append([]string{}, zai.FallbackModels()...)
			}
		}
	}
	return zai.OrderModels(zai.MergeFreeModels(out))
}

// PreferZaiModel prefers official free-tier models when present (pay-as-you-go $0).
func (a *App) PreferZaiModel(available []string) string {
	return zai.PreferFreeModel(available, a.cfg.Get().ZaiModel)
}

func (a *App) SaveActiveProvider(provider string) error {
	if err := a.cfg.SetActiveProvider(provider); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

func (a *App) SaveAutoModels(on bool) error {
	return a.cfg.SetAutoModels(on)
}

func (a *App) SaveTheme(theme string) error {
	return a.cfg.SetTheme(theme)
}

func (a *App) SaveAgentMaxSteps(steps int) error {
	return a.cfg.SetAgentMaxSteps(steps)
}

// SaveGitAuth is deprecated: Git uses OS credential helpers / SSH keys.
// Calling it clears any leftover in-app secrets.
func (a *App) SaveGitAuth(_, _ string) error {
	return a.cfg.SetGitAuth("", "")
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
	a.loadRules()
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

// RenameChatSession changes the title of one chat tab.
func (a *App) RenameChatSession(sessionID, title string) error {
	if a.chats == nil {
		return fmt.Errorf("chat store unavailable")
	}
	return a.chats.RenameSession(a.projectKey(), sessionID, title)
}

// ArchiveChatSession moves a chat tab into the archive folder and returns the
// transcript JSON of the new active session.
func (a *App) ArchiveChatSession(sessionID, title string) (string, error) {
	if a.chats == nil {
		return "[]", fmt.Errorf("chat store unavailable")
	}
	a.StopAgentSession(sessionID)
	b, err := a.chats.ArchiveSession(a.projectKey(), sessionID, title)
	if err != nil {
		return "[]", err
	}
	if b.ActiveID == "" {
		return "[]", nil
	}
	sess, err := a.chats.Get(a.projectKey(), b.ActiveID)
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
	if a.cfg.ActiveAPIKey() == "" {
		switch a.cfg.Provider() {
		case config.ProviderZAI:
			return fmt.Errorf("Z.ai API key is not set")
		default:
			return fmt.Errorf("DeepSeek API key is not set")
		}
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

	hasImages := false
	attNames := make([]string, 0, len(attachments))
	for _, att := range attachments {
		if att.Name != "" {
			attNames = append(attNames, att.Name)
		}
		if att.IsImage {
			hasImages = true
		}
	}
	bundle := a.loadRules()
	hints := rules.ExtractHintPaths(userMessage, attNames...)
	cfg := a.cfg.Get()
	provider := a.cfg.Provider()
	// Auto-models routing is DeepSeek-specific (flash/pro/vision).
	autoModels := cfg.AutoModels && provider == config.ProviderDeepSeek

	runner := &agent.Runner{
		Provider:       a.llm,
		Tools:          a.tools,
		MaxSteps:       a.cfg.MaxAgentSteps(),
		RulesText:      bundle.SelectForPrompt(hints),
		AutoModels:     autoModels,
		PreferredModel: a.cfg.ActiveModel(),
		UserText:       userMessage,
		HasImages:      hasImages,
		HintPathCount:  len(hints),
	}

	var costUSD float64
	var inTokens, outTokens int
	runner.OnUsage = func(model string, u *llm.Usage) {
		if u == nil {
			return
		}
		costUSD += costing.Cost(model, u)
		inTokens += costing.InputTokens(u)
		outTokens += max(u.CompletionTokens, 0)
		// Live preview in the top bar (totals not yet persisted until the run ends).
		base := a.cfg.Get()
		chatCost, chatIn, chatOut := 0.0, 0, 0
		if a.chats != nil {
			if sess, err := a.chats.Get(a.projectKey(), sid); err == nil && sess != nil {
				chatCost, chatIn, chatOut = sess.CostUSD, sess.InputTokens, sess.OutputTokens
			}
		}
		a.emitFor(sid, agent.Event{Type: "usage", Content: usagePayload(
			base.TotalCostUSD+costUSD, base.TotalInputTokens+inTokens, base.TotalOutputTokens+outTokens,
			chatCost+costUSD, chatIn+inTokens, chatOut+outTokens,
		)})
	}

	go func() {
		dockicon.BeginAgent()
		defer dockicon.EndAgent()

		emit := func(evt agent.Event) {
			if evt.Type == "model" && evt.Content != "" {
				_ = a.cfg.SetActiveModel(evt.Content)
				a.refreshProvider()
			}
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
			if err == nil {
				a.history = newHist
			}
		}
		delete(a.cancels, sid)
		a.mu.Unlock()

		// Persist LLM history only on success; on error keep the previous state
		// so a retry does not duplicate the failed user message.
		if a.chats != nil && err == nil {
			if sess, gerr := a.chats.Get(a.projectKey(), sid); gerr == nil {
				sess.History = newHist
				_ = a.chats.SaveSession(a.projectKey(), sess)
			}
		}
		if inTokens+outTokens > 0 {
			_ = a.cfg.AddUsage(costUSD, inTokens, outTokens)
			if a.chats != nil {
				_, _ = a.chats.AddUsage(a.projectKey(), sid, costUSD, inTokens, outTokens)
			}
		}
		tot := a.cfg.Get()
		chatCost, chatIn, chatOut := 0.0, 0, 0
		if a.chats != nil {
			if sess, gerr := a.chats.Get(a.projectKey(), sid); gerr == nil && sess != nil {
				chatCost, chatIn, chatOut = sess.CostUSD, sess.InputTokens, sess.OutputTokens
			}
		}
		a.emitFor(sid, agent.Event{Type: "usage", Content: usagePayload(
			tot.TotalCostUSD, tot.TotalInputTokens, tot.TotalOutputTokens,
			chatCost, chatIn, chatOut,
		)})
		a.emitFor(sid, agent.Event{Type: "persist", Content: "1"})
	}()
	return nil
}

func usagePayload(totalCost float64, totalIn, totalOut int, chatCost float64, chatIn, chatOut int) string {
	b, _ := json.Marshal(map[string]any{
		"costUsd":          totalCost,
		"inputTokens":      totalIn,
		"outputTokens":     totalOut,
		"chatCostUsd":      chatCost,
		"chatInputTokens":  chatIn,
		"chatOutputTokens": chatOut,
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
	if a.cfg.ActiveAPIKey() == "" {
		switch a.cfg.Provider() {
		case config.ProviderZAI:
			return "", fmt.Errorf("Z.ai API key is not set")
		default:
			return "", fmt.Errorf("DeepSeek API key is not set")
		}
	}
	maxTokens := 64
	chatReq := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: userMessage},
		},
		MaxTokens: &maxTokens,
		Thinking:  map[string]any{"type": "disabled"},
	}
	// GLM models (esp. 5.3+) always think; client maps disabled→low, but need room for CoT.
	if a.cfg.Provider() == config.ProviderZAI {
		maxTokens = 512
		chatReq.MaxTokens = &maxTokens
		chatReq.Thinking = map[string]any{"type": "enabled"}
		chatReq.ReasoningEffort = "low"
	}
	resp, err := a.llm.ChatCompletion(a.ctx, chatReq)
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
