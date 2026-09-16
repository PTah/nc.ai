package agent

import "testing"

func TestParseLocalModelSize(t *testing.T) {
	if got := ParseLocalModelSize("qwen2.5-coder:7b"); got != 7 {
		t.Fatalf("7b: got %v", got)
	}
	if got := ParseLocalModelSize("deepseek-coder-v2:16b"); got != 16 {
		t.Fatalf("16b: got %v", got)
	}
	if got := ParseLocalModelSize("llama3"); got != 0 {
		t.Fatalf("unknown: got %v", got)
	}
}

func TestPickLocalModel_PrefersToolsAndSize(t *testing.T) {
	avail := []LocalModelInfo{
		NewLocalModelInfo("deepseek-coder-v2:16b", []string{"completion", "insert"}),
		NewLocalModelInfo("qwen2.5-coder:7b", []string{"completion", "tools", "insert"}),
		NewLocalModelInfo("qwen2.5-coder:14b", []string{"completion", "tools"}),
	}
	d := PickLocalModel(avail, RouteInput{UserText: "hi"}, "qwen2.5-coder:7b")
	if d.Model != "qwen2.5-coder:7b" {
		t.Fatalf("simple preferred: got %s (%s)", d.Model, d.Reason)
	}
	d = PickLocalModel(avail, RouteInput{UserText: "сделай рефакторинг auth"}, "")
	if d.Model != "qwen2.5-coder:14b" {
		t.Fatalf("complex: got %s want 14b (16b has no tools)", d.Model)
	}
	d = PickLocalModel(avail, RouteInput{UserText: "ok", Step: 8}, "")
	if d.Model != "qwen2.5-coder:14b" {
		t.Fatalf("long-run: got %s", d.Model)
	}
}

func TestPickLocalModel_FastWithoutPreferred(t *testing.T) {
	avail := []LocalModelInfo{
		NewLocalModelInfo("qwen2.5-coder:14b", []string{"tools"}),
		NewLocalModelInfo("qwen2.5-coder:7b", []string{"tools"}),
	}
	d := PickLocalModel(avail, RouteInput{UserText: "hi"}, "")
	if d.Model != "qwen2.5-coder:7b" {
		t.Fatalf("fast: got %s", d.Model)
	}
}

func TestIsLocalProviderPrefix(t *testing.T) {
	r := &Runner{ProviderID: "local:home"}
	if !r.isLocal() {
		t.Fatal("local:home should be local")
	}
}
