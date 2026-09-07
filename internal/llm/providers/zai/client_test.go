package zai

import (
	"encoding/json"
	"testing"
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
