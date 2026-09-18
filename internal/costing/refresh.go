package costing

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// RefreshResult summarizes one price check pass.
type RefreshResult struct {
	DeepSeekChecked bool
	DeepSeekUpdated bool
	DeepSeekErr     string
	ZaiChecked      bool
	ZaiUpdated      bool
	ZaiErr          string
	OpenRouterChecked bool
	OpenRouterUpdated bool
	OpenRouterErr     string
}

// PriceStore persists live rate cards between app launches.
type PriceStore interface {
	DeepSeekPeakPrices() map[string]Prices
	DeepSeekPeakWindows() []PeakWindow
	DeepSeekPricesCheckedAt() time.Time
	SetDeepSeekPricing(peak map[string]Prices, windows []PeakWindow, checkedAt time.Time) error

	ZaiPrices() map[string]Prices
	ZaiPricesCheckedAt() time.Time
	SetZaiPricing(sheet map[string]Prices, checkedAt time.Time) error

	OpenRouterPrices() map[string]Prices
	OpenRouterPricesCheckedAt() time.Time
	OpenRouterModel() string
	SetOpenRouterPricing(sheet map[string]Prices, checkedAt time.Time) error
}

// ApplyPersisted loads saved sheets into the live costing tables (no network).
func ApplyPersisted(store PriceStore) {
	if store == nil {
		return
	}
	if sheet := store.DeepSeekPeakPrices(); len(sheet) > 0 {
		sanitizeFlashPeak(sheet, time.Now().UTC())
		SetDeepSeekPeakSheet(sheet)
	}
	if wins := store.DeepSeekPeakWindows(); len(wins) > 0 {
		SetPeakWindows(wins)
	}
	if sheet := store.ZaiPrices(); len(sheet) > 0 {
		SetZaiSheet(sheet)
	}
	if sheet := store.OpenRouterPrices(); len(sheet) > 0 {
		SetOpenRouterSheet(sheet)
	}
}

// deepSeekPriceRotateHour is the Beijing hour after which the current day's rate
// card is final: DeepSeek publishes price changes at 12:00 Beijing, so we wait
// until 13:00 Beijing before treating the day as already checked.
const (
	deepSeekPriceRotateHour = 13
	deepSeekPriceSlotMin    = deepSeekPriceRotateHour*60 + 5 // 13:05 Beijing
)

// beijingLoc returns DeepSeek's billing timezone (Asia/Shanghai).
func beijingLoc() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*60*60)
}

// deepSeekCheckDue is true when we have never checked, or the last effective check
// (one made after 13:00 Beijing) belongs to a previous Beijing calendar day.
// The local clock/DST never decides: DeepSeek rate cards are published in Beijing
// time, so only that calendar matters.
func deepSeekCheckDue(last, now time.Time) bool {
	bj := beijingLoc()
	nb := now.In(bj)
	if nb.Hour()*60+nb.Minute() < deepSeekPriceSlotMin {
		return false
	}
	if last.IsZero() {
		return true
	}
	ly, lm, ld := deepSeekEffectiveDay(last.In(bj))
	ny, nm, nd := nb.Date()
	return ly != ny || lm != nm || ld != nd
}

// deepSeekEffectiveDay maps a check to the Beijing day it validates: a check made
// before the rotate hour still belongs to the previous day, because that day's
// rates may not have been published yet.
func deepSeekEffectiveDay(t time.Time) (int, time.Month, int) {
	if t.Hour() < deepSeekPriceRotateHour {
		return t.AddDate(0, 0, -1).Date()
	}
	return t.Date()
}

// openRouterCheckDue allows one OpenRouter price check per Beijing day after 13:05 Beijing.
func openRouterCheckDue(last, now time.Time) bool {
	bj := beijingLoc()
	nb := now.In(bj)
	if nb.Hour()*60+nb.Minute() < deepSeekPriceSlotMin {
		return false
	}
	if last.IsZero() {
		return true
	}
	lb := last.In(bj)
	return lb.Year() != nb.Year() || lb.Month() != nb.Month() || lb.Day() != nb.Day()
}

// Price check providers, used as throttle keys.
const (
	providerDeepSeek   = "deepseek"
	providerZai        = "z.ai"
	providerOpenRouter = "openrouter"
)

// priceRetryAfter is how long a failed check is left alone. The refresh loop
// ticks every 15 minutes, and without this pause an unreachable endpoint
// produced the same error notice again and again.
const priceRetryAfter = time.Hour

var priceThrottle struct {
	mu   sync.Mutex
	next map[string]time.Time
}

// priceCheckAllowed reports whether the provider may be contacted now.
func priceCheckAllowed(provider string, now time.Time) bool {
	priceThrottle.mu.Lock()
	defer priceThrottle.mu.Unlock()
	next, ok := priceThrottle.next[provider]
	return !ok || !now.Before(next)
}

