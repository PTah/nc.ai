package appmeta

import "time"

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.5.27"
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
	"Агент: find_files/grep, read по строкам, get_env_info, карта проекта",
	"HITL: подтверждение опасных tools; умнее compact истории",
	"Проверка цен DeepSeek/OpenRouter/z.ai — системные сообщения в чате",
	"V4 Pro (DeepSeek) отключён с 14.09 — авто-выбор и расчёты идут по Flash",
	"Чаты и настройки только в NotCursor — не в ~/.cursor",
}

var HighlightsEN = []string{
	"Agent: find_files/grep, ranged read_file, get_env_info, project map",
	"HITL for dangerous tools; smarter history compact",
	"Price checks for DeepSeek/OpenRouter/z.ai — chat system notices",
	"DeepSeek V4 Pro retired on 2026-09-14 — auto routing and billing use Flash",
	"Chats/settings live under NotCursor — never ~/.cursor",
}

// UICopy is localized chrome for the Welcome splash.
type UICopy struct {
	Eyebrow   string
	Version   string // prefix before version number, e.g. "версия" / "version"
	WhatsNew  string
	Continue  string
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
