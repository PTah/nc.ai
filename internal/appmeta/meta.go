package appmeta

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.5.4"
)

// Highlights are author-written release notes for the Welcome splash (not git log).
// Keep at most 5 items; update manually for each release.
var HighlightsRU = []string{
	"Реализована логика работы с чатами и проектами (удалить, архивировать).",
	"Добавлена возможность просмотра и редактирования правил (двойной клик по названию правила)",
	"Подтверждение перед закрытием вкладки чата (×)",
	"Терминал подстраивается под высоту панели при ресайзе",
	"ПКМ по проекту: закрыть папку с удалением или архивацией чатов",
}

var HighlightsEN = []string{
	"Chat and project lifecycle: delete, archive, close folder",
	"View and edit Cursor rules (double-click a rule name)",
	"Confirm before closing a chat tab (×)",
	"Terminal fits panel height on resize",
	"Right-click a project to close it and handle its chats",
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
