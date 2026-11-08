// Created by nc.ai 1.1.10 on macbook 2026-11-08 21:14:14
package gitx

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"
)

// newProgressScanner — сканер с тем же сплиттером, что в cloneOne.
func newProgressScanner(s string) *bufio.Scanner {
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Split(splitProgressLines)
	return sc
}

// git clone --progress печатает проценты в stderr через \r. Разбираем ровно те
// строки, которые он выдаёт, иначе в диалоге клонирования снова будет «Клонирую…».
func TestParseGitProgress(t *testing.T) {
	cases := []struct {
		line    string
		wantPct int
		wantOK  bool
		label   string
	}{
		{"Receiving objects:  45% (1234/2743), 3.20 MiB | 1.10 MiB/s", 45, true, "получение объектов"},
		{"remote: Enumerating objects: 100% (1200/1200), done.", 100, true, "перечисление объектов"},
		{"remote: Compressing objects:  12% (12/100)", 12, true, "сжатие объектов"},
		{"Resolving deltas:  10% (100/1000)", 10, true, "разрешение дельт"},
		{"Counting objects:   7% (7/100)", 7, true, "подсчёт объектов"},
		{"старое\rReceiving objects:  50% (500/1000)", 50, true, "получение объектов"},
		{"remote: Total 1200 (delta 300), reused 900 (delta 200)", 0, false, ""},
		{"fatal: could not read Username for 'https://github.com'", 0, false, ""},
		{"", 0, false, ""},
	}
	for _, c := range cases {
		p, ok := parseGitProgress(c.line)
		if ok != c.wantOK {
			t.Errorf("parseGitProgress(%q) ok=%v, ожидали %v", c.line, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if p.Percent != c.wantPct {
			t.Errorf("parseGitProgress(%q) percent=%d, ожидали %d", c.line, p.Percent, c.wantPct)
		}
		if p.Phase != "git" {
			t.Errorf("parseGitProgress(%q) phase=%q, ожидали git", c.line, p.Phase)
		}
		if !strings.Contains(p.Detail, c.label) {
			t.Errorf("parseGitProgress(%q) detail=%q, ожидали упоминание %q", c.line, p.Detail, c.label)
		}
	}
}

// Прогресс приходит с \r, а не с \n: без собственного сплиттера весь поток
// склеился бы в одну строку и процент никогда не обновился.
func TestSplitProgressLines(t *testing.T) {
	input := "Counting objects:  10% (1/10)\rReceiving objects:  20% (2/10)\rReceiving objects:  30% (3/10)\nfinal"
	sc := newProgressScanner(input)
	var got []string
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	want := []string{
		"Counting objects:  10% (1/10)",
		"Receiving objects:  20% (2/10)",
		"Receiving objects:  30% (3/10)",
		"final",
	}
	if len(got) != len(want) {
		t.Fatalf("получили %d строк (%q), ожидали %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("строка %d = %q, ожидали %q", i, got[i], want[i])
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:                    "512 Б",
		2048:                   "2.0 КБ",
		3 * 1024 * 1024:        "3.0 МБ",
		21 * 1024 * 1024:       "21 МБ",
		5 * 1024 * 1024 * 1024: "5.0 ГБ",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, ожидали %q", in, got, want)
		}
	}
}

// progressWriter считает байты и отдаёт их в колбэк (с троттлингом по времени).
func TestProgressWriterReportsBytes(t *testing.T) {
	var buf bytes.Buffer
	var lastWritten, lastTotal int64
	pw := &progressWriter{
		w:     &buf,
		total: 1000,
		report: func(written, total int64) {
			lastWritten, lastTotal = written, total
		},
	}
	if _, err := pw.Write(make([]byte, 400)); err != nil {
		t.Fatal(err)
	}
	if lastWritten != 400 || lastTotal != 1000 {
		t.Fatalf("после первого Write: written=%d total=%d, ожидали 400/1000", lastWritten, lastTotal)
	}
	time.Sleep(130 * time.Millisecond) // пережидаем троттлинг
	if _, err := pw.Write(make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if lastWritten != 500 {
		t.Fatalf("после второго Write: written=%d, ожидали 500", lastWritten)
	}
	if buf.Len() != 500 {
		t.Fatalf("в приёмник записано %d байт, ожидали 500", buf.Len())
	}
}
