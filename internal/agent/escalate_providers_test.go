// Created by nc.ai 0.9.4 on new-admin-pc 2026-10-30 08:52:34
package agent

import "testing"

// TestEscalationGatePerProvider фиксирует E3: mid-run EscalationGate включён для
// каждого провайдера со «сильной» моделью, маппинг strong совпадает с autoroute,
// а подъём одноразовый (второй bump ничего не делает).
func TestEscalationGatePerProvider(t *testing.T) {
	cases := []struct {
		provider string
		strong   string
		weak     string
	}{
		{"deepseek", ModelPro, ModelFlash},
		{"openrouter", ModelORStrong, "qwen/qwen3-coder-flash"},
		{"zai", ModelZaiStrong, ModelZaiFree},
		{"qwen", ModelQwenStrong, ModelQwenFast},
		{"kimi", ModelKimiStrong, ModelKimiFast},
		{"yandex", ModelYandexStrong, ModelYandexDefault},
	}
	for _, c := range cases {
		r := &Runner{AutoModels: true, ProviderID: c.provider, runModel: c.weak}
		if !r.escalateEnabled() {
			t.Fatalf("%s: gate must be enabled with AutoModels", c.provider)
		}
		if got := r.strongModel(); got != c.strong {
			t.Fatalf("%s: strongModel=%q want %q", c.provider, got, c.strong)
		}
		if !r.isWeakAutoModel(c.weak) {
			t.Fatalf("%s: %q must count as weak", c.provider, c.weak)
		}
		if r.isWeakAutoModel(c.strong) {
			t.Fatalf("%s: strong %q must not count as weak", c.provider, c.strong)
		}
		if r.providerLabel() == "" {
			t.Fatalf("%s: empty provider label", c.provider)
		}
		emit := func(Event) {}
		if !r.bumpToStrong(emit, "escalate-dup-tool", "") {
			t.Fatalf("%s: expected mid-run bump from weak model", c.provider)
		}
		if r.runModel != c.strong {
			t.Fatalf("%s: runModel=%q want %q", c.provider, r.runModel, c.strong)
		}
		if r.bumpToStrong(emit, "escalate-empty", "") {
			t.Fatalf("%s: escalate must be one-way", c.provider)
		}
	}
}

func TestEscalationGateOff(t *testing.T) {
	for _, p := range []string{"deepseek", "openrouter", "zai", "qwen", "kimi", "yandex"} {
		r := &Runner{AutoModels: false, ProviderID: p}
		if r.escalateEnabled() {
			t.Fatalf("%s: gate must be off when AutoModels=false", p)
		}
	}
	for _, p := range []string{"local", "anthropic"} {
		r := &Runner{AutoModels: true, ProviderID: p}
		if r.escalateEnabled() {
			t.Fatalf("%s: must not escalate", p)
		}
	}
}
