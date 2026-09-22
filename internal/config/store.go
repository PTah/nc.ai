package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"notcursor.ai/app/internal/costing"
	"notcursor.ai/app/internal/fsx"
	"notcursor.ai/app/internal/secrets"
)

const appDirName = "NotCursor"

// Provider ids persisted in Settings.ActiveProvider.
const (
	ProviderDeepSeek   = "deepseek"
	ProviderZAI        = "zai"
	ProviderOpenRouter = "openrouter"
	ProviderQwen       = "qwen"
	ProviderLocal      = "local"
)

// Qwen (DashScope) endpoints persisted in Settings.QwenEndpoint.
const (
	QwenEndpointIntl = "intl"
	QwenEndpointCn   = "cn"
)

// DashScope OpenAI-compatible roots (kept in sync with internal/llm/providers/qwen).
const (
	QwenIntlBaseURL = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	QwenCnBaseURL   = "https://dashscope.aliyuncs.com/compatible-mode/v1"
)

// DefaultQwenBaseURL is the international DashScope compatible-mode root.
const DefaultQwenBaseURL = QwenIntlBaseURL

// qwenBaseURLFor maps a stored region id to the compatible-mode root.
func qwenBaseURLFor(endpoint string) string {
	if normalizeQwenEndpoint(endpoint) == QwenEndpointCn {
		return QwenCnBaseURL
	}
	return QwenIntlBaseURL
}

// DefaultLocalBaseURL is the Ollama OpenAI-compatible root on localhost.
const DefaultLocalBaseURL = "http://127.0.0.1:11434/v1"

// DefaultAgentStallMin — сколько минут тишины на шаге терпим по умолчанию,
// прежде чем оборвать прогон (0 в настройках = это значение, отрицательное = выкл).
const DefaultAgentStallMin = 10

// DefaultAgentMaxSteps caps the tool-using agent loop when the user has not set
// a value. Real diagnostics jobs (scan several switches, grep a big tree) need
// far more than the earlier 40/80 steps.
const DefaultAgentMaxSteps = 120

// DefaultAgentWarnSteps is when a long run starts telling the user that it is
// still working and what it is doing.
const DefaultAgentWarnSteps = 120

