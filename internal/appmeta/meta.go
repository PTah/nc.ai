// Edited by nc.ai 0.8.5.1 on new-admin-pc 2026-10-12 17:51:41
// Edited by nc.ai 0.8.5.2 on macbook 2026-10-12 20:42:49
// Edited by nc.ai 0.8.5.4 on new-admin-pc 2026-10-13 10:45:48
// Edited by nc.ai 0.8.5.5 on macbook 2026-10-13 21:22:17
// Edited by nc.ai 0.8.6.3 on new-admin-pc 2026-10-14 11:08:20
// Edited by nc.ai 0.8.6.3 on Papasha 2026-10-16 14:20:34
// Edited by nc.ai 0.8.7.7 on macbook 2026-10-16 22:22:13
// Edited by nc.ai 0.8.7.7 on Papasha 2026-10-16 22:59:27
// Edited by nc.ai 0.8.8.0 on macbook 2026-10-18 19:23:36
// Edited by nc.ai 0.8.8.2 on new-admin-pc 2026-10-19 08:03:00
// Edited by nc.ai 0.8.8.3 on new-admin-pc 2026-10-19 10:40:10
// Edited by nc.ai 0.8.8.4 on new-admin-pc 2026-10-19 11:04:25
// Edited by nc.ai 0.8.8.5 on new-admin-pc 2026-10-19 11:25:13
// Edited by nc.ai 0.8.8.6 on new-admin-pc 2026-10-19 11:47:53
// Edited by nc.ai 0.8.9.0 on Papasha 2026-10-19 15:52:10
// Edited by nc.ai 0.8.8.6 on new-admin-pc 2026-10-19 15:53:48
// Edited by nc.ai 0.8.9.1 on Papasha 2026-10-19 16:10:00
// Edited by nc.ai 0.8.9.0 on new-admin-pc 2026-10-19 16:05:50
// Edited by nc.ai 0.8.9.2 on Papasha 2026-10-19 16:24:00
// Edited by nc.ai 0.8.9.0 on new-admin-pc 2026-10-19 16:22:02
// Edited by nc.ai 0.8.9.2 on macbook 2026-10-19 20:37:39
package appmeta

import "time"

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.8.9.4"
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
	"Кнопки разделов в настройках закреплены сверху: «Интерфейс», «Система» и другие переключаются без прокрутки наверх",
	"Второй экземпляр приложения больше не открывается: одно окно, без гонки за settings.json и черновики",
	"Иконки темы и настроек закреплены справа в шапке: больше не уезжают на отдельную строку при длинном балансе — в тёмной и светлой теме одинаково",
	"Новый дизайн под macOS и Windows 11: стеклянная шапка, слоистые панели, мягкие тени; переключатель темы и шестерёнка в шапке",
	"Пузырьки сообщений, вкладки, поле ввода и модальные окна перерисованы; несколько папок в рабочем пространстве и запросы, идущие параллельно, работают как раньше",
}

var HighlightsEN = []string{
	"Settings section buttons are pinned to the top: switching between Interface, System and the rest no longer requires scrolling back up",
	"A second app instance no longer opens: one window, no race over settings.json and drafts",
	"Theme and settings icons are pinned to the top-right of the header: they no longer drop onto a separate row when the balance is long — identical in dark and light themes",
	"Redesigned for macOS and Windows 11: glass header, layered panels, soft shadows; theme toggle and gear live in the header",
	"Message bubbles, tabs, the composer and modals are redrawn; the multi-folder workspace and parallel requests behave as before",
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
