// Edited by nc.ai 0.8.5.1 on new-admin-pc 2026-10-12 17:51:41
// Edited by nc.ai 0.8.5.2 on macbook 2026-10-12 20:42:49
// Edited by nc.ai 0.8.5.4 on new-admin-pc 2026-10-13 10:45:48
// Edited by nc.ai 0.8.5.5 on macbook 2026-10-13 21:22:17
// Edited by nc.ai 0.8.6.3 on new-admin-pc 2026-10-14 11:08:20
// Edited by nc.ai 0.8.6.3 on Papasha 2026-10-16 14:20:34
package appmeta

import "time"

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.8.7.7"
)

// DeepSeekProRetireRFC3339 is the announced V4 Pro retirement instant, empty
// when the provider keeps serving the model.
//
// On 2026-09-10 DeepSeek announced that V4 Pro continues to be served after
// 2026-09-14 with unchanged billing ("we have decided to continue providing API
// services for DeepSeek V4 Pro after September 14, 2026"), and Models & Pricing
// still lists deepseek-v4-pro. So the retirement is called off: while the date is
// empty the model is treated as alive. If DeepSeek announces a new date, set it
// here again — the cost paths in internal/costing follow this value.
// See https://api-docs.deepseek.com/updates
const DeepSeekProRetireRFC3339 = ""

// DeepSeekProRetireAt returns the V4 Pro retirement instant (Beijing time); the
// zero time means "no announced retirement".
func DeepSeekProRetireAt() time.Time {
	if DeepSeekProRetireRFC3339 == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, DeepSeekProRetireRFC3339)
	if err != nil {
		return time.Time{}
	}
	return t
}

// DeepSeekProRetired reports whether V4 Pro is retired at the given instant.
// Without an announced date the model is not retired.
func DeepSeekProRetired(now time.Time) bool {
	return retiredAt(DeepSeekProRetireAt(), now)
}

// retiredAt answers the same question for an arbitrary instant, so both branches
// stay testable while the announced date is empty.
func retiredAt(at time.Time, now time.Time) bool {
	if at.IsZero() {
		return false
	}
	return !now.Before(at)
}

// Highlights are author-written release notes for the Welcome splash (not git log).
// Keep at most 5 items; update manually for each release.
var HighlightsRU = []string{
	"Обновление больше не обрывает прогон: пока идёт работа, кнопка «Скачать и перезапустить» заблокирована — раньше установка убивала прогон и теряла несохранённый ответ",
	"Замок проекта стал цветным: закрытый — красный, открытый — зелёный, с бликом; при наведении закрытый подрагивает, открытый качает дужкой",
	"Индикаторы двигаются даже в RDP-сессии: системный флаг reduced-motion больше не выключает вращение кольца и анимацию замка",
	"Финальный ответ в конце длинного прогона больше не обрывается на полуслове — этот путь тоже распознаёт обрыв",
	"Рабочее пространство из нескольких папок и multitask (запросы идут параллельно) — на месте",
}

var HighlightsEN = []string{
	"An update no longer kills a run: while work is in progress the \"Download and restart\" button is disabled — installing used to abort the run and lose the unsaved answer",
	"The project lock is colourful now: red when locked, green when open, with a gloss; on hover the locked one rattles, the open one swings its shackle",
	"Indicators animate even in an RDP session: the system reduced-motion flag no longer kills the ring spin and the lock animation",
	"The final answer at the end of a long run no longer stops mid-sentence — that path detects a cut-off too",
	"Multi-folder workspace and Multitask (requests run in parallel) stay in place",
}

// UICopy is localized chrome for the Welcome splash.
type UICopy struct {
	Eyebrow  string
	Version  string // prefix before version number, e.g. "версия" / "version"
	WhatsNew string
	Continue string
}

var CopyRU = UICopy{
	Eyebrow:  "Добро пожаловать",
	Version:  "версия",
	WhatsNew: "Что нового",
	Continue: "Продолжить",
}

var CopyEN = UICopy{
	Eyebrow:  "Welcome",
	Version:  "version",
	WhatsNew: "What's new",
	Continue: "Continue",
}

// IsRussianLocale reports whether lang looks like a Russian UI locale (e.g. ru, ru-RU).
func IsRussianLocale(lang string) bool {
	l := normalizeLang(lang)
	return l == "ru" || hasPrefixFold(l, "ru-") || hasPrefixFold(l, "ru_")
}

// HighlightsFor returns RU notes for Russian locales, otherwise EN.
func HighlightsFor(lang string) []string {
	src := HighlightsEN
	if IsRussianLocale(lang) {
		src = HighlightsRU
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// CopyFor returns localized splash labels.
func CopyFor(lang string) UICopy {
	if IsRussianLocale(lang) {
		return CopyRU
	}
	return CopyEN
}

func normalizeLang(lang string) string {
	l := ""
	for _, r := range lang {
		if r == ' ' || r == '\t' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			l += string(r + ('a' - 'A'))
			continue
		}
		l += string(r)
	}
	return l
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}
