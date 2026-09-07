package agent

import "testing"

func TestPickModel_VisionWins(t *testing.T) {
	d := PickModel(RouteInput{UserText: "refactor everything", HasImages: true, Step: 20})
	if d.Model != ModelVision {
		t.Fatalf("got %s want vision", d.Model)
	}
}

func TestPickModel_ComplexAndLong(t *testing.T) {
	d := PickModel(RouteInput{UserText: "сделай рефакторинг auth", HintPathCount: 1})
	if d.Model != ModelPro {
		t.Fatalf("complex: got %s want pro", d.Model)
	}
	d = PickModel(RouteInput{UserText: "ok", HintPathCount: 5})
	if d.Model != ModelPro {
		t.Fatalf("many paths: got %s want pro", d.Model)
	}
	d = PickModel(RouteInput{UserText: "hi", Step: 8})
	if d.Model != ModelPro {
		t.Fatalf("long-run: got %s want pro", d.Model)
	}
}

func TestPickModel_DefaultFlash(t *testing.T) {
	d := PickModel(RouteInput{UserText: "поправь опечатку в README"})
	if d.Model != ModelFlash {
		t.Fatalf("got %s want flash", d.Model)
	}
}

func TestPickZaiModel_DefaultFree(t *testing.T) {
	d := PickZaiModel(RouteInput{UserText: "поправь опечатку в README"})
	if d.Model != ModelZaiFree {
		t.Fatalf("got %s want free flash", d.Model)
	}
}

func TestPickZaiModel_ComplexTo53(t *testing.T) {
	d := PickZaiModel(RouteInput{UserText: "сделай рефакторинг auth"})
	if d.Model != ModelZaiStrong {
		t.Fatalf("complex: got %s want %s", d.Model, ModelZaiStrong)
	}
	d = PickZaiModel(RouteInput{UserText: "hi", Step: 8})
	if d.Model != ModelZaiStrong {
		t.Fatalf("long-run: got %s want %s", d.Model, ModelZaiStrong)
	}
}

func TestPickZaiModel_VisionFlash(t *testing.T) {
	d := PickZaiModel(RouteInput{UserText: "что на скрине", HasImages: true})
	if d.Model != ModelZaiVision {
		t.Fatalf("got %s want %s", d.Model, ModelZaiVision)
	}
}
