package costing

import (
	"sync"
	"time"
)

// PeakWindow is a half-open UTC minute range [StartMin, EndMin) on weekdays.
// Weekends are always off-peak (DeepSeek official schedule).
type PeakWindow struct {
	StartMin int `json:"startMin"` // minutes from midnight UTC
	EndMin   int `json:"endMin"`
}

// Official DeepSeek peak windows (as of 2026-09):
// 01:00–04:00 and 06:00–10:00 UTC, Monday–Friday.
// Source: https://api-docs.deepseek.com/quick_start/pricing/
var defaultPeakWindows = []PeakWindow{
	{StartMin: 1 * 60, EndMin: 4 * 60},
	{StartMin: 6 * 60, EndMin: 10 * 60},
}

const (
	DeepSeekPricingURL = "https://api-docs.deepseek.com/quick_start/pricing/"
	ZaiPricingURL      = "https://docs.z.ai/guides/overview/pricing.md"
	PriceCheckInterval = 7 * 24 * time.Hour
)

// Built-in DeepSeek peak rates (USD / 1M). Off-peak = half.
// Matches official docs; weekly refresh may override via SetDeepSeekPeakSheet.
var builtinDeepSeekPeak = map[string]Prices{
	"deepseek-v4-flash": {
		InputHit: 0.014, InputMiss: 0.44, Completion: 1.32,
	},
	"deepseek-v4-pro": {
		InputHit: 0.044, InputMiss: 1.32, Completion: 3.96,
	},
}

// Built-in Z.ai pay-as-you-go rates (USD / 1M).
// glm-5.3-flash list rates; live promo may lower them until refreshed from docs.
var builtinZaiSheet = map[string]Prices{
	"glm-5.3": {
		InputMiss: 1.4, InputHit: 0.26, Completion: 4.4,
	},
	"glm-5.3-flash": {
		InputMiss: 0.15, InputHit: 0.03, Completion: 0.50,
	},
	"glm-5.2": {
		InputMiss: 1.4, InputHit: 0.26, Completion: 4.4,
	},
	"glm-5.1": {
		InputMiss: 1.4, InputHit: 0.26, Completion: 4.4,
	},
	"glm-5": {
		InputMiss: 1.0, InputHit: 0.2, Completion: 3.2,
	},
	"glm-5-turbo": {
		InputMiss: 1.2, InputHit: 0.24, Completion: 4.0,
	},
	"glm-4.7": {
		InputMiss: 0.6, InputHit: 0.11, Completion: 2.2,
	},
	"glm-4.7-flashx": {
		InputMiss: 0.07, InputHit: 0.01, Completion: 0.4,
	},
	"glm-4.7-flash": {}, // free
	"glm-4.6": {
		InputMiss: 0.6, InputHit: 0.11, Completion: 2.2,
	},
	"glm-4.5": {
		InputMiss: 0.6, InputHit: 0.11, Completion: 2.2,
	},
	"glm-4.5-air": {
		InputMiss: 0.2, InputHit: 0.03, Completion: 1.1,
	},
	"glm-4.5-flash": {}, // free
}

var (
	sheetMu sync.RWMutex
	// live sheets start as builtins; Apply* replaces them after refresh / load from settings.
	liveDeepSeekPeak = cloneSheet(builtinDeepSeekPeak)
	liveZaiSheet     = cloneSheet(builtinZaiSheet)
	livePeakWindows  = cloneWindows(defaultPeakWindows)
)

func cloneSheet(in map[string]Prices) map[string]Prices {
	out := make(map[string]Prices, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneWindows(in []PeakWindow) []PeakWindow {
	out := make([]PeakWindow, len(in))
	copy(out, in)
	return out
}

// BuiltinDeepSeekPeak returns a copy of compile-time DeepSeek peak rates.
func BuiltinDeepSeekPeak() map[string]Prices { return cloneSheet(builtinDeepSeekPeak) }

// BuiltinZaiSheet returns a copy of compile-time Z.ai rates.
func BuiltinZaiSheet() map[string]Prices { return cloneSheet(builtinZaiSheet) }

// DefaultPeakWindows returns official DeepSeek peak windows.
func DefaultPeakWindows() []PeakWindow { return cloneWindows(defaultPeakWindows) }

// SetDeepSeekPeakSheet replaces the live DeepSeek peak rate card.
func SetDeepSeekPeakSheet(sheet map[string]Prices) {
	if len(sheet) == 0 {
		return
	}
	sheetMu.Lock()
	liveDeepSeekPeak = cloneSheet(sheet)
	sheetMu.Unlock()
}

// SetZaiSheet replaces the live Z.ai rate card (merges over builtin keys).
func SetZaiSheet(sheet map[string]Prices) {
	if len(sheet) == 0 {
		return
	}
	sheetMu.Lock()
	merged := cloneSheet(builtinZaiSheet)
	for k, v := range sheet {
		merged[k] = v
	}
	liveZaiSheet = merged
	sheetMu.Unlock()
}

// SetPeakWindows replaces DeepSeek peak UTC windows. Empty → defaults.
func SetPeakWindows(windows []PeakWindow) {
	sheetMu.Lock()
	if len(windows) == 0 {
		livePeakWindows = cloneWindows(defaultPeakWindows)
	} else {
		livePeakWindows = cloneWindows(windows)
	}
	sheetMu.Unlock()
}

func currentDeepSeekPeak() map[string]Prices {
	sheetMu.RLock()
	defer sheetMu.RUnlock()
	return cloneSheet(liveDeepSeekPeak)
}

func currentZaiSheet() map[string]Prices {
	sheetMu.RLock()
	defer sheetMu.RUnlock()
	return cloneSheet(liveZaiSheet)
}

func currentPeakWindows() []PeakWindow {
	sheetMu.RLock()
	defer sheetMu.RUnlock()
	return cloneWindows(livePeakWindows)
}

// SheetsEqual reports whether two price maps are numerically equal.
func SheetsEqual(a, b map[string]Prices) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !pricesEqual(va, vb) {
			return false
		}
	}
	return true
}

func pricesEqual(a, b Prices) bool {
	const eps = 1e-9
	return abs(a.InputHit-b.InputHit) < eps &&
		abs(a.InputMiss-b.InputMiss) < eps &&
		abs(a.Completion-b.Completion) < eps
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// WindowsEqual compares peak window lists.
func WindowsEqual(a, b []PeakWindow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
