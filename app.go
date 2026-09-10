package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"notcursor.ai/app/internal/agent"
	"notcursor.ai/app/internal/appmeta"
	"notcursor.ai/app/internal/chatstore"
	"notcursor.ai/app/internal/config"
	"notcursor.ai/app/internal/costing"
	"notcursor.ai/app/internal/dockicon"
	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/llm/providers/deepseek"
	"notcursor.ai/app/internal/llm/providers/openrouter"
	"notcursor.ai/app/internal/llm/providers/zai"
	"notcursor.ai/app/internal/redact"
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

	mu      sync.Mutex
	rulesMu sync.RWMutex
	cancels map[string]context.CancelFunc
	runGens map[string]uint64
	// deepSeekRetireNoticed guards the one-time chat notice about V4 Pro retirement.
	deepSeekRetireNoticed bool
	// sessionStickyModel keeps the last non-vision Auto model per chat session
	// so consecutive turns reuse one provider cache namespace.
	sessionStickyModel map[string]string
	approveMu          sync.Mutex
	pendingApprove     map[string]chan bool // key: sessionID+"/"+callID
	pendingAsk         map[string]chan string
	sessionID          string
	history            []llm.Message
	term               *shell.Session
	sshDir             string

	ideMu   sync.Mutex
	ideFile string
	ideLine int

	cursorRules rules.Bundle

	zaiBalMu sync.Mutex
	zaiBal   zai.AccountBalance

	orBalMu sync.Mutex
	orBal   openrouter.AccountBalance
}

func NewApp() *App {
	return &App{
		cfg:                config.NewStore(),
		ws:                 workspace.NewManager(),
		history:            []llm.Message{},
		cancels:            map[string]context.CancelFunc{},
		runGens:            map[string]uint64{},
		sessionStickyModel: map[string]string{},
		pendingApprove:     map[string]chan bool{},
		pendingAsk:         map[string]chan string{},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.cfg.Load()
	a.applyDeepSeekStartupDefaults()
	sshDir, err := a.cfg.SSHDir()
	if err == nil {
		a.sshDir = sshDir
	}
	if base, err := a.cfg.AppDataDir(); err == nil {
		a.chats, _ = chatstore.New(base)
	}
	a.tools = tools.NewRegistry(a.ws, a.sshDir)
	a.tools.Shell = shell.ResolveShell(a.cfg.Get().Shell)
	a.refreshProvider()
	// Drop legacy in-app Git passwords (auth is OS git / ~/.ssh now).
	if s := a.cfg.Get(); s.GitUsername != "" || s.GitPassword != "" {
		_ = a.cfg.SetGitAuth("", "")
	}

	for _, p := range a.cfg.Get().RecentProjects {
		_, _ = a.ws.Open(p)
	}
	// Re-activate the project that was open when the app was closed, so the
	// chat shown on startup matches where the user left off.
	if last := a.cfg.LastProject(); last != "" {
		for _, p := range a.cfg.Get().RecentProjects {
			if p == last {
				_, _ = a.ws.Open(p)
				break
			}
		}
	}
	a.loadRules()
	costing.ApplyPersisted(a.cfg)
	go a.priceRefreshLoop()
}

// applyDeepSeekStartupDefaults: DeepSeek → flash model + Auto-Models on.
func (a *App) applyDeepSeekStartupDefaults() {
	if a.cfg.Provider() != config.ProviderDeepSeek {
		return
	}
	_ = a.cfg.SetDeepSeekModel("deepseek-v4-flash")
	_ = a.cfg.SetAutoModels(true)
}

func (a *App) domReady(ctx context.Context) {
	a.noticeDeepSeekProRetired()
	s := a.cfg.Get()
	if s.WindowMaximised {
		runtime.WindowMaximise(ctx)
		return
	}
	if s.WindowPosSet {
		runtime.WindowSetPosition(ctx, s.WindowX, s.WindowY)
	}
}

// noticeDeepSeekProRetired tells the chat once per launch that V4 Pro is retired.
func (a *App) noticeDeepSeekProRetired() {
	if a.deepSeekRetireNoticed || !appmeta.DeepSeekProRetired(time.Now()) {
		return
	}
	a.deepSeekRetireNoticed = true
	a.emit(agent.Event{Type: "notice", Content: "Система: DeepSeek V4 Pro выведена из эксплуатации — доступны V4 Flash и V4 Flash Vision; запросы Pro тарифицируются как Flash"})
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

// priceRefreshLoop loads persisted sheets, then refreshes when due:
// DeepSeek and OpenRouter at most once per Beijing day after 13:05 Beijing;
// Z.ai weekly. The loop checks every 15 minutes.
func (a *App) priceRefreshLoop() {
	costing.ApplyPersisted(a.cfg)
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.emitPriceNotices(costing.RefreshIfDue(ctx, a.cfg, false))
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.emitPriceNotices(costing.RefreshIfDue(context.Background(), a.cfg, false))
		}
	}
}

