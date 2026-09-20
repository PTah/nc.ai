package appmeta

import "time"

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.6.31"
)

// DeepSeekProRetireRFC3339 is when the provider retires V4 Pro
// (12:00 Beijing on 2026-09-14). Requests are routed to V4.1 Flash and billed as Flash.
const DeepSeekProRetireRFC3339 = "2026-09-14T12:00:00+08:00"

// DeepSeekProRetireAt returns the V4 Pro retirement instant (Beijing time).
func DeepSeekProRetireAt() time.Time {
	t, err := time.Parse(time.RFC3339, DeepSeekProRetireRFC3339)
	if err != nil {
		return time.Date(2026, 9, 14, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	}
	return t
}

// DeepSeekProRetired reports whether V4 Pro is retired at the given instant.
func DeepSeekProRetired(now time.Time) bool {
	return !now.Before(DeepSeekProRetireAt())
}

// Highlights are author-written release notes for the Welcome splash (not git log).
// Keep at most 5 items; update manually for each release.
var HighlightsRU = []string{
	"Зависший шаг обрывается сам: если шаг молчит дольше 10 минут, прогон останавливается и объясняет, где встали и что делать",
	"При старте проверяются релизы на GitHub — по версии и по хешу сборки, поэтому видна и перезаливка файла",
	"План задач отмечает выполненное галочками и сам сворачивается, когда работа закончена",
	"В списке Projects слева от имени — иконка из папки проекта (или стандартная)",
	"Новый провайдер Qwen (Alibaba Cloud DashScope): qwen-max / qwen-plus / qwen-turbo, coder и vision",
}

var HighlightsEN = []string{
	"A stuck step aborts on its own: a step silent for over 10 minutes stops the run and explains where it hung and what to do",
	"GitHub releases are checked on start — by version and by build hash, so a re-uploaded file is noticed too",
	"The task plan ticks finished items and collapses by itself once the run is over",
	"Project list shows a folder icon (or a default glyph) beside each project name",
	"New provider Qwen (Alibaba Cloud DashScope): qwen-max / qwen-plus / qwen-turbo, coder and vision",
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
