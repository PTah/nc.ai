// Created by nc.ai 1.1.18 on new-admin-pc 2026-11-12 10:30:39
// Command indexprobe измеряет, сколько данных модель получает при поиске символа
// двумя путями: индекс проекта (find_symbol) против git grep + чтение файла.
//
//	go run ./cmd/indexprobe -root . -n 20
//
// Меряются только вызовы инструментов и байты, попадающие в контекст (прокси
// токенов), — без обращений к LLM, поэтому замер воспроизводим.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"notcursor.ai/app/internal/projindex"
)

// Бюджеты инструментов из internal/tools/budget.go: больше модель всё равно не
// увидит — grep режется по 32 КБ, read_file без диапазона отдаёт голову.
const (
	grepBudgetBytes = 32 * 1024
	readHeadLines   = 500
	readHeadBytes   = 32 * 1024
)

type sample struct {
	name       string
	indexBytes int
	indexCalls int
	grepBytes  int
	grepCalls  int
	grepFiles  int
}

func main() {
	root := flag.String("root", ".", "корень проекта")
	samples := flag.Int("n", 20, "сколько символов замерять")
	out := flag.String("out", "", "куда записать markdown-отчёт")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fatal(err)
	}
	idx, err := projindex.Build(abs)
	if err != nil {
		fatal(err)
	}
	names := pickSymbols(idx, *samples)
	if len(names) == 0 {
		fatal(fmt.Errorf("в индексе нет символов"))
	}

	var rows []sample
	for _, name := range names {
		s := sample{name: name}
		// Путь 1: индекс — один вызов find_symbol.
		hits := idx.FindSymbol(name, 5)
		s.indexCalls = 1
		s.indexBytes = len(projindex.FormatHits(hits))
		// Путь 2: grep по репо, затем чтение головы найденного файла.
		grepOut, files, err := gitGrep(abs, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "grep %s: %v\n", name, err)
			continue
		}
		s.grepCalls = 2
		s.grepFiles = files
		s.grepBytes = len(grepOut)
		if len(hits) > 0 {
			s.grepBytes += headBytes(filepath.Join(abs, filepath.FromSlash(hits[0].File)))
		} else if files > 0 {
			s.grepBytes += readHeadBytes
		}
		rows = append(rows, s)
	}

	report := render(rows)
	fmt.Print(report)
	if *out != "" {
		if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("\nзаписано в %s\n", *out)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "indexprobe:", err)
	os.Exit(1)
}

// pickSymbols берёт имена деклараций, которые встречаются в индексе ровно один
// раз (их и ищет модель), и разносит выборку по всему проекту.
func pickSymbols(idx *projindex.Index, n int) []string {
	count := map[string]int{}
	var all []string
	for _, f := range idx.Files {
		for _, sym := range f.Syms {
			if len(sym.Name) < 6 || strings.HasPrefix(sym.Name, "Test") {
				continue
			}
			count[sym.Name]++
			all = append(all, sym.Name)
		}
	}
	sort.Strings(all)
	var uniq []string
	seen := map[string]struct{}{}
	for _, name := range all {
		if count[name] != 1 {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		uniq = append(uniq, name)
	}
	if len(uniq) <= n {
		return uniq
	}
	step := len(uniq) / n
	out := make([]string, 0, n)
	for i := 0; i < len(uniq) && len(out) < n; i += step {
		out = append(out, uniq[i])
	}
	return out
}

func gitGrep(root, name string) ([]byte, int, error) {
	cmd := exec.Command("git", "-c", "color.ui=never", "grep", "-n", "--fixed-strings", "--no-color", "-e", name)
	cmd.Dir = root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &bytes.Buffer{}
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, 0, nil // совпадений нет
		}
		return nil, 0, err
	}
	out := buf.Bytes()
	files := map[string]struct{}{}
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, ":"); i > 0 {
			files[line[:i]] = struct{}{}
		}
	}
	if len(out) > grepBudgetBytes {
		out = out[:grepBudgetBytes]
	}
	return out, len(files), nil
}

// headBytes — сколько байт уйдёт модели при чтении файла без диапазона.
func headBytes(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	lines := bytes.SplitN(data, []byte("\n"), readHeadLines+1)
	head := bytes.Join(lines, []byte("\n"))
	if len(head) > readHeadBytes {
		return readHeadBytes
	}
	return len(head)
}

func render(rows []sample) string {
	if len(rows) == 0 {
		return "нет данных"
	}
	var sb strings.Builder
	sb.WriteString("| Символ | Индекс: вызовов/байт | grep+read: вызовов/байт | Файлов у grep |\n")
	sb.WriteString("|---|---|---|---|\n")
	var sumIndex, sumGrep, sumCallsIndex, sumCallsGrep int
	for _, r := range rows {
		fmt.Fprintf(&sb, "| `%s` | %d / %d | %d / %d | %d |\n",
			r.name, r.indexCalls, r.indexBytes, r.grepCalls, r.grepBytes, r.grepFiles)
		sumIndex += r.indexBytes
		sumGrep += r.grepBytes
		sumCallsIndex += r.indexCalls
		sumCallsGrep += r.grepCalls
	}
	n := len(rows)
	fmt.Fprintf(&sb, "\nЗадач: %d\n", n)
	fmt.Fprintf(&sb, "Индекс:  %d вызовов, %d байт (в среднем %.0f на задачу)\n",
		sumCallsIndex, sumIndex, float64(sumIndex)/float64(n))
	fmt.Fprintf(&sb, "grep:    %d вызовов, %d байт (в среднем %.0f на задачу)\n",
		sumCallsGrep, sumGrep, float64(sumGrep)/float64(n))
	if sumGrep > 0 {
		fmt.Fprintf(&sb, "Экономия контекста: %.1fx\n", float64(sumGrep)/float64(sumIndex))
	}
	return sb.String()
}