// priceCheckFailed parks the provider until now+priceRetryAfter.
func priceCheckFailed(provider string, now time.Time) {
	priceThrottle.mu.Lock()
	defer priceThrottle.mu.Unlock()
	if priceThrottle.next == nil {
		priceThrottle.next = map[string]time.Time{}
	}
	priceThrottle.next[provider] = now.Add(priceRetryAfter)
}

// priceCheckSucceeded clears the pause after a good fetch.
func priceCheckSucceeded(provider string) {
	priceThrottle.mu.Lock()
	defer priceThrottle.mu.Unlock()
	delete(priceThrottle.next, provider)
}

// RefreshIfDue fetches official docs when due.
// DeepSeek: at most once per Beijing calendar day, only after 13:05 Beijing.
// Z.ai: weekly interval.
// OpenRouter: at most once per Beijing calendar day, after 13:05 Beijing.
// force=true always fetches. Updates live sheets and persists when rates change.
func RefreshIfDue(ctx context.Context, store PriceStore, force bool) RefreshResult {
	var out RefreshResult
	if store == nil {
		return out
	}
	now := time.Now().UTC()

	dsDue := force || deepSeekCheckDue(store.DeepSeekPricesCheckedAt(), now)
	if dsDue && (force || priceCheckAllowed(providerDeepSeek, now)) {
		out.DeepSeekChecked = true
		snap, err := FetchDeepSeekPricing(ctx)
		if err != nil {
			out.DeepSeekErr = err.Error()
			priceCheckFailed(providerDeepSeek, now)
			log.Printf("costing: deepseek price check failed: %v", err)
		} else {
			priceCheckSucceeded(providerDeepSeek)
			sanitizeFlashPeak(snap.Peak, now)
			cur := currentDeepSeekPeak()
			curW := currentPeakWindows()
			changed := !SheetsEqual(cur, snap.Peak) || !WindowsEqual(curW, snap.Windows)
			SetDeepSeekPeakSheet(snap.Peak)
			SetPeakWindows(snap.Windows)
			if err := store.SetDeepSeekPricing(snap.Peak, snap.Windows, snap.Fetched); err != nil {
				out.DeepSeekErr = err.Error()
			} else if changed {
				out.DeepSeekUpdated = true
				log.Printf("costing: deepseek peak sheet updated from %s", snap.Source)
			} else {
				// Still stamp checkedAt so we do not re-hit the network every launch today.
				_ = store.SetDeepSeekPricing(snap.Peak, snap.Windows, snap.Fetched)
			}
		}
	}

	zaiDue := force || store.ZaiPricesCheckedAt().IsZero() ||
		now.Sub(store.ZaiPricesCheckedAt()) >= ZaiPriceCheckInterval
	if zaiDue && (force || priceCheckAllowed(providerZai, now)) {
		out.ZaiChecked = true
		snap, err := FetchZaiPricing(ctx)
		if err != nil {
			out.ZaiErr = err.Error()
			priceCheckFailed(providerZai, now)
			log.Printf("costing: z.ai price check failed: %v", err)
		} else {
			priceCheckSucceeded(providerZai)
			// Compare only overlapping keys that we care about + newly discovered glm-*.
			builtin := BuiltinZaiSheet()
			interesting := map[string]Prices{}
			for k, v := range snap.Sheet {
				if _, ok := builtin[k]; ok || strings.HasPrefix(k, "glm-") {
					interesting[k] = v
				}
			}
			cur := currentZaiSheet()
			changed := false
			for k, v := range interesting {
				if old, ok := cur[k]; !ok || !pricesEqual(old, v) {
					changed = true
					break
				}
			}
			SetZaiSheet(interesting)
			if err := store.SetZaiPricing(interesting, snap.Fetched); err != nil {
				out.ZaiErr = err.Error()
			} else if changed {
				out.ZaiUpdated = true
				log.Printf("costing: z.ai sheet updated from %s", snap.Source)
			}
		}
	}

	orDue := force || openRouterCheckDue(store.OpenRouterPricesCheckedAt(), now)
	if orDue && (force || priceCheckAllowed(providerOpenRouter, now)) {
		out.OpenRouterChecked = true
		wanted := openRouterWantedModels(store.OpenRouterModel())
		sheet, err := FetchOpenRouterPricing(ctx, wanted)
		if err != nil {
			out.OpenRouterErr = err.Error()
			priceCheckFailed(providerOpenRouter, now)
			log.Printf("costing: openrouter price check failed: %v", err)
		} else {
			priceCheckSucceeded(providerOpenRouter)
			cur := liveOpenRouterPrices()
			changed := false
			for k, v := range sheet {
				if old, ok := cur[k]; !ok || !pricesEqual(old, v) {
					changed = true
					break
				}
			}
			SetOpenRouterSheet(sheet)
			if err := store.SetOpenRouterPricing(sheet, now); err != nil {
				out.OpenRouterErr = err.Error()
			} else if changed {
				out.OpenRouterUpdated = true
				log.Printf("costing: openrouter sheet updated from %s", openRouterModelsURL)
			}
		}
	}

	return out
}
