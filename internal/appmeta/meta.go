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
// Edited by nc.ai 0.8.9.6 on Papasha 2026-10-24 16:55:51
// Edited by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:41:35
// Edited by nc.ai 0.8.9.7 on macbook 2026-10-29 21:49:23
// Edited by nc.ai 0.9.4 on new-admin-pc 2026-10-30 08:46:25
// Edited by nc.ai 1.0.0 on Papasha 2026-10-31 10:40:30
// Edited by nc.ai 1.0.1 on macbook 2026-10-31 22:56:21
// Edited by nc.ai 1.0.3 on Papasha 2026-11-01 12:10:05
// Edited by nc.ai 1.0.0 on new-admin-pc 2026-11-02 09:59:06
// Edited by nc.ai 1.1.3 on macbook 2026-11-08 18:54:24
package appmeta

import "time"

// App identity. Bump Version when you want the Welcome splash to appear again.
const (
	Name    = "NotCursor.ai"
	Version = "1.2.1"
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
	"Исправлено: после долгого простоя окно больше не остаётся белым пятном — UI мягко перерисовывается",
	"Workspace из нескольких папок: Menu → New workspace… — название и 2–4 папки в одном чате, как в Cursor",
	"Папки можно бросить в окно; агент видит все сразу (пути api/main.go), git и терминал — по имени папки",
	"Рабочее пространство из нескольких папок и запросы, идущие параллельно, работают как раньше",
	"Дерево файлов, поиск, grep, find_symbol, правила Cursor и память работают по всем папкам сразу",
}

var HighlightsEN = []string{
	"Fixed: after long idle the window no longer stays a blank white surface — the UI soft-recovers",
	"Multi-folder workspace: Menu → New workspace… — a name and 2–4 folders in one chat, like Cursor",
	"Drop folders into the window; the agent sees them all (paths like api/main.go); git and terminal take a folder name",
	"Requests in parallel chats and the multi-folder workspace keep working as before",
	"The file tree, search, grep, find_symbol, Cursor rules and memory cover every folder at once",
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
