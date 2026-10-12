package mcp

// Preset is a ready-made MCP server config for Settings → MCP.
type Preset struct {
	ID          string       `json:"id"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
	Server      ServerConfig `json:"server"`
	// SuggestedTools — recommended allowlist after first successful tools/list.
	SuggestedTools []string `json:"suggestedTools,omitempty"`
	// Install — how NotCursor downloads the server when the preset is applied.
	Install InstallSpec `json:"install"`
	// UsageHint — short agent-facing order of work (also in docs/mcp.md).
	UsageHint string `json:"usageHint,omitempty"`
}

// InstallSpec describes how to fetch a preset server onto the machine.
type InstallSpec struct {
	// Kind: pip | npm | uvx | npx (npx needs no global install — package pulled on start).
	Kind string `json:"kind"`
	// Package is the pip/npm/uvx package name.
	Package string `json:"package"`
	// Binary is the CLI name after install (empty for npx/uvx launcher presets).
	Binary string `json:"binary,omitempty"`
	// ServeArgs appended when launching the binary (e.g. ["serve"]).
	ServeArgs []string `json:"serveArgs,omitempty"`
}

// Presets returns built-in coding-oriented MCP server recipes (stdio).
// Commands match upstream READMEs (checked 2026-10 for 0.8.5.1).
func Presets() []Preset {
	return []Preset{
		{
			ID:    "codebase-memory",
			Label: "Codebase Memory",
			Description: "Локальный граф кода (AST/tree-sitter). Быстрый поиск символов и связей. " +
				"Источник: npm codebase-memory-mcp / github.com/DeusData/codebase-memory-mcp",
			Server: ServerConfig{
				ID:         "codebase-memory",
				Label:      "Codebase Memory",
				Enabled:    true,
				Transport:  "stdio",
				Command:    "codebase-memory-mcp",
				Args:       nil,
				TimeoutSec: 120,
			},
			Install: InstallSpec{
				Kind:    "npm",
				Package: "codebase-memory-mcp",
				Binary:  "codebase-memory-mcp",
			},
			SuggestedTools: []string{
				"index_repository", "search_graph", "trace_call_path",
				"query_graph", "get_architecture", "get_code_snippet", "search_code",
			},
			UsageHint: "Сначала index_repository (если проект ещё не в графе), затем search_graph / trace_call_path; " +
				"потом точечный read_file / apply_patch. Не начинай с широкого grep.",
		},
		{
			ID:    "code-review-graph",
			Label: "Code Review Graph",
			Description: "Граф для ревью и impact-анализа. " +
				"Источник: PyPI code-review-graph / github.com/tirth8205/code-review-graph",
			Server: ServerConfig{
				ID:         "code-review-graph",
				Label:      "Code Review Graph",
				Enabled:    true,
				Transport:  "stdio",
				Command:    "code-review-graph",
				Args:       []string{"serve"},
				TimeoutSec: 120,
			},
			Install: InstallSpec{
				Kind:      "pip",
				Package:   "code-review-graph",
				Binary:    "code-review-graph",
				ServeArgs: []string{"serve"},
			},
			SuggestedTools: []string{
				"get_minimal_context_tool", "get_review_context_tool",
				"query_graph_tool", "get_impact_radius_tool",
				"build_or_update_graph_tool", "semantic_search_nodes_tool",
			},
			UsageHint: "Один раз build_or_update_graph_tool по корню проекта; дальше get_minimal_context_tool / " +
				"get_review_context_tool / query_graph_tool, затем read_file. Широкий grep — в конце.",
		},
		{
			ID:    "codegraph",
			Label: "CodeGraph MCP",
			Description: "Публичный CodeGraph MCP (@agntk/codegraph-mcp). Тянется через npx при старте. " +
				"Источник: npm @agntk/codegraph-mcp",
			Server: ServerConfig{
				ID:         "codegraph",
				Label:      "CodeGraph MCP",
				Enabled:    true,
				Transport:  "stdio",
				Command:    "npx",
				Args:       []string{"-y", "@agntk/codegraph-mcp"},
				TimeoutSec: 180,
			},
			Install: InstallSpec{
				Kind:    "npx",
				Package: "@agntk/codegraph-mcp",
			},
			UsageHint: "Сначала инструменты индексации/поиска графа CodeGraph, затем read_file. " +
				"Не дублируй тот же поиск через grep без нужды.",
		},
		{
			ID:    "cognee-memory",
			Label: "Cognee memory",
			Description: "Память remember/recall (граф знаний). " +
				"Источник: PyPI cognee-mcp / docs.cognee.ai. Нужен LLM API key в env сервера.",
			Server: ServerConfig{
				ID:         "cognee",
				Label:      "Cognee memory",
				Enabled:    true,
				Transport:  "stdio",
				Command:    "uvx",
				Args:       []string{"cognee-mcp@latest"},
				TimeoutSec: 180,
			},
			Install: InstallSpec{
				Kind:    "uvx",
				Package: "cognee-mcp",
			},
			SuggestedTools: []string{"cognify", "search", "list_data", "prune"},
			UsageHint: "Для фактов/решений между сессиями: cognify → search. Для навигации по коду " +
				"предпочитай Codebase Memory / Code Review Graph, не Cognee.",
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
