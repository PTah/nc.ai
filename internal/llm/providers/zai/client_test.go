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
