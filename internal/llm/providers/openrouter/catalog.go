// Created by nc.ai 0.8.6.4 on Papasha 2026-10-16 15:42:24
package openrouter

import (
	"strings"
)

// Category buckets mirror how the OpenRouter site groups its catalogue, so the
// Settings list can be read and filtered the same way.
const (
	CategoryCoding    = "coding"
	CategoryReasoning = "reasoning"
	CategoryGeneral   = "general"
	CategoryImage     = "image"
	CategoryAudio     = "audio"
	CategoryVideo     = "video"
	CategoryOther     = "other"
)

// CategoryOrder is the display order: what this app actually uses comes first.
var CategoryOrder = []string{
	CategoryCoding,
	CategoryReasoning,
	CategoryGeneral,
	CategoryImage,
	CategoryAudio,
	CategoryVideo,
	CategoryOther,
}

// CategoryLabelRU is the human name shown in Settings.
func CategoryLabelRU(cat string) string {
	switch cat {
	case CategoryCoding:
		return "Кодинг"
	case CategoryReasoning:
		return "Рассуждения"
	case CategoryGeneral:
		return "Общие чаты"
	case CategoryImage:
		return "Картинки"
	case CategoryAudio:
		return "Аудио"
	case CategoryVideo:
		return "Видео"
	default:
		return "Прочее"
	}
}

// modalityOut returns the output part of "text+image->text" style modality.
func modalityOut(m ModelInfo) string {
	if m.Architecture == nil {
		return ""
	}
	mod := strings.ToLower(strings.TrimSpace(m.Architecture.Modality))
	if i := strings.Index(mod, "->"); i >= 0 {
		return strings.TrimSpace(mod[i+len("->"):])
	}
	return mod
}

// isCodingID is the same heuristic FilterCodingModels used: agent/coding models.
func isCodingID(id string) bool {
	low := strings.ToLower(id)
	return strings.Contains(low, "coder") ||
		strings.Contains(low, "codestral") ||
		strings.Contains(low, "devstral") ||
		strings.HasPrefix(low, "deepseek/") ||
		strings.Contains(low, "codex")
}

// SupportsTools reports whether the model advertises tool/function calling.
// The agent loop is useless without it, so the UI must be able to say so.
func SupportsTools(m ModelInfo) bool {
	for _, p := range m.SupportedParams {
		if strings.EqualFold(strings.TrimSpace(p), "tools") {
			return true
		}
	}
	return false
}

// CategoryOf classifies one /models entry. OpenRouter does not publish its site
// grouping through the API, so modality decides media types and the id decides
// coding vs general text. Vision models (text+image->text) stay text: they read
// images, they do not generate them.
func CategoryOf(m ModelInfo) string {
	out := modalityOut(m)
	switch {
	case strings.Contains(out, "image") && !strings.Contains(out, "text"):
		return CategoryImage
	case strings.Contains(out, "audio") && !strings.Contains(out, "text"):
		return CategoryAudio
	case strings.Contains(out, "video"):
		return CategoryVideo
	}
	id := strings.ToLower(strings.TrimSpace(m.ID))
	hay := id + " " + strings.ToLower(strings.TrimSpace(m.Name))
	// Reasoning wins over the coding heuristics: deepseek-r1 is an agent model by
	// prefix, but its bucket is "Рассуждения", not "Кодинг".
	if strings.Contains(hay, "embed") {
		return CategoryOther
	}
	if strings.Contains(hay, "reason") || strings.Contains(hay, "thinking") ||
		strings.Contains(hay, "qwq") || strings.Contains(hay, "-r1") ||
		strings.Contains(hay, "/o1") || strings.Contains(hay, "/o3") ||
		strings.Contains(hay, "/o4") || strings.Contains(hay, "magistral") {
		return CategoryReasoning
	}
	if isCodingID(id) {
		return CategoryCoding
	}
	return CategoryGeneral
}

// CatalogItem is one selectable model with the metadata Settings needs.
type CatalogItem struct {
	ID       string
	Category string
	Tools    bool
}

// Catalog builds the Settings list: the curated agent models first (they are
// known-good for tool calling), then the live catalogue grouped by category.
//
// Curated ids are validated against the live answer — OpenRouter retires models
// silently (qwen/qwen3-coder-plus is gone), and a curated entry that no longer
// exists must not stay selectable. Suffixed variants (:floor) never appear in
// /models, so they are checked through their base id. Without live data (no key /
// request failed) nothing can be verified, so the whole curated list is shown.
//
// maxPerCategory caps each non-coding bucket so the list stays usable; coding
// models are always kept in full.
func Catalog(items []ModelInfo, maxPerCategory int) []CatalogItem {
	if maxPerCategory <= 0 {
		maxPerCategory = 50
	}
	byID := make(map[string]ModelInfo, len(items))
	for _, m := range items {
		if id := strings.TrimSpace(m.ID); id != "" {
			byID[id] = m
		}
	}
	hasLive := len(items) > 0

	out := make([]CatalogItem, 0, len(items))
	seen := map[string]bool{}
	for _, id := range CuratedModels {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		base := id
		if i := strings.LastIndex(id, ":"); i > 0 {
			base = id[:i]
		}
		if hasLive {
			m, ok := byID[base]
			if !ok {
				continue // модель снята с каталога
			}
			if !SupportsTools(m) {
				continue // без tools агент на ней не поедет
			}
			seen[id] = true
			out = append(out, CatalogItem{ID: id, Category: CategoryOf(m), Tools: true})
			continue
		}
		seen[id] = true
		out = append(out, CatalogItem{ID: id, Category: CategoryCoding, Tools: true})
	}

	perCategory := map[string]int{}
	for _, m := range items {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		cat := CategoryOf(m)
		if cat != CategoryCoding && perCategory[cat] >= maxPerCategory {
			continue
		}
		seen[id] = true
		perCategory[cat]++
		out = append(out, CatalogItem{ID: id, Category: cat, Tools: SupportsTools(m)})
	}
	return out
}

// GroupIDs splits a catalog into ordered category buckets (empty ones dropped).
func GroupIDs(catalog []CatalogItem) []ModelGroup {
	byCat := map[string][]string{}
	for _, it := range catalog {
		byCat[it.Category] = append(byCat[it.Category], it.ID)
	}
	groups := make([]ModelGroup, 0, len(CategoryOrder))
	for _, cat := range CategoryOrder {
		if ids := byCat[cat]; len(ids) > 0 {
			groups = append(groups, ModelGroup{Category: cat, Models: ids})
		}
	}
	return groups
}

// ModelGroup is one category with its model ids, ready for the UI.
type ModelGroup struct {
	Category string   `json:"category"`
	Models   []string `json:"models"`
}
