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
package appmeta

import "time"

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "0.8.8.6"
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
	"Обновление без «троянного» PowerShell-дроппера: приложение само подменяет exe. «Не спрашивать» снимает всю пачку однотипных подтверждений",
	"«Разрешить и не спрашивать» — для инструмента целиком (во всех чатах); после ответа возвращает в прежний чат",
	"«этап»-карточки больше не показывают сырой JSON и сворачиваются под стрелку; инструменты — одним блоком «Исследовал»",
	"Баланс DeepSeek в шапке подписан понятно: баланс (доступно) · бонус (подарочные) · пополнение (оплачено вами)",
	"Когда приложение задаёт вопрос — подтверждение вне проекта, ask_user или закрытие — играет звук лопнувшего мыльного пузыря",
}

var HighlightsEN = []string{
	"Updates no longer use the trojan-like PowerShell dropper (the app swaps its own exe). \"Don't ask\" clears a whole batch of same-tool approvals",
	"\"Allow and don't ask\" applies to the tool across all chats; after answering, the app returns you to the previous chat",
	"Stage cards no longer show raw JSON and collapse under an arrow; tools fold into one \"Explored\" block",
	"The DeepSeek balance in the header is labelled plainly: balance (available) · bonus (granted) · top-up (paid by you)",
	"When the app asks a question — an out-of-project change approval, ask_user or the close prompt — it plays a soap-bubble pop",
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
