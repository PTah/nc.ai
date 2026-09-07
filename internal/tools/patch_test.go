package tools

import "testing"

func TestApplySearchReplace_Once(t *testing.T) {
	got, n, err := ApplySearchReplace("aaa\nbbb\nccc\n", "bbb", "BBB", false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || got != "aaa\nBBB\nccc\n" {
		t.Fatalf("got %q n=%d", got, n)
	}
}

func TestApplySearchReplace_Ambiguous(t *testing.T) {
	_, _, err := ApplySearchReplace("x y x", "x", "z", false)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
}

func TestApplySearchReplace_ReplaceAll(t *testing.T) {
	got, n, err := ApplySearchReplace("x y x", "x", "z", true)
	if err != nil || n != 2 || got != "z y z" {
		t.Fatalf("got %q n=%d err=%v", got, n, err)
	}
}

func TestParseSearchReplacePatch(t *testing.T) {
	patch := "<<<<<<< SEARCH\nold line\n=======\nnew line\n>>>>>>> REPLACE"
	old, neu, err := ParseSearchReplacePatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	if old != "old line" || neu != "new line" {
		t.Fatalf("old=%q new=%q", old, neu)
	}
}