type Settings struct {
	// ActiveProvider selects which LLM backend is used ("deepseek" | "zai" | "openrouter" | "local").
	ActiveProvider string `json:"activeProvider,omitempty"`

	DeepSeekAPIKey string `json:"deepseekApiKey"`
	DeepSeekModel  string `json:"deepseekModel"`

	ZaiAPIKey string `json:"zaiApiKey,omitempty"`
	ZaiModel  string `json:"zaiModel,omitempty"`
	// ZaiEndpoint: "paas" (pay-as-you-go, default) or "coding" (GLM Coding Plan).
	ZaiEndpoint string `json:"zaiEndpoint,omitempty"`

	OpenRouterAPIKey string `json:"openrouterApiKey,omitempty"`
	OpenRouterModel  string `json:"openrouterModel,omitempty"`

	QwenAPIKey string `json:"qwenApiKey,omitempty"`
	QwenModel  string `json:"qwenModel,omitempty"`
	// QwenEndpoint: "intl" (international, default) or "cn" (mainland China).
	QwenEndpoint string `json:"qwenEndpoint,omitempty"`

	// LocalBaseURL / LocalModel are legacy flat fields kept in sync with the
	// active (or first) entry in LocalEndpoints for older settings.json readers.
	LocalBaseURL string `json:"localBaseUrl,omitempty"`
	LocalModel   string `json:"localModel,omitempty"`
	// LocalAPIKey is legacy; live tokens live in the OS secret store under "local" / "local:<id>".
	LocalAPIKey string `json:"localApiKey,omitempty"`
	// LocalEndpoints is the list of OpenAI-compatible LAN/local servers.
	LocalEndpoints []LocalEndpoint `json:"localEndpoints,omitempty"`

	Shell          string   `json:"shell"`
	RecentProjects []string `json:"recentProjects"`
	LastProject    string   `json:"lastProject,omitempty"`

	// LastRunVersion + CleanExit let the next launch explain a (re)start:
	// a version change means "update", an unclean exit means a crash/kill.
	LastRunVersion string `json:"lastRunVersion,omitempty"`
	CleanExit      bool   `json:"cleanExit,omitempty"`
	GitUsername    string `json:"gitUsername"`
	GitPassword    string `json:"gitPassword"`
	SSHUser        string `json:"sshUser"`
	ShowTerminal   bool   `json:"showTerminal"`
	// ShowFiles: nil = default true (visible). Explicit false remembers Hide files.
	ShowFiles *bool `json:"showFiles,omitempty"`
	// EndSound: nil = default true (play the chime when the agent finishes).
	// Explicit false remembers "Звук в конце работы" turned off.
	EndSound *bool `json:"endSound,omitempty"`
	// ShowSettings remembers the right settings pane visibility.
	ShowSettings bool `json:"showSettings"`

	// Theme: "dark" (default) or "light".
	Theme string `json:"theme,omitempty"`

	// UiFont: interface (sans) font id — "default" | "verdana" | "tahoma" | "arial" | "system".
	UiFont string `json:"uiFont,omitempty"`
	// MonoFont: monospace font id — "default" | "lucida" | "consolas" | "courier" | "cascadia".
	MonoFont string `json:"monoFont,omitempty"`

	// AgentMaxSteps caps the tool-using agent loop. 0 means the default
	// (config.DefaultAgentMaxSteps), negative means "no cap" — the run then
	// only warns after AgentWarnSteps.
	AgentMaxSteps int `json:"agentMaxSteps,omitempty"`

	// AgentWarnSteps is the step count after which a long run emits a warning
	// notice (0 = default, negative = never warn).
	AgentWarnSteps int `json:"agentWarnSteps,omitempty"`

	// AgentStallMin — сколько минут тишины на шаге терпим, прежде чем оборвать
	// прогон с разбором. 0 = по умолчанию (DefaultAgentStallMin), -1 = выключено.
	AgentStallMin int `json:"agentStallMin,omitempty"`

	// AutoModels enables per-turn model routing (DeepSeek flash/pro/vision;
	// Z.ai free flash → glm-5.3; OpenRouter flash → coder on complex tasks).
	AutoModels bool `json:"autoModels,omitempty"`

	// ToolConfirm asks the user before dangerous tools (write_file, run_terminal,
	// git_push, ssh_exec). Default true when unset on first run — see ToolConfirmEnabled.
	ToolConfirm *bool `json:"toolConfirm,omitempty"`

	// PlanMode: explore-only (no writes/shell/git push/ssh).
	PlanMode bool `json:"planMode,omitempty"`

	// LocalLite: short system prompt + reduced tools for weak local LLMs.
	// Default false — full prompt/tools work better on capable coder models (7B+).
	// Nil = unset → off (see LocalLiteEnabled).
	LocalLite *bool `json:"localLite,omitempty"`

	// Main window geometry (logical pixels). Zero width/height → defaults.
	WindowWidth     int  `json:"windowWidth,omitempty"`
	WindowHeight    int  `json:"windowHeight,omitempty"`
	WindowX         int  `json:"windowX,omitempty"`
	WindowY         int  `json:"windowY,omitempty"`
	WindowPosSet    bool `json:"windowPosSet,omitempty"`
	WindowMaximised bool `json:"windowMaximised,omitempty"`

	// Inner layout sizes (px). Zero → CSS defaults.
	LayoutProjectsW int `json:"layoutProjectsW,omitempty"`
	LayoutTreeW     int `json:"layoutTreeW,omitempty"`
	LayoutSettingsW int `json:"layoutSettingsW,omitempty"`
	LayoutTerminalH int `json:"layoutTerminalH,omitempty"`
	LayoutComposerH int `json:"layoutComposerH,omitempty"`
	LayoutChatMaxW  int `json:"layoutChatMaxW,omitempty"`

	// All-time API usage counters (legacy combined + per-provider).
	TotalCostUSD         float64 `json:"totalCostUsd"`
	TotalInputTokens     int     `json:"totalInputTokens"`
	TotalOutputTokens    int     `json:"totalOutputTokens"`
	TotalCacheHitTokens  int     `json:"totalCacheHitTokens,omitempty"`
	TotalCacheMissTokens int     `json:"totalCacheMissTokens,omitempty"`

	DeepSeekCostUSD         float64 `json:"deepseekCostUsd,omitempty"`
	DeepSeekInputTokens     int     `json:"deepseekInputTokens,omitempty"`
	DeepSeekOutputTokens    int     `json:"deepseekOutputTokens,omitempty"`
	DeepSeekCacheHitTokens  int     `json:"deepseekCacheHitTokens,omitempty"`
	DeepSeekCacheMissTokens int     `json:"deepseekCacheMissTokens,omitempty"`

	ZaiCostUSD         float64 `json:"zaiCostUsd,omitempty"`
	ZaiInputTokens     int     `json:"zaiInputTokens,omitempty"`
	ZaiOutputTokens    int     `json:"zaiOutputTokens,omitempty"`
	ZaiCacheHitTokens  int     `json:"zaiCacheHitTokens,omitempty"`
	ZaiCacheMissTokens int     `json:"zaiCacheMissTokens,omitempty"`

	OpenRouterCostUSD         float64 `json:"openrouterCostUsd,omitempty"`
	OpenRouterInputTokens     int     `json:"openrouterInputTokens,omitempty"`
	OpenRouterOutputTokens    int     `json:"openrouterOutputTokens,omitempty"`
	OpenRouterCacheHitTokens  int     `json:"openrouterCacheHitTokens,omitempty"`
	OpenRouterCacheMissTokens int     `json:"openrouterCacheMissTokens,omitempty"`

	QwenCostUSD         float64 `json:"qwenCostUsd,omitempty"`
	QwenInputTokens     int     `json:"qwenInputTokens,omitempty"`
	QwenOutputTokens    int     `json:"qwenOutputTokens,omitempty"`
	QwenCacheHitTokens  int     `json:"qwenCacheHitTokens,omitempty"`
	QwenCacheMissTokens int     `json:"qwenCacheMissTokens,omitempty"`

	LocalCostUSD         float64 `json:"localCostUsd,omitempty"`
	LocalInputTokens     int     `json:"localInputTokens,omitempty"`
	LocalOutputTokens    int     `json:"localOutputTokens,omitempty"`
	LocalCacheHitTokens  int     `json:"localCacheHitTokens,omitempty"`
	LocalCacheMissTokens int     `json:"localCacheMissTokens,omitempty"`

	// LastSeenVersion is the app version for which Welcome was already shown.
	LastSeenVersion string `json:"lastSeenVersion,omitempty"`

	// Cached official price sheets (weekly refresh from provider docs).
	DeepSeekPeakPrices        map[string]costing.Prices `json:"deepseekPeakPrices,omitempty"`
	DeepSeekPeakWindows       []costing.PeakWindow      `json:"deepseekPeakWindows,omitempty"`
	DeepSeekPricesCheckedAt   string                    `json:"deepseekPricesCheckedAt,omitempty"` // RFC3339 UTC
	ZaiPrices                 map[string]costing.Prices `json:"zaiPrices,omitempty"`
	ZaiPricesCheckedAt        string                    `json:"zaiPricesCheckedAt,omitempty"`
	OpenRouterPrices          map[string]costing.Prices `json:"openrouterPrices,omitempty"`
	OpenRouterPricesCheckedAt string                    `json:"openrouterPricesCheckedAt,omitempty"`
	// DeepSeekModelsSeen is the last catalog returned by GET /models; used to
	// notify the chat when DeepSeek adds or retires models.
	DeepSeekModelsSeen []string `json:"deepSeekModelsSeen,omitempty"`
}

