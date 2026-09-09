package appmeta

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.5.24"
)

// Highlights are author-written release notes for the Welcome splash (not git log).
// Keep at most 5 items; update manually for each release.
var HighlightsRU = []string{
	"Кэш LLM: hit/miss % в шапке; стабильный system + turn-rules",
	"Compact истории только между ходами; sticky-модель в сессии",
	"Ctrl+Shift+S — открыть/закрыть Settings",
	"Мульти-запросы: pending над вводом, Send now прерывает агента",
	"Чаты и настройки только в NotCursor — не в ~/.cursor",
}

var HighlightsEN = []string{
	"LLM cache: hit/miss % in header; stable system + turn-rules",
	"History compact between turns only; sticky model per session",
	"Ctrl+Shift+S toggles Settings",
	"Multi-request: pending above input; Send now interrupts the agent",
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
