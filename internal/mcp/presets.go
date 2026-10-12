package mcp

// Preset is a ready-made MCP server config for Settings → MCP.
type Preset struct {
	ID          string       `json:"id"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
	Server      ServerConfig `json:"server"`
	// SuggestedTools — recommended allowlist after first successful tools/list.
	SuggestedTools []string `json:"suggestedTools,omitempty"`
}

// Presets returns built-in coding-oriented MCP server recipes (stdio).
// These are examples — the user still enables tools explicitly after Probe.
func Presets() []Preset {
	return []Preset{
		{
			ID:    "code-review-graph",
			Label: "Code Review Graph",
			Description: "Граф связей кода для навигации при ревью. " +
				"Сначала спроси граф, потом точечный read_file — меньше слепого grep.",
			Server: ServerConfig{
				ID:        "code-review-graph",
				Label:     "Code Review Graph",
				Enabled:   false,
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"-y", "code-review-graph-mcp"},
				TimeoutSec: 90,
			},
		},
		{
			ID:    "codegraph-context",
			Label: "CodeGraph Context",
			Description: "Структурный контекст репозитория (символы, зависимости). " +
				"Для обычной разработки, не security-скан.",
			Server: ServerConfig{
				ID:        "codegraph",
				Label:     "CodeGraph Context",
				Enabled:   false,
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"-y", "@codegraph/mcp-server"},
				TimeoutSec: 90,
			},
		},
		{
			ID:    "codebase-memory",
			Label: "Codebase Memory",
			Description: "Память по кодовой базе между сессиями (решения, карта модулей). " +
				"Не замена grep — дополнение.",
			Server: ServerConfig{
				ID:        "codebase-memory",
				Label:     "Codebase Memory",
				Enabled:   false,
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"-y", "codebase-memory-mcp"},
				TimeoutSec: 90,
			},
		},
		{
			ID:    "cognee-memory",
			Label: "Cognee memory",
			Description: "Session-память remember/recall/forget. Нужен Python/uv или Docker; " +
				"HTTP — этап 2 клиента, в MVP — stdio если доступен.",
			Server: ServerConfig{
				ID:        "cognee",
				Label:     "Cognee memory",
				Enabled:   false,
				Transport: "stdio",
				Command:   "uv",
				Args:      []string{"run", "cognee-mcp"},
				TimeoutSec: 120,
			},
			SuggestedTools: []string{"remember", "recall", "forget"},
		},
	}
}

// PresetByID returns a preset or false.
func PresetByID(id string) (Preset, bool) {
	id = sanitizeID(id)
	for _, p := range Presets() {
		if sanitizeID(p.ID) == id || sanitizeID(p.Server.ID) == id {
			return p, true
		}
	}
	return Preset{}, false
}
