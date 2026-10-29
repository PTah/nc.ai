// Created by nc.ai 0.9.4 on new-admin-pc 2026-10-30 09:05:19
package agent

import (
	"regexp"
	"testing"
)

func TestNewDataTagFormat(t *testing.T) {
	re := regexp.MustCompile(`^data-[0-9a-f]{8}$`)
	a := newDataTag()
	if !re.MatchString(a) {
		t.Fatalf("unexpected tag: %q", a)
	}
	if b := newDataTag(); a == b {
		t.Fatalf("tag must be random per call, got identical %q", a)
	}
}
