package appmeta

import "time"

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.6.32"
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
	"В настройках — «Новости провайдеров»: что нового у DeepSeek, Z.AI, OpenRouter и Qwen (модели, цены, изменения API), обновление раз в сутки или по кнопке",
	"Зависший шаг обрывается сам: если шаг молчит дольше 10 минут, прогон останавливается и объясняет, где встали и что делать",
	"При старте проверяются релизы на GitHub — по версии и по хешу сборки, поэтому видна и перезаливка файла",
	"План задач отмечает выполненное галочками и сам сворачивается, когда работа закончена",
	"В списке Projects слева от имени — иконка из папки проекта (или стандартная)",
}

var HighlightsEN = []string{
	"Settings now include provider news: what's new at DeepSeek, Z.AI, OpenRouter and Qwen (models, prices, API changes), refreshed daily or on demand",
	"A stuck step aborts on its own: a step silent for over 10 minutes stops the run and explains where it hung and what to do",
	"GitHub releases are checked on start — by version and by build hash, so a re-uploaded file is noticed too",
	"The task plan ticks finished items and collapses by itself once the run is over",
	"Project list shows a folder icon (or a default glyph) beside each project name",
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