// priceNotice builds a chat system notice for a price check result.
func priceNotice(provider string, updated bool, errMsg string) string {
	switch {
	case errMsg != "":
		return fmt.Sprintf("Система: проверка цен %s выполнена — ошибка: %s", provider, errMsg)
	case updated:
		return fmt.Sprintf("Система: проверка цен %s выполнена — обновлено", provider)
	default:
		return fmt.Sprintf("Система: проверка цен %s выполнена — без изменений", provider)
	}
}

func (a *App) emitPriceNotices(r costing.RefreshResult) {
	if r.DeepSeekChecked {
		a.emit(agent.Event{Type: "notice", Content: priceNotice("DeepSeek", r.DeepSeekUpdated, r.DeepSeekErr)})
	}
	if r.ZaiChecked {
		a.emit(agent.Event{Type: "notice", Content: priceNotice("z.ai", r.ZaiUpdated, r.ZaiErr)})
	}
	if r.OpenRouterChecked {
		a.emit(agent.Event{Type: "notice", Content: priceNotice("OpenRouter", r.OpenRouterUpdated, r.OpenRouterErr)})
	}
}

// RefreshProviderPrices forces a re-fetch of DeepSeek + Z.ai + OpenRouter price data.
func (a *App) RefreshProviderPrices() map[string]any {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	r := costing.RefreshIfDue(ctx, a.cfg, true)
	a.emitPriceNotices(r)
	return map[string]any{
		"deepseekChecked":   r.DeepSeekChecked,
		"deepseekUpdated":   r.DeepSeekUpdated,
		"deepseekError":     r.DeepSeekErr,
		"zaiChecked":        r.ZaiChecked,
		"zaiUpdated":        r.ZaiUpdated,
		"zaiError":          r.ZaiErr,
		"openrouterChecked": r.OpenRouterChecked,
		"openrouterUpdated": r.OpenRouterUpdated,
		"openrouterError":   r.OpenRouterErr,
	}
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	a.ctx = ctx
	a.saveWindowGeometry()
	a.saveLastProject()
	return false
}

func (a *App) shutdown(ctx context.Context) {
	a.ctx = ctx
	a.saveWindowGeometry()
	a.saveLastProject()
}

func (a *App) saveLastProject() {
	if root, err := a.ws.ActiveRoot(); err == nil && root != "" {
		_ = a.cfg.SetLastProject(root)
	}
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
	case config.ProviderOpenRouter:
		if model == "" {
			model = openrouter.DefaultModel
		}
		a.llm = openrouter.New(key, model)
	default:
		if model == "" {
			model = deepseek.DefaultModel
		}
		a.llm = deepseek.New(key, model)
	}
}

func (a *App) logAgentLine(line string) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	dir = filepath.Join(dir, "NotCursor")
	_ = os.MkdirAll(dir, 0o700)
	f, err := os.OpenFile(filepath.Join(dir, "agent.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
}

func (a *App) emit(evt agent.Event) {
	if a.ctx == nil {
		return
	}
	evt.Content = redact.String(evt.Content)
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
		"name":    appmeta.Name,
		"version": appmeta.Version,
		"stage":   "2-multi-provider",
	}
}

// WelcomeInfo is shown once after installing a newer build.
type WelcomeInfo struct {
	Show       bool     `json:"show"`
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Eyebrow    string   `json:"eyebrow"`
	VersionLbl string   `json:"versionLabel"`
	WhatsNew   string   `json:"whatsNew"`
	Continue   string   `json:"continueLabel"`
	Highlights []string `json:"highlights"`
	Locale     string   `json:"locale"`
}

// GetWelcome returns splash data when LastSeenVersion != current app version.
// lang is a BCP-47 tag from the UI (navigator.language), e.g. "ru-RU" or "en-US".
func (a *App) GetWelcome(lang string) WelcomeInfo {
	cur := appmeta.Version
	seen := a.cfg.LastSeenVersion()
	copy := appmeta.CopyFor(lang)
	return WelcomeInfo{
		Show:       seen != cur,
		Name:       appmeta.Name,
		Version:    cur,
		Eyebrow:    copy.Eyebrow,
		VersionLbl: copy.Version,
		WhatsNew:   copy.WhatsNew,
		Continue:   copy.Continue,
		Highlights: appmeta.HighlightsFor(lang),
		Locale:     lang,
	}
}