// MarshalJSON never writes API keys or git passwords to disk.
func (s Settings) MarshalJSON() ([]byte, error) {
	type persist Settings
	p := persist(s)
	p.DeepSeekAPIKey = ""
	p.ZaiAPIKey = ""
	p.OpenRouterAPIKey = ""
	p.QwenAPIKey = ""
	p.LocalAPIKey = ""
	p.GitPassword = ""
	return json.Marshal(p)
}

type Store struct {
	mu       sync.RWMutex
	settings Settings
	path     string
	baseDir  string
	sec      *secrets.Store
	keys     map[string]string
	fileOnly bool
}

func NewStore() *Store {
	return &Store{
		settings: Settings{
			ActiveProvider:  ProviderDeepSeek,
			DeepSeekModel:   "deepseek-flash",
			ZaiModel:        "glm-4.7-flash",
			ZaiEndpoint:     "paas",
			OpenRouterModel: "qwen/qwen3-coder-flash:floor",
			QwenModel:       "qwen-plus",
			QwenEndpoint:    QwenEndpointIntl,
			LocalBaseURL:    DefaultLocalBaseURL,
			Shell:           "",
			AgentMaxSteps:   DefaultAgentMaxSteps,
			AgentWarnSteps:  DefaultAgentWarnSteps,
			AgentStallMin:   DefaultAgentStallMin,
			AutoModels:      true,
		},
		keys: map[string]string{},
	}
}

// NewStoreForTest writes settings + secrets under dir and never uses the OS vault.
func NewStoreForTest(dir string) *Store {
	s := NewStore()
	s.baseDir = dir
	s.path = filepath.Join(dir, "settings.json")
	s.fileOnly = true
	return s
}

func (s *Store) AppDataDir() (string, error) {
	if strings.TrimSpace(s.baseDir) != "" {
		if err := os.MkdirAll(s.baseDir, 0o700); err != nil {
			return "", err
		}
		return s.baseDir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Store) SSHDir() (string, error) {
	base, err := s.AppDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Store) configPath() (string, error) {
	if s.path != "" {
		return s.path, nil
	}
	base, err := s.AppDataDir()
	if err != nil {
		return "", err
	}
	s.path = filepath.Join(base, "settings.json")
	return s.path, nil
}

func (s *Store) Load() error {
	path, err := s.configPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.Save()
		}
		return err
	}
	s.mu.Lock()
	if err := json.Unmarshal(data, &s.settings); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.settings.ActiveProvider == "" {
		s.settings.ActiveProvider = ProviderDeepSeek
	} else {
		s.settings.ActiveProvider = normalizeProvider(s.settings.ActiveProvider)
	}
	if s.settings.ZaiModel == "" {
		s.settings.ZaiModel = "glm-4.7-flash"
	}
	if s.settings.ZaiEndpoint == "" {
		s.settings.ZaiEndpoint = "paas"
	} else {
		s.settings.ZaiEndpoint = normalizeZaiEndpoint(s.settings.ZaiEndpoint)
	}
	if s.settings.DeepSeekModel == "" {
		s.settings.DeepSeekModel = "deepseek-flash"
	}
	if s.settings.OpenRouterModel == "" {
		s.settings.OpenRouterModel = "qwen/qwen3-coder-flash:floor"
	}
	if s.settings.QwenModel == "" {
		s.settings.QwenModel = "qwen-plus"
	}
	s.settings.QwenEndpoint = normalizeQwenEndpoint(s.settings.QwenEndpoint)
	endpointsChanged := s.ensureLocalEndpointsLocked()
	// Legacy default was the bare name "powershell"; empty now means auto-detect (pwsh → PS5).
	if strings.EqualFold(strings.TrimSpace(s.settings.Shell), "powershell") {
		s.settings.Shell = ""
	}
	// Migrate pre-multi-provider totals into DeepSeek bucket once.
	if s.settings.DeepSeekCostUSD == 0 && s.settings.DeepSeekInputTokens == 0 &&
		s.settings.DeepSeekOutputTokens == 0 && s.settings.ZaiCostUSD == 0 &&
		(s.settings.TotalCostUSD != 0 || s.settings.TotalInputTokens != 0 || s.settings.TotalOutputTokens != 0) {
		s.settings.DeepSeekCostUSD = s.settings.TotalCostUSD
		s.settings.DeepSeekInputTokens = s.settings.TotalInputTokens
		s.settings.DeepSeekOutputTokens = s.settings.TotalOutputTokens
	}
	if err := s.ensureSecretsLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	migrated := s.migrateLegacyKeysLocked() || endpointsChanged
	s.mu.Unlock()
	if migrated {
		return s.Save()
	}
	return nil
}

