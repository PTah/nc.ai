package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Шаг агента может «зависнуть»: провайдер не отвечает, инструмент не
// завершается, сеть отвалилась. Снаружи это выглядит как «шаг 22» без движения
// десятки минут — пользователь не понимает, ждать или что-то сломалось.
//
// Watchdog следит за признаками жизни (ответ модели, дельты стрима, начало и
// конец шага/инструмента). Если тишина превысила лимит — прогон принудительно
// обрывается, а в чат уходит разбор: на каком шаге, в какой фазе, сколько ждали
// и что с этим делать.

// DefaultStallLimit — сколько тишины терпим по умолчанию (0 = не следить).
const DefaultStallLimit = 10 * time.Minute

// stallTick — как часто watchdog проверяет тишину.
const stallTick = 15 * time.Second

// ErrStalled — прогон прерван watchdog'ом (а не сбоем провайдера).
var ErrStalled = errors.New("шаг завис")

type stallState struct {
	step       int
	stepStart  time.Time
	phase      string // "model" | "tool"
	detail     string // «Жду ответ модели» / «Запускаю: go test ./…»
	model      string
	tools      int
	lastAlive  time.Time
	lastAliveD string
}

// stallWatch — состояние контроля одного прогона.
type stallWatch struct {
	mu     sync.Mutex
	limit  time.Duration
	st     stallState
	dead   bool
	reason string
}

func newStallWatch(limit time.Duration) *stallWatch {
	now := time.Now()
	return &stallWatch{
		limit: limit,
		st: stallState{
			stepStart:  now,
			lastAlive:  now,
			lastAliveD: "старт прогона",
			phase:      "model",
			detail:     "Жду ответ модели",
		},
	}
}

// onStep начинает новый шаг.
func (s *stallWatch) onStep(step int, model string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.st.step = step
	s.st.stepStart = now
	s.st.model = model
	s.st.phase = "model"
	s.st.detail = "Жду ответ модели"
	s.st.lastAlive = now
	s.st.lastAliveD = "начало шага"
}

// onPhase переключает фазу: ожидание модели или выполнение инструмента.
func (s *stallWatch) onPhase(phase, detail string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.phase = phase
	if detail != "" {
		s.st.detail = detail
	}
}

// touch отмечает признак жизни (пришла дельта стрима, ответ модели, итог инструмента).
func (s *stallWatch) touch(what string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.lastAlive = time.Now()
	if what != "" {
		s.st.lastAliveD = what
	}
}

// onToolDone учитывает завершённый инструмент (для разбора затыка).
func (s *stallWatch) onToolDone() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.st.tools++
	s.mu.Unlock()
	s.touch("инструмент завершён")
}

// verdict возвращает текст разбора, если тишина превысила лимит.
func (s *stallWatch) verdict(now time.Time) (string, bool) {
	if s == nil || s.limit <= 0 {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dead {
		return "", false
	}
	quiet := now.Sub(s.st.lastAlive)
	if quiet < s.limit {
		return "", false
	}
	s.dead = true
	s.reason = s.explainLocked(now, quiet)
	return s.reason, true
}

// stalledText возвращает разбор, если затык уже зафиксирован (иначе пусто).
func (s *stallWatch) stalledText() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

func (s *stallWatch) explainLocked(now time.Time, quiet time.Duration) string {
	var b strings.Builder
	step := s.st.step
	if step <= 0 {
		step = 1
	}
	fmt.Fprintf(&b, "⚠️ Шаг %d прерван: %s без признаков жизни.\n", step, humanDuration(quiet))
	fmt.Fprintf(&b, "Ждали: %s · всего в прогоне: %s\n", s.st.detail, humanDuration(now.Sub(s.st.stepStart)))
	if s.st.model != "" {
		fmt.Fprintf(&b, "Модель: %s\n", s.st.model)
	}
	fmt.Fprintf(&b, "Инструментов выполнено за прогон: %d\n", s.st.tools)
	b.WriteString("Почему так бывает: ")
	switch s.st.phase {
	case "tool":
		b.WriteString("инструмент не завершился — обычно это зависшая команда (сборка, скрипт), SSH или ожидание ввода.")
		b.WriteString("\nЧто делать: проверить команду и её таймаут, запустить её вручную в терминале, при необходимости сократить шаг.")
	default:
		b.WriteString("провайдер не прислал ни одного токена — сеть, таймаут, перегрузка или слишком большой контекст для этой модели.")
		b.WriteString("\nЧто делать: сменить модель или провайдера, уменьшить контекст (чаще сжимать историю), проверить сеть/ключ.")
	}
	b.WriteString("\nПрерванный шаг можно повторить: напишите «продолжай» — агент начнёт с последнего понятного состояния.")
	return b.String()
}

// humanDuration — «9 мин 12 с», «1 ч 04 мин», «45 с».
func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%d ч %02d мин", h, m)
	case m > 0:
		return fmt.Sprintf("%d мин %02d с", m, sec)
	default:
		return fmt.Sprintf("%d с", sec)
	}
}
