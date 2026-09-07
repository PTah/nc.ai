package appmeta

// App identity and release highlights (shown on first launch of a new version).
const (
	Name    = "NotCursor.ai"
	Version = "0.4.5"
)

// Highlights are the five most recent significant features for the Welcome splash.
// Keep newest first; trim to 5 when bumping a release.
var Highlights = []string{
	"Welcome splash on new builds + Model prices card (all providers)",
	"OpenRouter as third LLM provider (Qwen coder + Auto-models)",
	"Right-click Copy for chat text",
	"apply_patch tool — precise SEARCH/REPLACE edits (fewer output tokens)",
	"Z.ai Auto-models: free flash → glm-5.3 on complex tasks",
}