func (s *Store) Save() error {
	path, err := s.configPath()
	if err != nil {
		return err
	}
	s.mu.RLock()
	data, err := json.MarshalIndent(&s.settings, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, data, 0o600)
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) SetDeepSeekAPIKey(key string) error {
	return s.setAPIKey(ProviderDeepSeek, key)
}

func (s *Store) SetDeepSeekModel(model string) error {
	s.mu.Lock()
	s.settings.DeepSeekModel = model
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetZaiAPIKey(key string) error {
	return s.setAPIKey(ProviderZAI, key)
}

func (s *Store) SetZaiModel(model string) error {
	s.mu.Lock()
	s.settings.ZaiModel = model
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetZaiEndpoint(endpoint string) error {
	s.mu.Lock()
	s.settings.ZaiEndpoint = normalizeZaiEndpoint(endpoint)
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetOpenRouterAPIKey(key string) error {
	return s.setAPIKey(ProviderOpenRouter, key)
}

func (s *Store) SetOpenRouterModel(model string) error {
	s.mu.Lock()
	s.settings.OpenRouterModel = model
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetQwenAPIKey(key string) error {
	return s.setAPIKey(ProviderQwen, key)
}

func (s *Store) SetQwenModel(model string) error {
	s.mu.Lock()
	s.settings.QwenModel = model
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetQwenEndpoint(endpoint string) error {
	s.mu.Lock()
	s.settings.QwenEndpoint = normalizeQwenEndpoint(endpoint)
	s.mu.Unlock()
	return s.Save()
}

// QwenEndpoint returns the normalized stored DashScope region.
func (s *Store) QwenEndpoint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeQwenEndpoint(s.settings.QwenEndpoint)
}

// QwenBaseURL returns the compatible-mode root for the stored region.
func (s *Store) QwenBaseURL() string {
	return qwenBaseURLFor(s.QwenEndpoint())
}

func normalizeQwenEndpoint(endpoint string) string {
	switch strings.TrimSpace(strings.ToLower(endpoint)) {
	case QwenEndpointCn:
		return QwenEndpointCn
	default:
		return QwenEndpointIntl
	}
}

func (s *Store) SetLocalAPIKey(key string) error {
	p := s.Provider()
	if !IsLocalProvider(p) {
		p = MakeLocalProvider(DefaultLocalEndpointID)
	}
	return s.setAPIKey(p, key)
}

func (s *Store) SetLocalModel(model string) error {
	s.mu.Lock()
	s.ensureLocalEndpointsLocked()
	model = strings.TrimSpace(model)
	if ep := s.activeLocalEndpointLocked(); ep != nil {
		ep.Model = model
	} else if len(s.settings.LocalEndpoints) > 0 {
		s.settings.LocalEndpoints[0].Model = model
	}
	s.syncLegacyLocalFieldsLocked()
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetLocalBaseURL(baseURL string) error {
	s.mu.Lock()
	s.ensureLocalEndpointsLocked()
	url := normalizeLocalBaseURL(baseURL)
	if ep := s.activeLocalEndpointLocked(); ep != nil {
		ep.BaseURL = url
	} else if len(s.settings.LocalEndpoints) > 0 {
		s.settings.LocalEndpoints[0].BaseURL = url
	}
	s.syncLegacyLocalFieldsLocked()
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) LocalBaseURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ep := s.activeLocalEndpointLocked(); ep != nil {
		return normalizeLocalBaseURL(ep.BaseURL)
	}
	if len(s.settings.LocalEndpoints) > 0 {
		return normalizeLocalBaseURL(s.settings.LocalEndpoints[0].BaseURL)
	}
	return normalizeLocalBaseURL(s.settings.LocalBaseURL)
}

func (s *Store) LocalModel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ep := s.activeLocalEndpointLocked(); ep != nil {
		return strings.TrimSpace(ep.Model)
	}
	if len(s.settings.LocalEndpoints) > 0 {
		return strings.TrimSpace(s.settings.LocalEndpoints[0].Model)
	}
	return strings.TrimSpace(s.settings.LocalModel)
}

func normalizeLocalBaseURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return DefaultLocalBaseURL
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	return strings.TrimRight(u, "/")
}

func (s *Store) ZaiEndpoint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeZaiEndpoint(s.settings.ZaiEndpoint)
}

func normalizeZaiEndpoint(e string) string {
	switch e {
	case "coding":
		return "coding"
	default:
		return "paas"
	}
}

// Provider returns the normalized active provider id.
func (s *Store) Provider() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeProvider(s.settings.ActiveProvider)
}

func (s *Store) SetActiveProvider(provider string) error {
	s.mu.Lock()
	p := normalizeProvider(provider)
	if IsLocalProvider(p) {
		s.ensureLocalEndpointsLocked()
		id := LocalEndpointID(p)
		if s.indexLocalEndpointLocked(id) < 0 && len(s.settings.LocalEndpoints) > 0 {
			p = MakeLocalProvider(s.settings.LocalEndpoints[0].ID)
		}
		s.syncLegacyLocalFieldsLocked()
	}
	s.settings.ActiveProvider = p
	s.mu.Unlock()
	return s.Save()
}

