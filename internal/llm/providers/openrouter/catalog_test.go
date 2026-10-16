// Created by nc.ai 0.8.6.4 on Papasha 2026-10-16 15:47:42
package openrouter

import "testing"

func mkModel(id, modality string, params ...string) ModelInfo {
	m := ModelInfo{ID: id, SupportedParams: params}
	if modality != "" {
		m.Architecture = &struct {
			Modality string `json:"modality,omitempty"`
		}{Modality: modality}
	}
	return m
}

func TestCategoryOfUsesModalityOutput(t *testing.T) {
	cases := []struct {
		name string
		m    ModelInfo
		want string
	}{
		{"image generation", mkModel("black-forest-labs/flux-1.1-pro", "text->image"), CategoryImage},
		{"audio out", mkModel("some/tts-1", "text->audio"), CategoryAudio},
		{"video out", mkModel("some/veo", "text->video"), CategoryVideo},
		// Vision reads images and answers with text: it is a text model, not a
		// picture generator, so it must not be filed under "Картинки".
		{"vision stays text", mkModel("qwen/qwen3-vl-8b-instruct", "text+image->text"), CategoryGeneral},
		{"coder id", mkModel("qwen/qwen3-coder-plus", "text->text"), CategoryCoding},
		{"reasoning", mkModel("qwen/qwq-32b", "text->text"), CategoryReasoning},
		{"plain chat", mkModel("openai/gpt-4o", "text->text"), CategoryGeneral},
	}
	for _, tc := range cases {
		if got := CategoryOf(tc.m); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestSupportsTools(t *testing.T) {
	if !SupportsTools(mkModel("a/b", "text->text", "tools", "temperature")) {
		t.Fatal("tools param must be detected")
	}
	if SupportsTools(mkModel("a/b", "text->text", "temperature")) {
		t.Fatal("model without tools must report false")
	}
}

func TestCatalogKeepsCuratedFirstAndCapsMedia(t *testing.T) {
	items := []ModelInfo{
		mkModel("vendor/plain-chat", "text->text"),
		mkModel("vendor/img-1", "text->image"),
		mkModel("vendor/img-2", "text->image"),
		mkModel("qwen/qwen3-coder", "text->text", "tools"),
	}
	cat := Catalog(items, 1)

	// Only curated ids present in the live catalog survive, so the first row is
	// the curated model that the live answer actually has.
	if len(cat) == 0 || cat[0].ID != "qwen/qwen3-coder" {
		t.Fatalf("live curated model must come first, got %+v", cat)
	}
	if !cat[0].Tools {
		t.Fatal("curated coding models are tool-capable")
	}
	var images []string
	for _, it := range cat {
		if it.Category == CategoryImage {
			images = append(images, it.ID)
		}
	}
	if len(images) != 1 {
		t.Fatalf("media buckets must be capped at 1, got %v", images)
	}
	for _, it := range cat {
		if it.ID == "qwen/qwen3-coder-flash" {
			t.Fatal("curated id missing from the live catalog must be dropped")
		}
	}
}

func TestCatalogDoesNotPromoteToolLessCurated(t *testing.T) {
	// The live entry exists but advertises no tools: it must not be presented as
	// a curated agent model. It may still be listed under its own category (the
	// UI warns about exactly such models), but never promoted to Tools=true.
	items := []ModelInfo{
		mkModel("qwen/qwen3-coder-next", "text->text"),
		mkModel("qwen/qwen3-coder-flash", "text->text", "tools"),
	}
	cat := Catalog(items, 5)
	if len(cat) == 0 || cat[0].ID != "qwen/qwen3-coder-flash" || !cat[0].Tools {
		t.Fatalf("tool-capable curated model must lead the list, got %+v", cat)
	}
	for _, it := range cat {
		if it.ID == "qwen/qwen3-coder-next" && it.Tools {
			t.Fatal("tool-less curated model must not be marked tool-capable")
		}
	}
}

func TestCatalogFallbackKeepsCurated(t *testing.T) {
	// No live data (no key / request failed): nothing can be verified, so the
	// curated list is shown as-is.
	cat := Catalog(nil, 0)
	if len(cat) != len(CuratedModels) {
		t.Fatalf("fallback must keep every curated id, got %d of %d", len(cat), len(CuratedModels))
	}
	for i, it := range cat {
		if it.ID != CuratedModels[i] {
			t.Fatalf("fallback order changed at %d: got %q want %q", i, it.ID, CuratedModels[i])
		}
		if !it.Tools {
			t.Fatalf("fallback curated entries are assumed tool-capable: %q", it.ID)
		}
	}
}

func TestGroupIDsOrder(t *testing.T) {
	cat := []CatalogItem{
		{ID: "vendor/img-1", Category: CategoryImage},
		{ID: "qwen/qwen3-coder", Category: CategoryCoding, Tools: true},
	}
	groups := GroupIDs(cat)
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(groups))
	}
	if groups[0].Category != CategoryCoding {
		t.Fatalf("coding must be the first group, got %q", groups[0].Category)
	}
}
