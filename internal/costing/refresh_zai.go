package costing

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	reZaiRow = regexp.MustCompile(`(?m)^\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|`)
	reStrike = regexp.MustCompile(`~~\\?\$[0-9.]+~~`)
	reDollar = regexp.MustCompile(`\$([0-9]+(?:\.[0-9]+)?)`)
)

// ZaiPricingSnapshot is parsed Z.ai pay-as-you-go text/vision model rates.
type ZaiPricingSnapshot struct {
	Sheet   map[string]Prices
	Source  string
	Fetched time.Time
}

// FetchZaiPricing downloads and parses https://docs.z.ai/guides/overview/pricing.md
func FetchZaiPricing(ctx context.Context) (ZaiPricingSnapshot, error) {
	body, err := httpGet(ctx, ZaiPricingURL)
	if err != nil {
		return ZaiPricingSnapshot{}, err
	}
	sheet, err := ParseZaiPricingMarkdown(string(body))
	if err != nil {
		return ZaiPricingSnapshot{}, err
	}
	return ZaiPricingSnapshot{
		Sheet:   sheet,
		Source:  ZaiPricingURL,
		Fetched: time.Now().UTC(),
	}, nil
}

// ParseZaiPricingMarkdown extracts USD/1M Input / Cached Input / Output per GLM model.
// Effective (promo) price wins over strikethrough list price in a cell.
func ParseZaiPricingMarkdown(md string) (map[string]Prices, error) {
	md = strings.ReplaceAll(md, `\$`, `$`)
	out := map[string]Prices{}
	for _, m := range reZaiRow.FindAllStringSubmatch(md, -1) {
		if len(m) < 6 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" || strings.EqualFold(name, "Model") || strings.HasPrefix(name, ":") {
			continue
		}
		key := normalizeZaiModelName(name)
		if key == "" {
			continue
		}
		inputCell := strings.TrimSpace(m[2])
		cachedCell := strings.TrimSpace(m[3])
		// m[4] = Cached Input Storage (ignored)
		outCell := strings.TrimSpace(m[5])

		if isFreeCell(inputCell) && isFreeCell(outCell) {
			out[key] = Prices{}
			continue
		}
		miss, okMiss := cellUSD(inputCell)
		hit, okHit := cellUSD(cachedCell)
		comp, okOut := cellUSD(outCell)
		if !okMiss && !okOut {
			continue
		}
		if !okHit {
			hit = 0
		}
		if !okMiss {
			miss = 0
		}
		if !okOut {
			comp = 0
		}
		out[key] = Prices{InputMiss: miss, InputHit: hit, Completion: comp}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("zai pricing: no model rows parsed")
	}
	return out, nil
}

func normalizeZaiModelName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, " ", "")
	n = strings.TrimPrefix(n, "zai/")
	// Keep only GLM chat-ish models we bill in-app.
	if !strings.HasPrefix(n, "glm-") {
		return ""
	}
	// Drop agent/image/video-only rows that slipped through.
	if strings.Contains(n, "slide") || strings.Contains(n, "cogview") || strings.Contains(n, "cogvideo") {
		return ""
	}
	return n
}

func isFreeCell(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "free" || s == "—" || s == "-" || s == "\\" || s == ""
}

// cellUSD returns the effective USD amount in a markdown table cell.
// Prefers the last $amount after removing ~~strikethrough~~ list prices.
func cellUSD(cell string) (float64, bool) {
	cleaned := reStrike.ReplaceAllString(cell, " ")
	cleaned = strings.ReplaceAll(cleaned, `\$`, `$`)
	matches := reDollar.FindAllStringSubmatch(cleaned, -1)
	if len(matches) == 0 {
		// try without cleanup on original (no promo markup)
		matches = reDollar.FindAllStringSubmatch(strings.ReplaceAll(cell, `\$`, `$`), -1)
	}
	if len(matches) == 0 {
		return 0, false
	}
	return parseFloat(matches[len(matches)-1][1]), true
}
