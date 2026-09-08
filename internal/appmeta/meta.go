package appmeta

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.5.9"
)

// Highlights are author-written release notes for the Welcome splash (not git log).
// Keep at most 5 items; update manually for each release.
var HighlightsRU = []string{
	"Ширину колонки чата можно тянуть мышкой (края ленты сообщений)",
	"build.ps1 умеет пересобрать и перезапустить уже открытый NotCursor",
	"Кнопка «Закрыть» в Model prices — внизу по центру",
	"Диалоги подтверждения внутри приложения (без заголовка wails.localhost)",
	"Исправлен ПКМ «Закрыть папку проекта»",
}

var HighlightsEN = []string{
	"Drag the chat column edges to resize its width",
	"build.ps1 can rebuild and restart a running NotCursor",
	"Model prices Close button centered at the bottom",
	"In-app confirm dialogs (no wails.localhost title bar)",
	"Fixed right-click “Close project folder”",
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