// ActiveAPIKey returns the API key for the active provider.
func (s *Store) ActiveAPIKey() string {
	return s.APIKey(s.Provider())
}

func (s *Store) APIKey(provider string) string {
	id := secretID(provider)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys == nil {
		s.keys = map[string]string{}
	}
	if v, ok := s.keys[id]; ok {
		return v
	}
	if err := s.ensureSecretsLocked(); err != nil {
		return ""
	}
	v, err := s.sec.Get(id)
	if err != nil {
		return ""
	}
	s.keys[id] = v
	return v
}

func (s *Store) HasAPIKey(provider string) bool {
	return strings.TrimSpace(s.APIKey(provider)) != ""
}

func (s *Store) ClearAPIKey(provider string) error {
	return s.setAPIKey(provider, "")
}

func (s *Store) ClearAllAPIKeys() error {
	var first error
	for _, id := range []string{ProviderDeepSeek, ProviderZAI, ProviderOpenRouter, ProviderQwen} {
		if err := s.setAPIKey(id, ""); err != nil && first == nil {
			first = err
		}
	}
	for _, ep := range s.LocalEndpoints() {
		if err := s.setAPIKey(MakeLocalProvider(ep.ID), ""); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *Store) SecretsBackendLabel() string {
	return secrets.BackendLabel()
}

func secretID(provider string) string {
	p := normalizeProvider(provider)
	switch {
	case p == ProviderZAI:
		return secrets.IDZai
	case p == ProviderOpenRouter:
		return secrets.IDOpenRouter
	case p == ProviderQwen:
		return secrets.IDQwen
	case IsLocalProvider(p):
		return LocalSecretID(LocalEndpointID(p))
	default:
		return secrets.IDDeepSeek
	}
}

func (s *Store) ensureSecretsLocked() error {
	if s.sec != nil {
		return nil
	}
	dir, err := s.AppDataDir()
	if err != nil {
		return err
	}
	if s.fileOnly {
		s.sec = secrets.OpenFileOnly(dir)
	} else {
		s.sec = secrets.Open(dir)
	}
	if s.keys == nil {
		s.keys = map[string]string{}
	}
	return nil
}

func (s *Store) migrateLegacyKeysLocked() bool {
	if s.sec == nil {
		return false
	}
	changed := false
	move := func(id, raw string, clear func()) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if err := s.sec.Set(id, raw); err != nil {
			return
		}
		s.keys[id] = raw
		clear()
		changed = true
	}
	move(secrets.IDDeepSeek, s.settings.DeepSeekAPIKey, func() { s.settings.DeepSeekAPIKey = "" })
	move(secrets.IDZai, s.settings.ZaiAPIKey, func() { s.settings.ZaiAPIKey = "" })
	move(secrets.IDOpenRouter, s.settings.OpenRouterAPIKey, func() { s.settings.OpenRouterAPIKey = "" })
	move(secrets.IDQwen, s.settings.QwenAPIKey, func() { s.settings.QwenAPIKey = "" })
	move(secrets.IDLocal, s.settings.LocalAPIKey, func() { s.settings.LocalAPIKey = "" })
	if strings.TrimSpace(s.settings.GitPassword) != "" {
		s.settings.GitPassword = ""
		changed = true
	}
	for _, id := range []string{secrets.IDDeepSeek, secrets.IDZai, secrets.IDOpenRouter, secrets.IDQwen, secrets.IDLocal} {
		if s.keys[id] != "" {
			continue
		}
		if v, err := s.sec.Get(id); err == nil && v != "" {
			s.keys[id] = v
		}
	}
	return changed
}

func (s *Store) setAPIKey(provider, key string) error {
	id := secretID(provider)
	key = strings.TrimSpace(key)
	s.mu.Lock()
	if err := s.ensureSecretsLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	var err error
	if key == "" {
		err = s.sec.Delete(id)
		delete(s.keys, id)
	} else {
		err = s.sec.Set(id, key)
		if err == nil {
			s.keys[id] = key
		}
	}
	s.settings.DeepSeekAPIKey = ""
	s.settings.ZaiAPIKey = ""
	s.settings.OpenRouterAPIKey = ""
	s.settings.QwenAPIKey = ""
	s.settings.LocalAPIKey = ""
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.Save()
}

// ActiveModel returns the model id for the active provider.
func (s *Store) ActiveModel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := normalizeProvider(s.settings.ActiveProvider)
	switch {
	case p == ProviderZAI:
		return s.settings.ZaiModel
	case p == ProviderOpenRouter:
		return s.settings.OpenRouterModel
	case p == ProviderQwen:
		if s.settings.QwenModel == "" {
			return "qwen-plus"
		}
		return s.settings.QwenModel
	case IsLocalProvider(p):
		if ep := s.activeLocalEndpointLocked(); ep != nil {
			return strings.TrimSpace(ep.Model)
		}
		return strings.TrimSpace(s.settings.LocalModel)
	default:
		return s.settings.DeepSeekModel
	}
}

