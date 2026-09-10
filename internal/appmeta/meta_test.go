package appmeta

import (
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
	before := time.Date(2026, 9, 14, 11, 59, 0, 0, time.FixedZone("CST", 8*3600))
	after := time.Date(2026, 9, 14, 12, 1, 0, 0, time.FixedZone("CST", 8*3600))
	if DeepSeekProRetired(before) {
		t.Fatal("pro must be available before 12:00 Beijing")
	}
	if !DeepSeekProRetired(after) {
		t.Fatal("pro must be retired after 12:00 Beijing")
	}
}
