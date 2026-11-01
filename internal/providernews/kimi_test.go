// Created by nc.ai 1.0.4 on Papasha 2026-11-01 12:30:10
package providernews

import "testing"

// changelog Kimi приходит в markdown: месяц строкой «**October 2026**», записи —
// заголовками «### …» или пунктами «* …». Тест фиксирует разбор и даты.
const kimiChangelogFixture = `# Platform Changelog

> Review historical Kimi Open Platform feature releases.

This page is updated periodically.

* пункт без месяца не должен попасть в список

**October 2026**

### 📦 Batch API now supports Kimi K3.1 Pro and Kimi K3

Batch inference is still priced at 60% of standard rates.

**September 2026**

### 🔍 Web Search API

Launch web searches through ` + "`/v1/tools/search`" + `.

### 💰 Billing method change

Account usage is now deducted at a uniform ratio.

**August 2026**

* The ` + "`kimi-k2.5`" + ` and all ` + "`moonshot-v1`" + ` series models were retired. Please migrate to [Kimi K3](https://platform.kimi.ai/docs/guide/kimi-k3-quickstart.md).
* Files API updates: newly generated file IDs now carry the ` + "`file_`" + ` prefix.
`

func TestParseKimiChangelog(t *testing.T) {
	items := parseKimiChangelog([]byte(kimiChangelogFixture))
	if len(items) != 5 {
		t.Fatalf("ожидалось 5 записей, получено %d: %+v", len(items), items)
	}
	want := []struct {
		date  string
		title string
	}{
		{"2026-10-01", "📦 Batch API now supports Kimi K3.1 Pro and Kimi K3"},
		{"2026-09-01", "🔍 Web Search API"},
		{"2026-09-01", "💰 Billing method change"},
		{"2026-08-01", "The kimi-k2.5 and all moonshot-v1 series models were retired. Please migrate to Kimi K3."},
		{"2026-08-01", "Files API updates: newly generated file IDs now carry the file_ prefix."},
	}
	for i, w := range want {
		got := items[i]
		if got.Date != w.date {
			t.Errorf("items[%d]: дата %q, ожидалась %q", i, got.Date, w.date)
		}
		if got.Title != w.title {
			t.Errorf("items[%d]: заголовок %q, ожидался %q", i, got.Title, w.title)
		}
		if got.Provider != "Kimi" {
			t.Errorf("items[%d]: провайдер %q", i, got.Provider)
		}
		if got.URL != kimiChangelogURL {
			t.Errorf("items[%d]: ссылка %q, ожидалась %q", i, got.URL, kimiChangelogURL)
		}
		if got.ID == "" {
			t.Errorf("items[%d]: пустой id", i)
		}
	}
}

// Ссылка в markdown должна превращаться в текст, иначе в заголовке новости
// видно «[Kimi K3](https://…)».
func TestPlainMarkdownStripsLinksAndEmphasis(t *testing.T) {
	got := plainMarkdown("**Жирный** и `code`, ссылка [Kimi K3](https://x.y/z) и *курсив*")
	want := "Жирный и code, ссылка Kimi K3 и курсив"
	if got != want {
		t.Fatalf("получено %q, ожидалось %q", got, want)
	}
}

func TestKimiMonthDate(t *testing.T) {
	cases := map[string]string{
		"October|2026":  "2026-10-01",
		"january|2025":  "2025-01-01",
		"Smarch|2026":   "",
		"December|2024": "2024-12-01",
	}
	for in, want := range cases {
		var month, year string
		for i := 0; i < len(in); i++ {
			if in[i] == '|' {
				month, year = in[:i], in[i+1:]
				break
			}
		}
		if got := kimiMonthDate(month, year); got != want {
			t.Errorf("kimiMonthDate(%q, %q) = %q, ожидалось %q", month, year, got, want)
		}
	}
}
