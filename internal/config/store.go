package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const appDirName = "NotCursor"

// Provider ids persisted in Settings.ActiveProvider.
const (
	ProviderDeepSeek = "deepseek"
	ProviderZAI      = "zai"
)

type Settings struct {
	// ActiveProvider selects which LLM backend is used ("deepseek" | "zai").
	ActiveProvider string `json:"activeProvider,omitempty"`

	DeepSeekAPIKey string `json:"deepseekApiKey"`
	DeepSeekModel  string `json:"deepseekModel"`

	ZaiAPIKey string `json:"zaiApiKey,omitempty"`
	ZaiModel  string `json:"zaiModel,omitempty"`
	// ZaiEndpoint: "paas" (pay-as-you-go, default) or "coding" (GLM Coding Plan).
	ZaiEndpoint string `json:"zaiEndpoint,omitempty"`

	Shell          string   `json:"shell"`
	RecentProjects []string `json:"recentProjects"`
	GitUsername    string   `json:"gitUsername"`
	GitPassword    string   `json:"gitPassword"`
	SSHUser        string   `json:"sshUser"`
	ShowTerminal   bool     `json:"showTerminal"`
	// ShowFiles: nil = default true (visible). Explicit false remembers Hide files.
	ShowFiles *bool `json:"showFiles,omitempty"`
	// ShowSettings remembers the right settings pane visibility.
	ShowSettings bool `json:"showSettings"`

	// Theme: "dark" (default) or "light".
	Theme string `json:"theme,omitempty"`

	// AgentMaxSteps caps the tool-using agent loop. 0 means the default (40).
	AgentMaxSteps int `json:"agentMaxSteps,omitempty"`

	// AutoModels enables per-turn DeepSeek flash/pro/vision routing.
	// Ignored when ActiveProvider is not deepseek.
	AutoModels bool `json:"autoModels,omitempty"`

	// Main window geometry (logical pixels). Zero width/height → defaults.
	WindowWidth     int  `json:"windowWidth,omitempty"`
	WindowHeight    int  `json:"windowHeight,omitempty"`
	WindowX         int  `json:"windowX,omitempty"`
	WindowY         int  `json:"windowY,omitempty"`
	WindowPosSet    bool `json:"windowPosSet,omitempty"`
	WindowMaximised bool `json:"windowMaximised,omitempty"`

	// Inner layout sizes (px). Zero → CSS defaults.
	LayoutProjectsW  int `json:"layoutProjectsW,omitempty"`
	LayoutTreeW      int `json:"layoutTreeW,omitempty"`
	LayoutSettingsW  int `json:"layoutSettingsW,omitempty"`
	LayoutTerminalH  int `json:"layoutTerminalH,omitempty"`
	LayoutComposerH  int `json:"layoutComposerH,omitempty"`

	// All-time API usage counters backing the USD spend counter in the top bar.
	TotalCostUSD      float64 `json:"totalCostUsd"`
	TotalInputTokens  int     `json:"totalInputTokens"`
	TotalOutputTokens int     `json:"totalOutputTokens"`
}

type Store struct {
	mu       sync.RWMutex
	settings Settings
	path     string
}

func NewStore() *Store {
	return &Store{
		settings: Settings{
			ActiveProvider: ProviderDeepSeek,
			DeepSeekModel:  "deepseek-v4-flash",
			ZaiModel:       "glm-4.7-flash",
			ZaiEndpoint:    "paas",
			Shell:          "powershell",
			AgentMaxSteps:  40,
		},
	}
}

func (s *Store) AppDataDir() (string, error) {
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
	defer s.mu.Unlock()
	if err := json.Unmarshal(data, &s.settings); err != nil {
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
		s.settings.DeepSeekModel = "deepseek-v4-flash"
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
	return os.WriteFile(path, data, 0o600)
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) SetDeepSeekAPIKey(key string) error {
	s.mu.Lock()
	s.settings.DeepSeekAPIKey = key
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetDeepSeekModel(model string) error {
	s.mu.Lock()
	s.settings.DeepSeekModel = model
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetZaiAPIKey(key string) error {
	s.mu.Lock()
	s.settings.ZaiAPIKey = key
	s.mu.Unlock()
	return s.Save()
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
	s.settings.ActiveProvider = normalizeProvider(provider)
	s.mu.Unlock()
	return s.Save()
}

// ActiveAPIKey returns the API key for the active provider.
func (s *Store) ActiveAPIKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch normalizeProvider(s.settings.ActiveProvider) {
	case ProviderZAI:
		return s.settings.ZaiAPIKey
	default:
		return s.settings.DeepSeekAPIKey
	}
}

// ActiveModel returns the model id for the active provider.
func (s *Store) ActiveModel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch normalizeProvider(s.settings.ActiveProvider) {
	case ProviderZAI:
		return s.settings.ZaiModel
	default:
		return s.settings.DeepSeekModel
	}
}

// SetActiveModel persists the model for the currently active provider.
func (s *Store) SetActiveModel(model string) error {
	s.mu.Lock()
	switch normalizeProvider(s.settings.ActiveProvider) {
	case ProviderZAI:
		s.settings.ZaiModel = model
	default:
		s.settings.DeepSeekModel = model
	}
	s.mu.Unlock()
	return s.Save()
}

func normalizeProvider(p string) string {
	switch p {
	case ProviderZAI:
		return ProviderZAI
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

func (s *Store) SetShowSettings(show bool) error {
	s.mu.Lock()
	s.settings.ShowSettings = show
	s.mu.Unlock()
	return s.Save()
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

func (s *Store) MaxAgentSteps() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings.AgentMaxSteps <= 0 {
		return 40
	}
	return s.settings.AgentMaxSteps
}

func (s *Store) SetAgentMaxSteps(steps int) error {
	if steps <= 0 {
		steps = 40
	}
	if steps > 500 {
		steps = 500
	}
	s.mu.Lock()
	s.settings.AgentMaxSteps = steps
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

func (s *Store) SetLayoutSizes(projectsW, treeW, settingsW, terminalH int) error {
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

// AddUsage accumulates all-time API spend (USD) and token counters and
// persists them to settings.json so the counter survives app restarts.
func (s *Store) AddUsage(costUSD float64, inputTokens, outputTokens int) error {
	s.mu.Lock()
	s.settings.TotalCostUSD += costUSD
	s.settings.TotalInputTokens += inputTokens
	s.settings.TotalOutputTokens += outputTokens
	s.mu.Unlock()
	return s.Save()
}
