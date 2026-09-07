package zai

import (
	"encoding/json"
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestDecodeModelsList(t *testing.T) {
	const raw = `{
	  "object": "list",
	  "data": [
	    {"id": "glm-5.3", "object": "model", "created": 1718223400, "owned_by": "z-ai"},
	    {"id": "glm-5.3-flash", "object": "model", "created": 1718223500, "owned_by": "z-ai"},
	    {"id": "glm-4.7-flash", "object": "model", "created": 1713223100, "owned_by": "z-ai"}
	  ]
	}`
	var out modelsListResponse
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 3 {
		t.Fatalf("len=%d", len(out.Data))
	}
	if out.Data[0].ID != "glm-5.3" {
		t.Fatalf("id=%q", out.Data[0].ID)
	}
}

func TestPreferAndOrderModels(t *testing.T) {
	avail := []string{"glm-5.3", "glm-4.5-flash", "glm-4.7-flash"}
	ordered := OrderModels(avail)
	if len(ordered) < 3 || ordered[0] != "glm-4.7-flash" || ordered[1] != "glm-4.5-flash" {
		t.Fatalf("OrderModels=%v", ordered)
	}
	if got := PreferModel(avail, "glm-5.3"); got != "glm-5.3" {
		t.Fatalf("keep current: %q", got)
	}
	if got := PreferModel(avail, "glm-missing"); got != "glm-4.7-flash" {
		t.Fatalf("prefer free: %q", got)
	}
	if got := PreferModel([]string{"glm-5.3"}, ""); got != "glm-5.3" {
		t.Fatalf("first avail: %q", got)
	}
	merged := MergeFreeModels([]string{"glm-5.3", "glm-5.3-flash"})
	if !IsFreeModel("glm-4.7-flash") || len(OrderModels(merged)) < 3 {
		t.Fatalf("MergeFreeModels=%v", merged)
	}
	if got := PreferFreeModel([]string{"glm-5.3-flash", "glm-4.7-flash"}, "glm-5.3-flash"); got != "glm-4.7-flash" {
		t.Fatalf("PreferFreeModel should pick free over paid current: %q", got)
	}
}

func TestThinkingDisabled(t *testing.T) {
	if thinkingDisabled(nil) {
		t.Fatal("nil should not be disabled")
	}
	if thinkingDisabled(map[string]any{"type": "enabled"}) {
		t.Fatal("enabled should not be disabled")
	}
	if !thinkingDisabled(map[string]any{"type": "disabled"}) {
		t.Fatal("disabled should be detected")
	}
}

func TestMapAPIError1305(t *testing.T) {
	body := []byte(`{"error":{"code":"1305","message":"The service may be temporarily overloaded, please try again later"}}`)
	err := mapAPIError(429, body)
	if err == nil || err.Error() != "Модель перегружена, попробуйте позднее…" {
		t.Fatalf("got %v", err)
	}
}

func TestMapAPIError1302(t *testing.T) {
	body := []byte(`{"error":{"code":"1302","message":"Rate limit reached for requests"}}`)
	err := mapAPIError(429, body)
	if err == nil || err.Error() != "Превышен лимит запросов, попробуйте позднее…" {
		t.Fatalf("got %v", err)
	}
}

func TestPrepareMessagesStripsImagesForTextModel(t *testing.T) {
	in := []llm.Message{{
		Role: "user",
		Parts: []llm.ContentPart{
			{Type: "text", Text: "привет"},
			{Type: "image_url", ImageURL: &llm.ImageURL{URL: "data:image/png;base64,xx"}},
		},
	}}
	out := prepareMessages("glm-4.7-flash", in)
	if len(out) != 1 || out[0].Parts != nil {
		t.Fatalf("parts should be cleared: %+v", out[0])
	}
	if !strings.Contains(out[0].Content, "привет") {
		t.Fatalf("content=%q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "изображение опущено") {
		t.Fatalf("expected image placeholder, got %q", out[0].Content)
	}
}
