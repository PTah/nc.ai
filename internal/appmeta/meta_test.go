package appmeta

import (
	"strings"
	"testing"
	"time"
)

func TestIsRussianLocale(t *testing.T) {
	for _, lang := range []string{"ru", "ru-RU", "ru_RU", "RU", " ru-ru "} {
		if !IsRussianLocale(lang) {
			t.Fatalf("expected Russian for %q", lang)
		}
	}
	for _, lang := range []string{"en", "en-US", "de-DE", ""} {
		if IsRussianLocale(lang) {
			t.Fatalf("expected non-Russian for %q", lang)
		}
	}
}

func TestHighlightsFor(t *testing.T) {
	ru := HighlightsFor("ru-RU")
	en := HighlightsFor("en-US")
	if len(ru) != 5 || len(en) != 5 {
		t.Fatalf("want 5 highlights, got ru=%d en=%d", len(ru), len(en))
	}
	if ru[0] == en[0] {
		t.Fatal("RU and EN highlights should differ")
	}
}

func TestDeepSeekProRetired(t *testing.T) {
	// DeepSeek called the retirement off on 2026-09-10 and still lists
	// deepseek-v4-pro in Models & Pricing, so without an announced date the model
	// stays alive — this is what the costing paths rely on.
	if DeepSeekProRetireRFC3339 != "" {
		t.Fatalf("ожидалось снятое уведомление об отставке, получено %q", DeepSeekProRetireRFC3339)
	}
	if !DeepSeekProRetireAt().IsZero() {
		t.Fatal("без объявленной даты момент отставки должен быть нулевым")
	}
	after := time.Date(2026, 9, 14, 12, 1, 0, 0, time.FixedZone("CST", 8*3600))
	if DeepSeekProRetired(after) {
		t.Fatal("pro должен считаться живым после отмены отставки")
	}

	// И логика на случай, если провайдер объявит новую дату.
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if retiredAt(time.Time{}, after) {
		t.Fatal("нулевая дата не должна считаться отставкой")
	}
	if retiredAt(at, at.Add(-time.Minute)) {
		t.Fatal("до объявленного момента модель должна быть доступна")
	}
	if !retiredAt(at, at.Add(time.Minute)) {
		t.Fatal("после объявленного момента модель должна считаться ушедшей")
	}
}

func TestHighlightsMentionProviderNews(t *testing.T) {
	found := false
	for _, line := range HighlightsRU {
		if strings.Contains(line, "Новости провайдеров") {
			found = true
		}
	}
	if !found {
		t.Fatal("в списке «что нового» нет упоминания новостей провайдеров")
	}
}
