package costing

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	rePeakHoursUTC = regexp.MustCompile(`(?i)Peak hours are\s+(\d{1,2}):(\d{2})\s*-\s*(\d{1,2}):(\d{2})\s+and\s+(\d{1,2}):(\d{2})\s*-\s*(\d{1,2}):(\d{2})\s+UTC`)
	// Current docs: PEAK + flash + pro. Older pages also had a vision column (same as flash).
	rePeakRow     = regexp.MustCompile(`(?is)>PEAK</td>\s*<td>\$([0-9.]+)</td>\s*<td>\$([0-9.]+)</td>(?:\s*<td>\$([0-9.]+)</td>)?`)
	reSectionPeak = regexp.MustCompile(`(?is)(CACHE HIT|CACHE MISS|OUTPUT TOKENS).*?>PEAK</td>\s*<td>\$([0-9.]+)</td>\s*<td>\$([0-9.]+)</td>(?:\s*<td>\$([0-9.]+)</td>)?`)
)

// DeepSeekPricingSnapshot is parsed peak rates + peak windows from the docs page.
type DeepSeekPricingSnapshot struct {
	Peak     map[string]Prices
	Windows  []PeakWindow
	Source   string
	Fetched  time.Time
}

// FetchDeepSeekPricing downloads and parses official DeepSeek Models & Pricing.
func FetchDeepSeekPricing(ctx context.Context) (DeepSeekPricingSnapshot, error) {
	body, err := httpGet(ctx, DeepSeekPricingURL)
	if err != nil {
		return DeepSeekPricingSnapshot{}, err
	}
	snap, err := ParseDeepSeekPricingHTML(string(body))
	if err != nil {
		return DeepSeekPricingSnapshot{}, err
	}
	snap.Source = DeepSeekPricingURL
	snap.Fetched = time.Now().UTC()
	return snap, nil
}

// ParseDeepSeekPricingHTML extracts peak USD/1M rates for flash/pro (+ vision→flash).
func ParseDeepSeekPricingHTML(html string) (DeepSeekPricingSnapshot, error) {
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	hitFlash, hitPro, missFlash, missPro, outFlash, outPro := 0.0, 0.0, 0.0, 0.0, 0.0, 0.0
	gotHit, gotMiss, gotOut := false, false, false

	// Prefer section-aware matches (CACHE HIT / MISS / OUTPUT + PEAK row).
	for _, m := range reSectionPeak.FindAllStringSubmatch(html, -1) {
		if len(m) < 4 {
			continue
		}
		kind := strings.ToUpper(m[1])
		a, b := parseFloat(m[2]), parseFloat(m[3]) // flash, pro (optional vision column ignored)
		switch {
		case strings.Contains(kind, "CACHE HIT"):
			hitFlash, hitPro, gotHit = a, b, true
		case strings.Contains(kind, "CACHE MISS"):
			missFlash, missPro, gotMiss = a, b, true
		case strings.Contains(kind, "OUTPUT"):
			outFlash, outPro, gotOut = a, b, true
		}
	}

	if !gotHit || !gotMiss || !gotOut {
		// Fallback: collect PEAK rows in document order (hit, miss, output).
		rows := rePeakRow.FindAllStringSubmatch(html, -1)
		if len(rows) < 3 {
			return DeepSeekPricingSnapshot{}, fmt.Errorf("deepseek pricing: could not find PEAK rate rows")
		}
		hitFlash, hitPro = parseFloat(rows[0][1]), parseFloat(rows[0][2])
		missFlash, missPro = parseFloat(rows[1][1]), parseFloat(rows[1][2])
		outFlash, outPro = parseFloat(rows[2][1]), parseFloat(rows[2][2])
	}

	if hitFlash <= 0 || missFlash <= 0 || outFlash <= 0 {
		return DeepSeekPricingSnapshot{}, fmt.Errorf("deepseek pricing: invalid flash peak rates")
	}
	if hitPro <= 0 || missPro <= 0 || outPro <= 0 {
		return DeepSeekPricingSnapshot{}, fmt.Errorf("deepseek pricing: invalid pro peak rates")
	}

	peak := map[string]Prices{
		"deepseek-v4-flash": {InputHit: hitFlash, InputMiss: missFlash, Completion: outFlash},
		"deepseek-v4-pro":   {InputHit: hitPro, InputMiss: missPro, Completion: outPro},
	}

	windows := DefaultPeakWindows()
	if m := rePeakHoursUTC.FindStringSubmatch(html); len(m) == 9 {
		windows = []PeakWindow{
			{StartMin: hm(m[1], m[2]), EndMin: hm(m[3], m[4])},
			{StartMin: hm(m[5], m[6]), EndMin: hm(m[7], m[8])},
		}
	}

	return DeepSeekPricingSnapshot{Peak: peak, Windows: windows}, nil
}

func hm(h, m string) int {
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	return hh*60 + mm
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NotCursor.ai price-check/1.0")
	req.Header.Set("Accept", "text/html,text/markdown,*/*")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d fetching %s", res.StatusCode, url)
	}
	return data, nil
}