// SetActiveModel persists the model for the currently active provider.
func (s *Store) SetActiveModel(model string) error {
	s.mu.Lock()
	p := normalizeProvider(s.settings.ActiveProvider)
	switch {
	case p == ProviderZAI:
		s.settings.ZaiModel = model
	case p == ProviderOpenRouter:
		s.settings.OpenRouterModel = model
	case p == ProviderQwen:
		s.settings.QwenModel = model
	case IsLocalProvider(p):
		model = strings.TrimSpace(model)
		if ep := s.activeLocalEndpointLocked(); ep != nil {
			ep.Model = model
		}
		s.syncLegacyLocalFieldsLocked()
	default:
		s.settings.DeepSeekModel = model
	}
	s.mu.Unlock()
	return s.Save()
}

func normalizeProvider(p string) string {
	p = strings.TrimSpace(strings.ToLower(p))
	switch {
	case p == ProviderZAI:
		return ProviderZAI
	case p == ProviderOpenRouter:
		return ProviderOpenRouter
	case p == ProviderQwen:
		return ProviderQwen
	case IsLocalProvider(p):
		return MakeLocalProvider(LocalEndpointID(p))
	default:
		return ProviderDeepSeek
	}
}

func (s *Store) AutoModels() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings.AutoModels
}

func (s *Store) SetAutoModels(on bool) error {
	s.mu.Lock()
	s.settings.AutoModels = on
	s.mu.Unlock()
	return s.Save()
}

// ToolConfirmEnabled reports whether dangerous tools require UI approval.
// Default is true when the setting was never persisted.
func (s *Store) ToolConfirmEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.ToolConfirm == nil {
		return true
	}
	return *s.settings.ToolConfirm
}

func (s *Store) SetToolConfirm(on bool) error {
	s.mu.Lock()
	v := on
	s.settings.ToolConfirm = &v
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) PlanModeEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings.PlanMode
}

func (s *Store) SetPlanMode(on bool) error {
	s.mu.Lock()
	s.settings.PlanMode = on
	s.mu.Unlock()
	return s.Save()
}

// LocalLiteEnabled reports whether Local uses the short prompt + reduced tools.
// Default false when unset (full agent works better on qwen-coder 7B/14B).
func (s *Store) LocalLiteEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.LocalLite == nil {
		return false
	}
	return *s.settings.LocalLite
}

func (s *Store) SetLocalLite(on bool) error {
	s.mu.Lock()
	v := on
	s.settings.LocalLite = &v
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetGitAuth(user, pass string) error {
	s.mu.Lock()
	s.settings.GitUsername = user
	s.settings.GitPassword = pass
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetShowTerminal(show bool) error {
	s.mu.Lock()
	s.settings.ShowTerminal = show
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetShell(path string) error {
	s.mu.Lock()
	s.settings.Shell = strings.TrimSpace(path)
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) FilesVisible() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.ShowFiles == nil {
		return true
	}
	return *s.settings.ShowFiles
}

func (s *Store) SetShowFiles(show bool) error {
	s.mu.Lock()
	v := show
	s.settings.ShowFiles = &v
	s.mu.Unlock()
	return s.Save()
}

// EndSoundEnabled reports whether the chime plays when the agent finishes.
// Default is true: the sound is on until the user turns it off.
func (s *Store) EndSoundEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.EndSound == nil {
		return true
	}
	return *s.settings.EndSound
}

func (s *Store) SetEndSound(on bool) error {
	s.mu.Lock()
	v := on
	s.settings.EndSound = &v
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetShowSettings(show bool) error {
	s.mu.Lock()
	s.settings.ShowSettings = show
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) LastSeenVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings.LastSeenVersion
}

func (s *Store) SetLastSeenVersion(ver string) error {
	s.mu.Lock()
	s.settings.LastSeenVersion = strings.TrimSpace(ver)
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) DeepSeekPeakPrices() map[string]costing.Prices {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePriceMap(s.settings.DeepSeekPeakPrices)
}

func (s *Store) DeepSeekPeakWindows() []costing.PeakWindow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.settings.DeepSeekPeakWindows) == 0 {
		return nil
	}
	out := make([]costing.PeakWindow, len(s.settings.DeepSeekPeakWindows))
	copy(out, s.settings.DeepSeekPeakWindows)
	return out
}

func (s *Store) DeepSeekPricesCheckedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return parseRFC3339(s.settings.DeepSeekPricesCheckedAt)
}

func (s *Store) SetDeepSeekPricing(peak map[string]costing.Prices, windows []costing.PeakWindow, checkedAt time.Time) error {
	s.mu.Lock()
	s.settings.DeepSeekPeakPrices = clonePriceMap(peak)
	if len(windows) > 0 {
		w := make([]costing.PeakWindow, len(windows))
		copy(w, windows)
		s.settings.DeepSeekPeakWindows = w
	}
	if !checkedAt.IsZero() {
		s.settings.DeepSeekPricesCheckedAt = checkedAt.UTC().Format(time.RFC3339)
	}
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) ZaiPrices() map[string]costing.Prices {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePriceMap(s.settings.ZaiPrices)
}

func (s *Store) ZaiPricesCheckedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return parseRFC3339(s.settings.ZaiPricesCheckedAt)
}

func (s *Store) SetZaiPricing(sheet map[string]costing.Prices, checkedAt time.Time) error {
	s.mu.Lock()
	s.settings.ZaiPrices = clonePriceMap(sheet)
	if !checkedAt.IsZero() {
		s.settings.ZaiPricesCheckedAt = checkedAt.UTC().Format(time.RFC3339)
	}
	s.mu.Unlock()
	return s.Save()
}