// AckWelcome marks the current version as seen so the splash does not reappear.
func (a *App) AckWelcome() error {
	return a.cfg.SetLastSeenVersion(appmeta.Version)
}

// ListModelPrices returns curated USD/1M rates ordered weak → strong.
func (a *App) ListModelPrices() []costing.ModelPrice {
	return costing.Catalog()
}

// GetDeepSeekPeakInfo reports whether DeepSeek is in peak hours now (lang for tooltip).
func (a *App) GetDeepSeekPeakInfo(lang string) costing.PeakInfo {
	return costing.DeepSeekPeakInfoNow(lang, time.Now(), time.Local)
}

func (a *App) GetSettings() map[string]any {
	s := a.cfg.Get()
	provider := a.cfg.Provider()
	appData, _ := a.cfg.AppDataDir()
	return map[string]any{
		"activeProvider":     provider,
		"deepseekModel":      s.DeepSeekModel,
		"deepseekProRetired": appmeta.DeepSeekProRetired(time.Now()),
		"deepseekKeySet":     s.DeepSeekAPIKey != "",
		"zaiModel":           orDefault(s.ZaiModel, zai.DefaultModel),
		"zaiKeySet":          s.ZaiAPIKey != "",
		"zaiEndpoint":        a.cfg.ZaiEndpoint(),
		"openrouterModel":    orDefault(s.OpenRouterModel, openrouter.DefaultModel),
		"openrouterKeySet":   s.OpenRouterAPIKey != "",
		"shell":              s.Shell,
		"shellResolved":      shell.ResolveShell(s.Shell),
		"shellDetected":      shell.DetectDefaultShell(),
		"recentProjects":     s.RecentProjects,
		"showTerminal":       s.ShowTerminal,
		"showFiles":          a.cfg.FilesVisible(),
		"showSettings":       s.ShowSettings,
		"theme":              a.cfg.Theme(),
		"agentMaxSteps":      a.cfg.MaxAgentSteps(),
		"autoModels":         a.cfg.AutoModels(),
		"toolConfirm":        a.cfg.ToolConfirmEnabled(),
		"planMode":           a.cfg.PlanModeEnabled(),
		"visionModel":        deepseek.VisionModel,
		"appDataDir":         appData,
		"layoutProjectsW":    nonzero(s.LayoutProjectsW, 200),
		"layoutTreeW":        nonzero(s.LayoutTreeW, 220),
		"layoutSettingsW":    nonzero(s.LayoutSettingsW, 230),
		"layoutTerminalH":    nonzero(s.LayoutTerminalH, 160),
		"layoutComposerH":    nonzero(s.LayoutComposerH, 150),
		// 0 = fill the whole chat pane (no artificial max-width).
		"layoutChatMaxW": s.LayoutChatMaxW,
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

// allowedCursorRulePath returns true if abs is under a known Cursor rules directory
// (global ~/.cursor/rules or the open project's .cursor/rules).
func (a *App) allowedCursorRulePath(abs string) bool {
	abs = filepath.Clean(abs)
	a.rulesMu.RLock()
	b := a.cursorRules
	a.rulesMu.RUnlock()
	dirs := []string{}
	if b.GlobalDir != "" {
		dirs = append(dirs, filepath.Clean(b.GlobalDir))
	}
	if b.ProjectDir != "" {
		dirs = append(dirs, filepath.Clean(b.ProjectDir))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		rel, err := filepath.Rel(d, abs)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return true
		}
	}
	// Legacy single-file rules next to rules dirs / project root.
	for _, r := range append(append([]rules.Rule{}, b.Global...), b.Project...) {
		if filepath.Clean(filepath.FromSlash(r.Path)) == abs {
			return true
		}
	}
	return false
}

// ReadCursorRule returns the raw text of a Cursor rule file (absolute path).
func (a *App) ReadCursorRule(absPath string) (string, error) {
	abs := filepath.Clean(filepath.FromSlash(strings.TrimSpace(absPath)))
	if abs == "" || abs == "." {
		return "", fmt.Errorf("empty rule path")
	}
	if !a.allowedCursorRulePath(abs) {
		return "", fmt.Errorf("rule path not allowed")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if len(data) > 512*1024 {
		data = data[:512*1024]
	}
	return string(data), nil
}

// WriteCursorRule overwrites a Cursor rule file (absolute path) and reloads the cache.
func (a *App) WriteCursorRule(absPath, content string) error {
	abs := filepath.Clean(filepath.FromSlash(strings.TrimSpace(absPath)))
	if abs == "" || abs == "." {
		return fmt.Errorf("empty rule path")
	}
	if !a.allowedCursorRulePath(abs) {
		return fmt.Errorf("rule path not allowed")
	}
	ext := strings.ToLower(filepath.Ext(abs))
	base := strings.ToLower(filepath.Base(abs))
	if ext != ".mdc" && ext != ".md" && base != ".cursorrules" && base != "agents.md" {
		return fmt.Errorf("unsupported rule extension")
	}
	if len(content) > 512*1024 {
		return fmt.Errorf("rule too large")
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return err
	}
	a.loadRules()
	return nil
}

// UsageStats is the top-bar spend counter (active provider all-time + active chat).
// Typed struct (not map[string]any) so Wails/WebKit on macOS reliably
// delivers numeric fields to the frontend.
type UsageStats struct {
	Provider            string  `json:"provider"`
	CostUsd             float64 `json:"costUsd"`
	InputTokens         int     `json:"inputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	CacheHitTokens      int     `json:"cacheHitTokens"`
	CacheMissTokens     int     `json:"cacheMissTokens"`
	ChatCostUsd         float64 `json:"chatCostUsd"`
	ChatInputTokens     int     `json:"chatInputTokens"`
	ChatOutputTokens    int     `json:"chatOutputTokens"`
	ChatCacheHitTokens  int     `json:"chatCacheHitTokens"`
	ChatCacheMissTokens int     `json:"chatCacheMissTokens"`
	BalanceOk           bool    `json:"balanceOk"`
	BalanceUsd          float64 `json:"balanceUsd"`
	BalanceDetail       string  `json:"balanceDetail"`
}

// GetUsageStats returns spend for the active provider plus the active chat spend.
func (a *App) GetUsageStats() UsageStats {
	provider := a.cfg.Provider()
	cost, in, out, hit, miss := a.cfg.ProviderUsage(provider)
	outStats := UsageStats{
		Provider:        provider,
		CostUsd:         cost,
		InputTokens:     in,
		OutputTokens:    out,
		CacheHitTokens:  hit,
		CacheMissTokens: miss,
	}
	if provider == config.ProviderZAI {
		a.zaiBalMu.Lock()
		bal := a.zaiBal
		a.zaiBalMu.Unlock()
		outStats.BalanceOk = bal.OK
		outStats.BalanceUsd = bal.AvailableUSD
		outStats.BalanceDetail = bal.Detail
	}
	if provider == config.ProviderOpenRouter {
		a.orBalMu.Lock()
		bal := a.orBal
		a.orBalMu.Unlock()
		outStats.BalanceOk = bal.OK
		outStats.BalanceUsd = bal.AvailableUSD
		outStats.BalanceDetail = bal.Detail
	}
	if a.chats == nil {
		return outStats
	}
	a.mu.Lock()
	sid := a.sessionID
	a.mu.Unlock()
	if sid == "" {
		return outStats
	}
	if sess, err := a.chats.Get(a.projectKey(), sid); err == nil && sess != nil {
		chatCost, chatIn, chatOut, chatHit, chatMiss := sess.ProviderUsage(provider)
		outStats.ChatCostUsd = chatCost
		outStats.ChatInputTokens = chatIn
		outStats.ChatOutputTokens = chatOut
		outStats.ChatCacheHitTokens = chatHit
		outStats.ChatCacheMissTokens = chatMiss
	}
	return outStats
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

// SaveShell sets the interactive/one-shot shell executable path (empty = auto-detect).
func (a *App) SaveShell(path string) error {
	if err := a.cfg.SetShell(path); err != nil {
		return err
	}
	if a.tools != nil {
		a.tools.Shell = shell.ResolveShell(path)
	}
	// Restart interactive terminal so the new shell takes effect.
	if a.term != nil {
		root, _ := a.ws.ActiveRoot()
		a.ensureTerminal(root)
	}
	return nil
}

// DetectDefaultShell returns the OS-preferred shell path (pwsh/PS5 or $SHELL).
func (a *App) DetectDefaultShell() string {
	return shell.DetectDefaultShell()
}

func (a *App) SaveShowFiles(show bool) error {
	return a.cfg.SetShowFiles(show)
}

func (a *App) SaveShowSettings(show bool) error {
	return a.cfg.SetShowSettings(show)
}

// SaveLayoutSizes persists inner pane widths/heights (projects/tree/settings/terminal/chat column).
func (a *App) SaveLayoutSizes(projectsW, treeW, settingsW, terminalH, chatMaxW int) error {
	return a.cfg.SetLayoutSizes(projectsW, treeW, settingsW, terminalH, chatMaxW)
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
	if appmeta.DeepSeekProRetired(time.Now()) && strings.EqualFold(strings.TrimSpace(model), "deepseek-v4-pro") {
		// V4 Pro is retired: keep the user on V4 Flash.
		model = "deepseek-v4-flash"
	}
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

func (a *App) SaveOpenRouterKey(apiKey string) error {
	if err := a.cfg.SetOpenRouterAPIKey(apiKey); err != nil {
		return err
	}
	a.refreshProvider()
	return nil
}

func (a *App) SaveOpenRouterModel(model string) error {
	if err := a.cfg.SetOpenRouterModel(model); err != nil {
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

// PreferZaiModel keeps the saved model when it is still available; otherwise
// prefers free-tier, then the first id in the list.
func (a *App) PreferZaiModel(available []string) string {
	return zai.PreferModel(available, a.cfg.Get().ZaiModel)
}

// GetZaiBalance best-effort remaining credits / Coding Plan quota for the saved key.
func (a *App) GetZaiBalance() zai.AccountBalance {
	key := a.cfg.Get().ZaiAPIKey
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	bal := zai.FetchBalance(ctx, key, a.cfg.ZaiEndpoint())
	a.zaiBalMu.Lock()
	a.zaiBal = bal
	a.zaiBalMu.Unlock()
	return bal
}

// ListOpenRouterModels returns curated + coding tool models from OpenRouter.
func (a *App) ListOpenRouterModels() []string {
	key := a.cfg.Get().OpenRouterAPIKey
	if key == "" {
		return openrouter.OrderModels(openrouter.FallbackModels())
	}
	client := openrouter.New(key, a.cfg.Get().OpenRouterModel)
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	items, err := client.ListModels(ctx)
	if err != nil || len(items) == 0 {
		return openrouter.OrderModels(openrouter.FallbackModels())
	}
	return openrouter.OrderModels(openrouter.MergeCurated(openrouter.FilterCodingModels(items, 40)))
}

// PreferOpenRouterModel keeps the saved model when still available; else default flash.
func (a *App) PreferOpenRouterModel(available []string) string {
	return openrouter.PreferModel(available, a.cfg.Get().OpenRouterModel)
}

// GetOpenRouterBalance best-effort remaining prepaid credits for the saved key.
func (a *App) GetOpenRouterBalance() openrouter.AccountBalance {
	key := a.cfg.Get().OpenRouterAPIKey
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	bal := openrouter.FetchBalance(ctx, key)
	a.orBalMu.Lock()
	a.orBal = bal
	a.orBalMu.Unlock()
	return bal
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

func (a *App) SaveToolConfirm(on bool) error {
	return a.cfg.SetToolConfirm(on)
}

func (a *App) SavePlanMode(on bool) error {
	return a.cfg.SetPlanMode(on)
}

// SetIDEContext records the file open in the in-app editor for the next agent turn.
func (a *App) SetIDEContext(path string, line int) {
	a.ideMu.Lock()
	a.ideFile = strings.TrimSpace(path)
	a.ideLine = line
	a.ideMu.Unlock()
}

func (a *App) ideContextText() string {
	a.ideMu.Lock()
	path := a.ideFile
	line := a.ideLine
	a.ideMu.Unlock()
	if path == "" {
		return ""
	}
	if line <= 0 {
		return "active_file=" + path
	}
	return fmt.Sprintf("active_file=%s\ncursor_line=%d", path, line)
}

// ResolveToolApproval answers a pending tool_ask (HITL) for the agent loop.
func (a *App) ResolveToolApproval(sessionID, callID string, allow bool) {
	key := strings.TrimSpace(sessionID) + "/" + strings.TrimSpace(callID)
	a.approveMu.Lock()
	ch := a.pendingApprove[key]
	delete(a.pendingApprove, key)
	a.approveMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- allow:
	default:
	}
}

// ResolveUserAsk answers a pending ask_user tool call.
func (a *App) ResolveUserAsk(sessionID, callID, answer string) {
	key := strings.TrimSpace(sessionID) + "/" + strings.TrimSpace(callID)
	a.approveMu.Lock()
	ch := a.pendingAsk[key]
	delete(a.pendingAsk, key)
	a.approveMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- answer:
	default:
	}
}

func (a *App) waitToolApproval(ctx context.Context, sessionID, callID, name, argsJSON string) (bool, error) {
	key := strings.TrimSpace(sessionID) + "/" + strings.TrimSpace(callID)
	ch := make(chan bool, 1)
	a.approveMu.Lock()
	if old := a.pendingApprove[key]; old != nil {
		select {
		case old <- false:
		default:
		}
	}
	a.pendingApprove[key] = ch
	a.approveMu.Unlock()
	defer func() {
		a.approveMu.Lock()
		if a.pendingApprove[key] == ch {
			delete(a.pendingApprove, key)
		}
		a.approveMu.Unlock()
	}()

	_ = name
	_ = argsJSON
	select {
	case allow := <-ch:
		return allow, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (a *App) waitUserAsk(ctx context.Context, sessionID, callID, question string, options []string) (string, error) {
	key := strings.TrimSpace(sessionID) + "/" + strings.TrimSpace(callID)
	ch := make(chan string, 1)
	a.approveMu.Lock()
	if old := a.pendingAsk[key]; old != nil {
		select {
		case old <- "":
		default:
		}
	}
	a.pendingAsk[key] = ch
	a.approveMu.Unlock()
	defer func() {
		a.approveMu.Lock()
		if a.pendingAsk[key] == ch {
			delete(a.pendingAsk, key)
		}
		a.approveMu.Unlock()
	}()
	_ = question
	_ = options
	select {
	case ans := <-ch:
		if strings.TrimSpace(ans) == "" {
			return "", fmt.Errorf("user dismissed the question")
		}
		return ans, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
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
	_ = a.cfg.SetLastProject(p.Path)
	a.loadRules()
	return p, nil
}

// ProjectHasChats reports whether the project has non-empty chat sessions on disk.
func (a *App) ProjectHasChats(path string) (bool, error) {
	if a.chats == nil {
		return false, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	return a.chats.HasMeaningfulChats(abs)
}

// CloseProject removes a project from the open list.
// chatAction: "" (no chat file / already handled), "delete", or "archive".
// Returns the newly active project (may be nil).
func (a *App) CloseProject(path, chatAction string) (*workspace.Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	wasActive := false
	if root, err := a.ws.ActiveRoot(); err == nil && root == abs {
		wasActive = true
		a.StopAgentSession("")
	}
	switch strings.ToLower(strings.TrimSpace(chatAction)) {
	case "delete":
		if a.chats != nil {
			if err := a.chats.DeleteBundle(abs); err != nil {
				return nil, err
			}
		}
	case "archive":
		if a.chats != nil {
			if _, err := a.chats.ArchiveAllSessions(abs); err != nil {
				return nil, err
			}
		}
	case "", "none", "keep":
		// leave chat files on disk
	default:
		return nil, fmt.Errorf("unknown chat action: %s", chatAction)
	}
	next, err := a.ws.Close(abs)
	if err != nil {
		return nil, err
	}
	_ = a.cfg.RemoveRecentProject(abs)
	if wasActive {
		a.mu.Lock()
		a.history = nil
		a.sessionID = ""
		a.mu.Unlock()
		if next != nil {
			a.loadRules()
			_ = a.loadActiveIntoMemory()
		} else {
			a.loadRules()
		}
	}
	return next, nil
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

// ListArchivedChats returns chats from %AppData%/Roaming/NotCursor/chat_archive.
func (a *App) ListArchivedChats() ([]chatstore.ArchivedChat, error) {
	if a.chats == nil {
		return nil, nil
	}
	return a.chats.ListArchived()
}

// AppDataDir returns the app data root (Windows: %AppData%\Roaming\NotCursor).
func (a *App) AppDataDir() (string, error) {
	return a.cfg.AppDataDir()
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
	// The session must be the one requested: chatstore no longer falls back
	// silently, but double-check here as a hard barrier.
	if sess.ID != sessionID {
		return fmt.Errorf("session id mismatch: %s != %s", sess.ID, sessionID)
	}
	// Race protection: an empty transcript (e.g. while switching projects)
	// must never overwrite existing history.
	if !isMeaningfulItemsJSON(itemsJSON) && isMeaningfulItemsJSON(sess.ItemsJSON) {
		return nil
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

func isMeaningfulItemsJSON(s string) bool {
	t := strings.TrimSpace(s)
	return t != "" && t != "[]" && t != "null"
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
	denyPending := func(sid string) {
		a.approveMu.Lock()
		defer a.approveMu.Unlock()
		for key, ch := range a.pendingApprove {
			if sid != "" && !strings.HasPrefix(key, sid+"/") {
				continue
			}
			select {
			case ch <- false:
			default:
			}
			delete(a.pendingApprove, key)
		}
		for key, ch := range a.pendingAsk {
			if sid != "" && !strings.HasPrefix(key, sid+"/") {
				continue
			}
			select {
			case ch <- "":
			default:
			}
			delete(a.pendingAsk, key)
		}
	}
	if sessionID == "" {
		for id, c := range a.cancels {
			if c != nil {
				c()
			}
			delete(a.cancels, id)
		}
		denyPending("")
		return
	}
	if c := a.cancels[sessionID]; c != nil {
		c()
		delete(a.cancels, sessionID)
	}
	denyPending(sessionID)
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
		case config.ProviderOpenRouter:
			return fmt.Errorf("OpenRouter API key is not set")
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
	a.runGens[sid]++
	gen := a.runGens[sid]
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
	autoModels := cfg.AutoModels

	a.mu.Lock()
	sticky := a.sessionStickyModel[sid]
	a.mu.Unlock()

	projectMap := ""
	if tree, err := a.ws.ProjectTree(350); err == nil {
		projectMap = tree
	}

	runner := &agent.Runner{
		Provider:       a.llm,
		Tools:          a.tools,
		MaxSteps:       a.cfg.MaxAgentSteps(),
		RulesText:      bundle.SelectStableForPrompt(),
		TurnRulesText:  bundle.SelectTurnRules(hints),
		StickyModel:    sticky,
		ProjectMap:     projectMap,
		IDEContext:     a.ideContextText(),
		PlanMode:       a.cfg.PlanModeEnabled(),
		AutoModels:     autoModels,
		ProviderID:     provider,
		PreferredModel: a.cfg.ActiveModel(),
		UserText:       userMessage,
		HasImages:      hasImages,
		HintPathCount:  len(hints),
	}
	// ApproveTool is always wired; it decides per call whether a prompt is
	// needed, so toggling "Confirm dangerous tools" applies immediately.
	runner.ApproveTool = func(ctx context.Context, callID, name, argsJSON string) (bool, error) {
		if !a.cfg.ToolConfirmEnabled() {
			return true, nil
		}
		a.emit(agent.Event{Type: "tool_ask", Name: name, Content: argsJSON, CallID: callID})
		return a.waitToolApproval(ctx, sid, callID, name, argsJSON)
	}
	if a.tools != nil {
		a.tools.AskUser = func(ctx context.Context, callID, question string, options []string) (string, error) {
			payload, _ := json.Marshal(map[string]any{"question": question, "options": options})
			a.emitFor(sid, agent.Event{Type: "tool_ask", Name: "ask_user", Content: string(payload), CallID: callID})
			return a.waitUserAsk(ctx, sid, callID, question, options)
		}
	}

	var costUSD float64
	var inTokens, outTokens, cacheHit, cacheMiss int
	runner.OnUsage = func(model string, u *llm.Usage) {
		if u == nil {
			return
		}
		costUSD += costing.Cost(model, u)
		inTokens += costing.InputTokens(u)
		outTokens += max(u.CompletionTokens, 0)
		cacheHit += max(u.PromptCacheHitTokens, 0)
		cacheMiss += max(u.PromptCacheMissTokens, 0)
		// Live preview in the top bar (provider totals not yet persisted until the run ends).
		baseCost, baseIn, baseOut, baseHit, baseMiss := a.cfg.ProviderUsage(provider)
		chatCost, chatIn, chatOut, chatHit, chatMiss := 0.0, 0, 0, 0, 0
		if a.chats != nil {
			if sess, err := a.chats.Get(a.projectKey(), sid); err == nil && sess != nil {
				chatCost, chatIn, chatOut, chatHit, chatMiss = sess.ProviderUsage(provider)
			}
		}
		balOK, balUSD, balDetail := a.usageBalanceSnapshot(provider)
		a.emitFor(sid, agent.Event{Type: "usage", Content: usagePayload(
			provider,
			baseCost+costUSD, baseIn+inTokens, baseOut+outTokens, baseHit+cacheHit, baseMiss+cacheMiss,
			chatCost+costUSD, chatIn+inTokens, chatOut+outTokens, chatHit+cacheHit, chatMiss+cacheMiss,
			balOK, balUSD, balDetail,
		)})
	}

	go func() {
		dockicon.BeginAgent()
		defer dockicon.EndAgent()

		stillCurrent := func() bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			return a.runGens[sid] == gen
		}

		emit := func(evt agent.Event) {
			if !stillCurrent() {
				return
			}
			if evt.Type == "model" && evt.Content != "" {
				_ = a.cfg.SetActiveModel(evt.Content)
				a.refreshProvider()
				a.logAgentLine(fmt.Sprintf("model=%s session=%s reason=%q", evt.Content, sid, evt.Name))
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
		current := a.runGens[sid] == gen
		if current {
			if a.sessionID == sid && err == nil {
				a.history = newHist
			}
			delete(a.cancels, sid)
		}
		a.mu.Unlock()
		if !current {
			return
		}

		// Persist LLM history only on success; on error keep the previous state
		// so a retry does not duplicate the failed user message.
		if a.chats != nil && err == nil {
			if sess, gerr := a.chats.Get(a.projectKey(), sid); gerr == nil {
				sess.History = newHist
				_ = a.chats.SaveSession(a.projectKey(), sess)
			}
		}
		if inTokens+outTokens > 0 || cacheHit+cacheMiss > 0 {
			_ = a.cfg.AddUsage(provider, costUSD, inTokens, outTokens, cacheHit, cacheMiss)
			if a.chats != nil {
				_, _ = a.chats.AddUsage(a.projectKey(), sid, provider, costUSD, inTokens, outTokens, cacheHit, cacheMiss)
			}
		}
		if locked := runner.LockedModel(); locked != "" && !agent.IsVisionModel(locked) {
			a.mu.Lock()
			a.sessionStickyModel[sid] = locked
			a.mu.Unlock()
		}
		totCost, totIn, totOut, totHit, totMiss := a.cfg.ProviderUsage(provider)
		chatCost, chatIn, chatOut, chatHit, chatMiss := 0.0, 0, 0, 0, 0
		if a.chats != nil {
			if sess, gerr := a.chats.Get(a.projectKey(), sid); gerr == nil && sess != nil {
				chatCost, chatIn, chatOut, chatHit, chatMiss = sess.ProviderUsage(provider)
			}
		}
		balOK, balUSD, balDetail := a.usageBalanceSnapshot(provider)
		a.emitFor(sid, agent.Event{Type: "usage", Content: usagePayload(
			provider,
			totCost, totIn, totOut, totHit, totMiss,
			chatCost, chatIn, chatOut, chatHit, chatMiss,
			balOK, balUSD, balDetail,
		)})
		a.emitFor(sid, agent.Event{Type: "persist", Content: "1"})
	}()
	return nil
}

func usagePayload(provider string, totalCost float64, totalIn, totalOut, totalHit, totalMiss int, chatCost float64, chatIn, chatOut, chatHit, chatMiss int, balOK bool, balUSD float64, balDetail string) string {
	b, _ := json.Marshal(map[string]any{
		"provider":            provider,
		"costUsd":             totalCost,
		"inputTokens":         totalIn,
		"outputTokens":        totalOut,
		"cacheHitTokens":      totalHit,
		"cacheMissTokens":     totalMiss,
		"chatCostUsd":         chatCost,
		"chatInputTokens":     chatIn,
		"chatOutputTokens":    chatOut,
		"chatCacheHitTokens":  chatHit,
		"chatCacheMissTokens": chatMiss,
		"balanceOk":           balOK,
		"balanceUsd":          balUSD,
		"balanceDetail":       balDetail,
	})
	return string(b)
}

func (a *App) usageBalanceSnapshot(provider string) (ok bool, usd float64, detail string) {
	switch provider {
	case config.ProviderZAI:
		a.zaiBalMu.Lock()
		bal := a.zaiBal
		a.zaiBalMu.Unlock()
		return bal.OK, bal.AvailableUSD, bal.Detail
	case config.ProviderOpenRouter:
		a.orBalMu.Lock()
		bal := a.orBal
		a.orBalMu.Unlock()
		return bal.OK, bal.AvailableUSD, bal.Detail
	default:
		return false, 0, ""
	}
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
		case config.ProviderOpenRouter:
			return "", fmt.Errorf("OpenRouter API key is not set")
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
	msg := resp.Choices[0].Message
	if s := strings.TrimSpace(msg.Content); s != "" {
		return s, nil
	}
	// Thinking-only replies (common on GLM free flash with low max_tokens).
	if a.cfg.Provider() == config.ProviderZAI {
		return "ok", nil
	}
	return "", nil
}

// --- terminal ---

func (a *App) ensureTerminal(cwd string) {
	if a.term != nil {
		a.term.Stop()
	}
	shellPath := shell.ResolveShell(a.cfg.Get().Shell)
	if a.tools != nil {
		a.tools.Shell = shellPath
	}
	a.term = shell.NewSession(cwd, shellPath, a.emitTerm)
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

func (a *App) TerminalResize(cols, rows int) error {
	if a.term == nil {
		return nil
	}
	return a.term.Resize(cols, rows)
}

func (a *App) RunShell(command string) (map[string]any, error) {
	root, _ := a.ws.ActiveRoot()
	shellPath := shell.ResolveShell(a.cfg.Get().Shell)
	res, err := shell.Run(a.ctx, command, root, shellPath, 0)
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
