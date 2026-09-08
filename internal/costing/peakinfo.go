package costing

import (
	"fmt"
	"strings"
	"time"
)

// PeakInfo is UI status for DeepSeek peak billing (local-time windows).
type PeakInfo struct {
	Peak           bool   `json:"peak"`
	Tooltip        string `json:"tooltip"`
	WindowsLocal   string `json:"windowsLocal"`
	WindowsBeijing string `json:"windowsBeijing"`
}

// DeepSeekPeakInfoNow returns whether DeepSeek is currently in peak and localized copy.
func DeepSeekPeakInfoNow(lang string, now time.Time, loc *time.Location) PeakInfo {
	if loc == nil {
		loc = time.Local
	}
	if now.IsZero() {
		now = time.Now()
	}
	peak := IsPeak(now.UTC())
	winsLocal := FormatPeakWindowsLocal(loc)
	winsBJ := "09:00–12:00, 14:00–18:00 (Beijing, Mon–Fri)"
	tip := peakTooltip(lang, peak, winsLocal)
	return PeakInfo{
		Peak:           peak,
		Tooltip:        tip,
		WindowsLocal:   winsLocal,
		WindowsBeijing: winsBJ,
	}
}

func peakTooltip(lang string, peak bool, winsLocal string) string {
	ru := isRussianLang(lang)
	if peak {
		if ru {
			return "Вы работаете в высокозагруженные часы, цена запросов удвоена (" + winsLocal + ")"
		}
		return "You are in peak hours — request prices are doubled (" + winsLocal + ")"
	}
	if ru {
		return "Сейчас off-peak для DeepSeek (дешевле). Пик: " + winsLocal
	}
	return "DeepSeek is off-peak now (cheaper). Peak: " + winsLocal
}

func isRussianLang(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return l == "ru" || strings.HasPrefix(l, "ru-") || strings.HasPrefix(l, "ru_")
}

// FormatPeakWindowsLocal converts configured UTC peak windows into local clock ranges.
func FormatPeakWindowsLocal(loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	wins := currentPeakWindows()
	if len(wins) == 0 {
		wins = DefaultPeakWindows()
	}
	// Anchor on a fixed UTC Monday so weekday conversion is stable.
	base := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	parts := make([]string, 0, len(wins))
	for _, w := range wins {
		start := base.Add(time.Duration(w.StartMin) * time.Minute).In(loc)
		end := base.Add(time.Duration(w.EndMin) * time.Minute).In(loc)
		parts = append(parts, fmt.Sprintf("%02d:%02d–%02d:%02d", start.Hour(), start.Minute(), end.Hour(), end.Minute()))
	}
	return strings.Join(parts, ", ") + " (local, Mon–Fri)"
}