// DeepSeekModelsSeen returns the last catalog from GET /models (may be empty).
func (s *Store) DeepSeekModelsSeen() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.settings.DeepSeekModelsSeen))
	copy(out, s.settings.DeepSeekModelsSeen)
	return out
}

// SetDeepSeekModelsSeen stores the latest catalog snapshot.
func (s *Store) SetDeepSeekModelsSeen(models []string) error {
	s.mu.Lock()
	seen := make([]string, len(models))
	copy(seen, models)
	s.settings.DeepSeekModelsSeen = seen
	s.mu.Unlock()
	return s.Save()
}

// OpenRouterModel returns the saved OpenRouter model id.
func (s *Store) OpenRouterModel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings.OpenRouterModel
}

func (s *Store) OpenRouterPrices() map[string]costing.Prices {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePriceMap(s.settings.OpenRouterPrices)
}

func (s *Store) OpenRouterPricesCheckedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return parseRFC3339(s.settings.OpenRouterPricesCheckedAt)
}

func (s *Store) SetOpenRouterPricing(sheet map[string]costing.Prices, checkedAt time.Time) error {
	s.mu.Lock()
	s.settings.OpenRouterPrices = clonePriceMap(sheet)
	if !checkedAt.IsZero() {
		s.settings.OpenRouterPricesCheckedAt = checkedAt.UTC().Format(time.RFC3339)
	}
	s.mu.Unlock()
	return s.Save()
}

func clonePriceMap(in map[string]costing.Prices) map[string]costing.Prices {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]costing.Prices, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func parseRFC3339(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func (s *Store) SetTheme(theme string) error {
	switch theme {
	case "light", "dark":
	default:
		theme = "dark"
	}
	s.mu.Lock()
	s.settings.Theme = theme
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) Theme() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.Theme == "light" {
		return "light"
	}
	return "dark"
}

func normalizeUiFont(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "verdana", "tahoma", "arial", "system":
		return strings.ToLower(strings.TrimSpace(id))
	default:
		return "default"
	}
}

func normalizeMonoFont(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "lucida", "consolas", "courier", "cascadia":
		return strings.ToLower(strings.TrimSpace(id))
	default:
		return "default"
	}
}

func (s *Store) SetUiFont(id string) error {
	id = normalizeUiFont(id)
	s.mu.Lock()
	s.settings.UiFont = id
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) UiFont() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeUiFont(s.settings.UiFont)
}

func (s *Store) SetMonoFont(id string) error {
	id = normalizeMonoFont(id)
	s.mu.Lock()
	s.settings.MonoFont = id
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) MonoFont() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeMonoFont(s.settings.MonoFont)
}

// MaxAgentSteps returns the stored hard cap: 0 = default, negative = no cap
// (the run only warns after AgentWarnSteps).
func (s *Store) MaxAgentSteps() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.AgentMaxSteps == 0 {
		return DefaultAgentMaxSteps
	}
	return s.settings.AgentMaxSteps
}

// AgentWarnSteps returns the step count after which a long run reports a
// warning. 0 = default, negative = warnings off.
func (s *Store) AgentWarnSteps() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.AgentWarnSteps == 0 {
		return DefaultAgentWarnSteps
	}
	return s.settings.AgentWarnSteps
}

func (s *Store) SetAgentWarnSteps(steps int) error {
	if steps < 0 {
		steps = -1
	}
	if steps > 5000 {
		steps = 5000
	}
	s.mu.Lock()
	s.settings.AgentWarnSteps = steps
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetAgentMaxSteps(steps int) error {
	if steps == 0 {
		steps = DefaultAgentMaxSteps
	}
	if steps < 0 {
		// -1 = unlimited: keep the marker, the runner treats it as "no cap".
		steps = -1
	}
	if steps > 5000 {
		steps = 5000
	}
	s.mu.Lock()
	s.settings.AgentMaxSteps = steps
	s.mu.Unlock()
	return s.Save()
}

// AgentStallLimit возвращает лимит тишины на шаге: 0 = значение по умолчанию,
// отрицательное = watchdog выключен.
func (s *Store) AgentStallLimit() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	min := s.settings.AgentStallMin
	if min == 0 {
		min = DefaultAgentStallMin
	}
	if min < 0 {
		return 0
	}
	if min > 240 {
		min = 240
	}
	return time.Duration(min) * time.Minute
}

