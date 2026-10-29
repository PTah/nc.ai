// Created by nc.ai 0.9.4 on new-admin-pc 2026-10-30 09:05:19
package tools

import (
	"regexp"
	"strings"
	"testing"
)

func TestWrapUntrustedTags(t *testing.T) {
	out := wrapUntrusted("hello")
	if !strings.Contains(out, "hello") {
		t.Fatalf("content lost: %q", out)
	}
	re := regexp.MustCompile(`^<data-[0-9a-f]{8}>\nhello\n</data-[0-9a-f]{8}>$`)
	if !re.MatchString(out) {
		t.Fatalf("unexpected wrapping: %q", out)
	}
	open := out[1:strings.Index(out, ">")]
	closeTag := "</" + open + ">"
	if !strings.HasSuffix(out, closeTag) {
		t.Fatalf("open/close tags differ: %q", out)
	}
}

func TestWrapUntrustedEmpty(t *testing.T) {
	if got := wrapUntrusted(""); got != "" {
		t.Fatalf("empty must stay empty, got %q", got)
	}
}

func TestWrapUntrustedRandomTag(t *testing.T) {
	a := wrapUntrusted("x")
	b := wrapUntrusted("x")
	if a == b {
		t.Fatalf("tag must be random per call, got identical %q", a)
	}
}
