package agent

import "testing"

func TestResolveModel_StickyWithinRun(t *testing.T) {
	r := &Runner{
		AutoModels: true,
		ProviderID: "deepseek",
		UserText:   "hi",
	}
	emit := func(Event) {}
	m1 := r.resolveModel(0, emit)
	if m1 != ModelFlash {
		t.Fatalf("step0 got %s want flash", m1)
	}
	// Would escalate at step>=8 without sticky lock.
	m2 := r.resolveModel(8, emit)
	if m2 != ModelFlash {
		t.Fatalf("step8 sticky got %s want flash", m2)
	}
}

func TestResolveModel_SessionSticky(t *testing.T) {
	r := &Runner{
		AutoModels:  true,
		ProviderID:  "deepseek",
		StickyModel: ModelPro,
		UserText:    "hi",
	}
	emit := func(Event) {}
	if got := r.resolveModel(0, emit); got != ModelPro {
		t.Fatalf("got %s want sticky pro", got)
	}
}

func TestResolveModel_VisionUpgrade(t *testing.T) {
	r := &Runner{
		AutoModels: true,
		ProviderID: "deepseek",
		UserText:   "hi",
	}
	emit := func(Event) {}
	_ = r.resolveModel(0, emit)
	r.HasImages = true
	if got := r.resolveModel(1, emit); got != ModelVision {
		t.Fatalf("got %s want vision upgrade", got)
	}
}

func TestIsVisionModel(t *testing.T) {
	// DeepSeek V4.1 Flash is multimodal, so ModelVision == ModelFlash and neither
	// may be treated as a vision-only model (that would break sticky routing).
	if IsVisionModel(ModelVision) || IsVisionModel(ModelFlash) || IsVisionModel(ModelPro) {
		t.Fatal("deepseek models must not be treated as vision-only")
	}
	if !IsVisionModel(ModelZaiVision) || !IsVisionModel(ModelORVision) {
		t.Fatal("dedicated vision models must still be detected")
	}
}
