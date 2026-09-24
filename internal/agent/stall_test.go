package agent

import (
	"strings"
	"testing"
	"time"
)

func TestStallWarningAtHalfLimit(t *testing.T) {
	w := newStallWatch(10 * time.Minute)
	start := time.Now()

	// До половины лимита предупреждения нет.
	if text, ok := w.warning(start.Add(4 * time.Minute)); ok {
		t.Fatalf("слишком раннее предупреждение: %q", text)
	}
	// На половине — одно предупреждение с фазой и остатком времени.
	text, ok := w.warning(start.Add(5 * time.Minute))
	if !ok {
		t.Fatal("ожидалось предупреждение на половине лимита")
	}
	for _, want := range []string{"Шаг 1", "жду ответ модели", "5 мин"} {
		if !strings.Contains(text, want) {
			t.Errorf("в предупреждении нет %q: %q", want, text)
		}
	}
	// Повторно не повторяем.
	if _, again := w.warning(start.Add(6 * time.Minute)); again {
		t.Fatal("предупреждение должно отправляться один раз")
	}
	// После срабатывания verdict предупреждений быть не должно.
	if _, bad := w.verdict(start.Add(11 * time.Minute)); !bad {
		t.Fatal("ожидался обрыв прогона после лимита")
	}
	if _, firedWarn := w.warning(start.Add(12 * time.Minute)); firedWarn {
		t.Fatal("после обрыва предупреждения не отправляются")
	}
}

func TestStallWarningPhaseHuman(t *testing.T) {
	w := newStallWatch(time.Minute)
	w.onPhase("tool", "Запускаю: go test ./…")
	text, ok := w.warning(time.Now().Add(40 * time.Second))
	if !ok {
		t.Fatal("ожидалось предупреждение в фазе инструмента")
	}
	if !strings.Contains(text, "выполняю инструмент") {
		t.Errorf("фаза не переведена: %q", text)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45 с"},
		{9*time.Minute + 12*time.Second, "9 мин 12 с"},
		{time.Hour + 4*time.Minute, "1 ч 04 мин"},
	}
	for _, c := range cases {
		if got := humanDuration(c.in); got != c.want {
			t.Errorf("humanDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStallVerdictLifecycle(t *testing.T) {
	w := newStallWatch(10 * time.Minute)
	w.onStep(22, "qwen-plus")
	base := time.Now()

	if _, bad := w.verdict(base.Add(9 * time.Minute)); bad {
		t.Fatal("до лимита затыка быть не должно")
	}
	w.touch("модель присылает ответ")
	if _, bad := w.verdict(base.Add(5 * time.Minute)); bad {
		t.Fatal("признак жизни должен сбрасывать счётчик тишины")
	}

	text, bad := w.verdict(base.Add(12 * time.Minute))
	if !bad {
		t.Fatal("ожидали разбор затыка после лимита тишины")
	}
	for _, want := range []string{"Шаг 22", "qwen-plus", "Что делать"} {
		if !strings.Contains(text, want) {
			t.Fatalf("в разборе нет %q:\n%s", want, text)
		}
	}
	if w.stalledText() != text {
		t.Fatal("stalledText должен вернуть тот же разбор")
	}
	if _, again := w.verdict(base.Add(time.Hour)); again {
		t.Fatal("повторно срабатывать не должен")
	}
}

func TestStallVerdictToolPhase(t *testing.T) {
	w := newStallWatch(5 * time.Minute)
	w.onStep(7, "deepseek-flash")
	w.onPhase("tool", "Запускаю: go test ./...")
	w.onToolDone()

	text, bad := w.verdict(time.Now().Add(6 * time.Minute))
	if !bad {
		t.Fatal("ожидали затык в фазе инструмента")
	}
	for _, want := range []string{"Запускаю: go test ./...", "инструмент", "Инструментов выполнено за прогон: 1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("в разборе нет %q:\n%s", want, text)
		}
	}
}

func TestStallDisabled(t *testing.T) {
	w := newStallWatch(0)
	w.onStep(3, "qwen-plus")
	if _, bad := w.verdict(time.Now().Add(24 * time.Hour)); bad {
		t.Fatal("выключенный watchdog срабатывать не должен")
	}
}
