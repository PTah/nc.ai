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

func TestPickOpenRouterModel_DefaultFlash(t *testing.T) {
	d := PickOpenRouterModel(RouteInput{UserText: "поправь опечатку в README"})
	if d.Model != ModelORFlash {
		t.Fatalf("got %s want %s", d.Model, ModelORFlash)
	}
}

func TestPickOpenRouterModel_ComplexAndVision(t *testing.T) {
	d := PickOpenRouterModel(RouteInput{UserText: "сделай рефакторинг auth"})
	if d.Model != ModelORStrong {
		t.Fatalf("complex: got %s want %s", d.Model, ModelORStrong)
	}
	d = PickOpenRouterModel(RouteInput{UserText: "hi", Step: 8})
	if d.Model != ModelORStrong {
		t.Fatalf("long-run: got %s want %s", d.Model, ModelORStrong)
	}
	d = PickOpenRouterModel(RouteInput{UserText: "скрин", HasImages: true})
	if d.Model != ModelORVision {
		t.Fatalf("vision: got %s want %s", d.Model, ModelORVision)
	}
}

func TestPickQwenModel_DefaultPlus(t *testing.T) {
	d := PickQwenModel(RouteInput{UserText: "поправь опечатку в README"})
	if d.Model != ModelQwenPlus {
		t.Fatalf("got %s want %s", d.Model, ModelQwenPlus)
	}
}

func TestPickQwenModel_ComplexAndVision(t *testing.T) {
	d := PickQwenModel(RouteInput{UserText: "сделай рефакторинг auth"})
	if d.Model != ModelQwenStrong {
		t.Fatalf("complex: got %s want %s", d.Model, ModelQwenStrong)
	}
	d = PickQwenModel(RouteInput{UserText: "hi", Step: 8})
	if d.Model != ModelQwenStrong {
		t.Fatalf("long-run: got %s want %s", d.Model, ModelQwenStrong)
	}
	d = PickQwenModel(RouteInput{UserText: "скрин", HasImages: true})
	if d.Model != ModelQwenVision {
		t.Fatalf("vision: got %s want %s", d.Model, ModelQwenVision)
	}
}
