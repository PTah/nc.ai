package costing

import (
	"strings"
	"testing"
	"time"
)

func TestParseDeepSeekPricingHTML(t *testing.T) {
	html := `
<table>
<tr><td rowspan="2">1M INPUT TOKENS<br>(CACHE HIT)</td><td>OFF-PEAK</td><td>$0.007</td><td>$0.022</td><td>$0.007</td></tr>
<tr><td>PEAK</td><td>$0.014</td><td>$0.044</td><td>$0.014</td></tr>
<tr><td rowspan="2">1M INPUT TOKENS<br>(CACHE MISS)</td><td>OFF-PEAK</td><td>$0.22</td><td>$0.66</td><td>$0.22</td></tr>
<tr><td>PEAK</td><td>$0.44</td><td>$1.32</td><td>$0.44</td></tr>
<tr><td rowspan="2">1M OUTPUT TOKENS</td><td>OFF-PEAK</td><td>$0.66</td><td>$1.98</td><td>$0.66</td></tr>
<tr><td>PEAK</td><td>$1.32</td><td>$3.96</td><td>$1.32</td></tr>
</table>
<p>(1) Off-peak rates are half of the peak rates. Peak hours are 01:00 - 04:00 and 06:00 - 10:00 UTC, Monday through Friday (all other hours are off-peak).</p>
`
	snap, err := ParseDeepSeekPricingHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	flash := snap.Peak["deepseek-v4-flash"]
	pro := snap.Peak["deepseek-v4-pro"]
	if !almost(flash.InputHit, 0.014) || !almost(flash.InputMiss, 0.44) || !almost(flash.Completion, 1.32) {
		t.Fatalf("flash peak = %+v", flash)
	}
	if !almost(pro.InputHit, 0.044) || !almost(pro.InputMiss, 1.32) || !almost(pro.Completion, 3.96) {
		t.Fatalf("pro peak = %+v", pro)
	}
	if len(snap.Windows) != 2 || snap.Windows[0].StartMin != 60 || snap.Windows[0].EndMin != 240 {
		t.Fatalf("windows = %+v", snap.Windows)
	}
	if snap.Windows[1].StartMin != 360 || snap.Windows[1].EndMin != 600 {
		t.Fatalf("windows[1] = %+v", snap.Windows[1])
	}
}

func TestParseZaiPricingMarkdown(t *testing.T) {
	md := `
| Model         | Input              | Cached Input       | Cached Input Storage | Output            |
| GLM-5.3-Flash | ~~\$0.15~~ \$0.075 | ~~\$0.03~~ \$0.015 | Limited-time Free    | ~~\$0.50~~ \$0.25 |
| GLM-5.3       | \$1.4              | \$0.26             | Limited-time Free    | \$4.4             |
| GLM-4.7-Flash | Free               | Free               | Free                 | Free              |
| GLM-4.7-FlashX| \$0.07             | \$0.01             | Limited-time Free    | \$0.4             |
`
	sheet, err := ParseZaiPricingMarkdown(md)
	if err != nil {
		t.Fatal(err)
	}
	flash := sheet["glm-5.3-flash"]
	if !almost(flash.InputMiss, 0.075) || !almost(flash.InputHit, 0.015) || !almost(flash.Completion, 0.25) {
		t.Fatalf("promo flash = %+v", flash)
	}
	full := sheet["glm-5.3"]
	if !almost(full.InputMiss, 1.4) || !almost(full.InputHit, 0.26) || !almost(full.Completion, 4.4) {
		t.Fatalf("glm-5.3 = %+v", full)
	}
	if !pricesEqual(sheet["glm-4.7-flash"], Prices{}) {
		t.Fatalf("free flash should be zero: %+v", sheet["glm-4.7-flash"])
	}
	fx := sheet["glm-4.7-flashx"]
	if !almost(fx.InputMiss, 0.07) || !almost(fx.Completion, 0.4) {
		t.Fatalf("flashx = %+v", fx)
	}
}