func (s *Store) SetAgentStallMin(min int) error {
	if min < 0 {
		min = -1 // -1 = выключено
	}
	if min > 240 {
		min = 240
	}
	s.mu.Lock()
	s.settings.AgentStallMin = min
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetWindowGeometry(width, height, x, y int, maximised bool) error {
	s.mu.Lock()
	// While maximised, keep the last normal size/position for restore after restart.
	if !maximised {
		if width > 0 {
			s.settings.WindowWidth = width
		}
		if height > 0 {
			s.settings.WindowHeight = height
		}
		s.settings.WindowX = x
		s.settings.WindowY = y
		s.settings.WindowPosSet = true
	}
	s.settings.WindowMaximised = maximised
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetLayoutSizes(projectsW, treeW, settingsW, terminalH, chatMaxW int) error {
	s.mu.Lock()
	if projectsW > 0 {
		s.settings.LayoutProjectsW = projectsW
	}
	if treeW > 0 {
		s.settings.LayoutTreeW = treeW
	}
	if settingsW > 0 {
		s.settings.LayoutSettingsW = settingsW
	}
	if terminalH > 0 {
		s.settings.LayoutTerminalH = terminalH
	}
	// 0 means “fill chat pane”; always persist the value from the UI snapshot.
	if chatMaxW >= 0 {
		s.settings.LayoutChatMaxW = chatMaxW
	}
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetComposerH(h int) error {
	if h < 110 {
		h = 110
	}
	if h > 480 {
		h = 480
	}
	s.mu.Lock()
	s.settings.LayoutComposerH = h
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) LastProject() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings.LastProject
}

// SetRunState records the last seen version and whether this run is ending
// gracefully, so the next launch can tell an update from a crash/kill.
func (s *Store) SetRunState(version string, clean bool) error {
	s.mu.Lock()
	s.settings.LastRunVersion = version
	s.settings.CleanExit = clean
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetLastProject(path string) error {
	if path == "" {
		return nil
	}
	s.mu.Lock()
	s.settings.LastProject = path
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) AddRecentProject(path string) error {
	s.mu.Lock()
	out := []string{path}
	for _, p := range s.settings.RecentProjects {
		if p != path {
			out = append(out, p)
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	s.settings.RecentProjects = out
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) RemoveRecentProject(path string) error {
	s.mu.Lock()
	out := make([]string, 0, len(s.settings.RecentProjects))
	for _, p := range s.settings.RecentProjects {
		if p != path {
			out = append(out, p)
		}
	}
	s.settings.RecentProjects = out
	s.mu.Unlock()
	return s.Save()
}

// AddUsage accumulates all-time spend for the given provider and the legacy combined total.
func (s *Store) AddUsage(provider string, costUSD float64, inputTokens, outputTokens, cacheHit, cacheMiss int) error {
	s.mu.Lock()
	s.settings.TotalCostUSD += costUSD
	s.settings.TotalInputTokens += inputTokens
	s.settings.TotalOutputTokens += outputTokens
	s.settings.TotalCacheHitTokens += cacheHit
	s.settings.TotalCacheMissTokens += cacheMiss
	switch {
	case normalizeProvider(provider) == ProviderZAI:
		s.settings.ZaiCostUSD += costUSD
		s.settings.ZaiInputTokens += inputTokens
		s.settings.ZaiOutputTokens += outputTokens
		s.settings.ZaiCacheHitTokens += cacheHit
		s.settings.ZaiCacheMissTokens += cacheMiss
	case normalizeProvider(provider) == ProviderOpenRouter:
		s.settings.OpenRouterCostUSD += costUSD
		s.settings.OpenRouterInputTokens += inputTokens
		s.settings.OpenRouterOutputTokens += outputTokens
		s.settings.OpenRouterCacheHitTokens += cacheHit
		s.settings.OpenRouterCacheMissTokens += cacheMiss
	case normalizeProvider(provider) == ProviderQwen:
		s.settings.QwenCostUSD += costUSD
		s.settings.QwenInputTokens += inputTokens
		s.settings.QwenOutputTokens += outputTokens
		s.settings.QwenCacheHitTokens += cacheHit
		s.settings.QwenCacheMissTokens += cacheMiss
	case IsLocalProvider(provider):
		s.settings.LocalCostUSD += costUSD
		s.settings.LocalInputTokens += inputTokens
		s.settings.LocalOutputTokens += outputTokens
		s.settings.LocalCacheHitTokens += cacheHit
		s.settings.LocalCacheMissTokens += cacheMiss
	default:
		s.settings.DeepSeekCostUSD += costUSD
		s.settings.DeepSeekInputTokens += inputTokens
		s.settings.DeepSeekOutputTokens += outputTokens
		s.settings.DeepSeekCacheHitTokens += cacheHit
		s.settings.DeepSeekCacheMissTokens += cacheMiss
	}
	s.mu.Unlock()
	return s.Save()
}

// ProviderUsage returns persisted totals for one provider.
func (s *Store) ProviderUsage(provider string) (cost float64, in, out, cacheHit, cacheMiss int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch {
	case normalizeProvider(provider) == ProviderZAI:
		return s.settings.ZaiCostUSD, s.settings.ZaiInputTokens, s.settings.ZaiOutputTokens,
			s.settings.ZaiCacheHitTokens, s.settings.ZaiCacheMissTokens
	case normalizeProvider(provider) == ProviderOpenRouter:
		return s.settings.OpenRouterCostUSD, s.settings.OpenRouterInputTokens, s.settings.OpenRouterOutputTokens,
			s.settings.OpenRouterCacheHitTokens, s.settings.OpenRouterCacheMissTokens
	case normalizeProvider(provider) == ProviderQwen:
		return s.settings.QwenCostUSD, s.settings.QwenInputTokens, s.settings.QwenOutputTokens,
			s.settings.QwenCacheHitTokens, s.settings.QwenCacheMissTokens
	case IsLocalProvider(provider):
		return s.settings.LocalCostUSD, s.settings.LocalInputTokens, s.settings.LocalOutputTokens,
			s.settings.LocalCacheHitTokens, s.settings.LocalCacheMissTokens
	default:
		return s.settings.DeepSeekCostUSD, s.settings.DeepSeekInputTokens, s.settings.DeepSeekOutputTokens,
			s.settings.DeepSeekCacheHitTokens, s.settings.DeepSeekCacheMissTokens
	}
}
