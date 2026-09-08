package costing

import (
	"context"
	"log"
	"strings"
	"time"
)

// RefreshResult summarizes one weekly price check.
type RefreshResult struct {
	DeepSeekChecked bool
	DeepSeekUpdated bool
	DeepSeekErr     string
	ZaiChecked      bool
	ZaiUpdated      bool
	ZaiErr          string
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
}

// deepSeekCheckDue is true when we have never checked, or the last check was on a
// previous local calendar day (once per day on launch / ticker).
func deepSeekCheckDue(last, now time.Time) bool {
	if last.IsZero() {
		return true
	}
	ly, lm, ld := last.In(time.Local).Date()
	ny, nm, nd := now.In(time.Local).Date()
	return ly != ny || lm != nm || ld != nd
}

// RefreshIfDue fetches official docs when due.
// DeepSeek: at most once per local calendar day. Z.ai: weekly interval.
// force=true always fetches. Updates live sheets and persists when rates change.
func RefreshIfDue(ctx context.Context, store PriceStore, force bool) RefreshResult {
	var out RefreshResult
	if store == nil {
		return out
	}
	now := time.Now().UTC()

	dsDue := force || deepSeekCheckDue(store.DeepSeekPricesCheckedAt(), now)
	if dsDue {
		out.DeepSeekChecked = true
		snap, err := FetchDeepSeekPricing(ctx)
		if err != nil {
			out.DeepSeekErr = err.Error()
			log.Printf("costing: deepseek price check failed: %v", err)
		} else {
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
	if zaiDue {
		out.ZaiChecked = true
		snap, err := FetchZaiPricing(ctx)
		if err != nil {
			out.ZaiErr = err.Error()
			log.Printf("costing: z.ai price check failed: %v", err)
		} else {
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
	return out
}
