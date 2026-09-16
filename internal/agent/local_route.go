package agent

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// LocalModelInfo describes a model on a local OpenAI-compatible / Ollama server.
type LocalModelInfo struct {
	ID           string
	Tools        bool    // true if Ollama reports "tools" capability (or unknown → true)
	SizeB        float64 // parameter size in billions, 0 if unknown
	CoderLike    bool
	Capabilities []string
}

var sizeTokenRe = regexp.MustCompile(`(?i)(?:^|[:\-/_.])(\d+(?:\.\d+)?)\s*b(?:$|[:\-/_.])`)

// ParseLocalModelSize extracts e.g. 7 / 16 from "qwen2.5-coder:7b" or "deepseek-coder-v2:16b".
func ParseLocalModelSize(name string) float64 {
	m := sizeTokenRe.FindStringSubmatch(strings.TrimSpace(name))
	if len(m) < 2 {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

func isCoderLikeName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "coder") || strings.Contains(lower, "code")
}

// NewLocalModelInfo builds info from an id and optional capability list.
// If capabilities is empty (OpenAI /models only), Tools defaults to true so we
// do not exclude models when the server does not report caps.
func NewLocalModelInfo(id string, capabilities []string) LocalModelInfo {
	id = strings.TrimSpace(id)
	info := LocalModelInfo{
		ID:           id,
		SizeB:        ParseLocalModelSize(id),
		CoderLike:    isCoderLikeName(id),
		Capabilities: append([]string{}, capabilities...),
		Tools:        true,
	}
	if len(capabilities) > 0 {
		info.Tools = false
		for _, c := range capabilities {
			if strings.EqualFold(strings.TrimSpace(c), "tools") {
				info.Tools = true
				break
			}
		}
	}
	return info
}

// PickLocalModel chooses a weak (fast) vs strong local model from the catalog.
//
// Priority:
//  1. Prefer tool-capable models when any report tools
//  2. Prefer coder-like names when available in the filtered set
//  3. Complex / long-run / many paths → largest; otherwise → smallest (fast)
//  4. Fall back to preferred, then first available
func PickLocalModel(available []LocalModelInfo, in RouteInput, preferred string) RouteDecision {
	preferred = strings.TrimSpace(preferred)
	if len(available) == 0 {
		if preferred != "" {
			return RouteDecision{Model: preferred, Reason: "local-preferred"}
		}
		return RouteDecision{}
	}

	pool := available
	anyTools := false
	for _, m := range available {
		if m.Tools {
			anyTools = true
			break
		}
	}
	if anyTools {
		filtered := make([]LocalModelInfo, 0, len(available))
		for _, m := range available {
			if m.Tools {
				filtered = append(filtered, m)
			}
		}
		pool = filtered
	}

	coderPool := make([]LocalModelInfo, 0, len(pool))
	for _, m := range pool {
		if m.CoderLike {
			coderPool = append(coderPool, m)
		}
	}
	if len(coderPool) > 0 {
		pool = coderPool
	}

	wantStrong := in.Step >= 8 || in.HintPathCount >= 4 || isComplexTask(in.UserText)
	sorted := append([]LocalModelInfo{}, pool...)
	sort.SliceStable(sorted, func(i, j int) bool {
		si, sj := sorted[i].SizeB, sorted[j].SizeB
		if si == 0 {
			si = 1e9 // unknown → treat as large when sorting ascending for weak pick
		}
		if sj == 0 {
			sj = 1e9
		}
		if wantStrong {
			if si == sj {
				return sorted[i].ID < sorted[j].ID
			}
			return si > sj
		}
		if si == sj {
			return sorted[i].ID < sorted[j].ID
		}
		return si < sj
	})

	pick := sorted[0]
	reason := "local-fast"
	if wantStrong {
		reason = "local-strong"
		if in.Step >= 8 {
			reason = "local-long-run"
		} else if in.HintPathCount >= 4 || isComplexTask(in.UserText) {
			reason = "local-complex"
		}
	}

	// If preferred is in the same capability class and Auto wants fast, keep preferred
	// when it's among the smaller half — otherwise stick to pick.
	if preferred != "" {
		for _, m := range pool {
			if m.ID == preferred {
				if !wantStrong {
					return RouteDecision{Model: preferred, Reason: "local-preferred"}
				}
				break
			}
		}
	}
	return RouteDecision{Model: pick.ID, Reason: reason}
}