func TestDeepSeekCheckDueBeijing(t *testing.T) {
	bj := beijingLoc()

	now := time.Date(2026, 9, 10, 15, 0, 0, 0, bj)
	if !deepSeekCheckDue(time.Time{}, now) {
		t.Fatal("zero last should be due")
	}

	// Before 13:00 Beijing never triggers a refresh.
	early := time.Date(2026, 9, 10, 10, 0, 0, 0, bj)
	if deepSeekCheckDue(time.Time{}, early) {
		t.Fatal("before 13:00 Beijing should not be due")
	}

	// A check after 13:00 Beijing on the same Beijing day is not due again.
	if deepSeekCheckDue(time.Date(2026, 9, 10, 14, 0, 0, 0, bj), now) {
		t.Fatal("same Beijing day after rotate should not be due")
	}

	// A pre-rotate check the same Beijing day may be stale, so it is due again.
	if !deepSeekCheckDue(time.Date(2026, 9, 10, 9, 0, 0, 0, bj), now) {
		t.Fatal("pre-rotate check same Beijing day should be due again")
	}

	// Previous Beijing day is due.
	if !deepSeekCheckDue(time.Date(2026, 9, 9, 20, 0, 0, 0, bj), now) {
		t.Fatal("previous Beijing day should be due")
	}

	// Same Beijing instant expressed in UTC must not change the decision.
	utc14BJ := time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC) // 14:00 Beijing
	if deepSeekCheckDue(utc14BJ, now) {
		t.Fatal("same Beijing day via UTC should not be due")
	}
}

func TestFormatPeakWindowsLocalUTCPlus10(t *testing.T) {
	SetPeakWindows(DefaultPeakWindows())
	loc := time.FixedZone("UTC+10", 10*3600)
	got := FormatPeakWindowsLocal(loc)
	want := "11:00–14:00, 16:00–20:00 (local, Mon–Fri)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIsPeakOfficialWindows(t *testing.T) {
	SetPeakWindows(DefaultPeakWindows())
	// Mon 01:30 UTC → peak
	if !IsPeak(time.Date(2026, 9, 7, 1, 30, 0, 0, time.UTC)) {
		t.Fatal("Mon 01:30 should be peak")
	}
	// Mon 05:00 UTC → gap between windows → off-peak
	if IsPeak(time.Date(2026, 9, 7, 5, 0, 0, 0, time.UTC)) {
		t.Fatal("Mon 05:00 should be off-peak")
	}
	// Mon 07:00 UTC → peak
	if !IsPeak(time.Date(2026, 9, 7, 7, 0, 0, 0, time.UTC)) {
		t.Fatal("Mon 07:00 should be peak")
	}
	// Mon 12:00 UTC → off-peak
	if IsPeak(time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("Mon 12:00 should be off-peak")
	}
	// Boundary: 04:00 exclusive end
	if IsPeak(time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC)) {
		t.Fatal("Mon 04:00 should be off-peak (exclusive end)")
	}
	if !IsPeak(time.Date(2026, 9, 7, 3, 59, 0, 0, time.UTC)) {
		t.Fatal("Mon 03:59 should be peak")
	}
	// Weekend always off-peak
	if IsPeak(time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC)) { // Sat
		t.Fatal("Saturday should be off-peak")
	}
}

func TestFetchDeepSeekLive(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	snap, err := FetchDeepSeekPricing(nil)
	if err != nil {
		t.Skip("network unavailable:", err)
	}
	if !SheetsEqual(snap.Peak, BuiltinDeepSeekPeak()) && !strings.Contains(snap.Source, "deepseek") {
		t.Logf("live sheet differs from builtin: %+v", snap.Peak)
	}
	flash := snap.Peak["deepseek-v4-flash"]
	if flash.InputMiss <= 0 || flash.Completion <= 0 {
		t.Fatalf("bad live flash: %+v", flash)
	}
}

func TestFetchZaiLive(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	snap, err := FetchZaiPricing(nil)
	if err != nil {
		t.Skip("network unavailable:", err)
	}
	if _, ok := snap.Sheet["glm-5.3"]; !ok {
		t.Fatalf("missing glm-5.3 in %+v", snap.Sheet)
	}
}
