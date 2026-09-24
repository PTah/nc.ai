package llm

import (
	"strings"
	"testing"
)

func TestSplitThink(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		body, think string
	}{
		{"без блока", "просто ответ", "просто ответ", ""},
		{"ведущий блок", "<think>думаю</think>Ответ", "Ответ", "думаю"},
		{"переносы и пробелы", "\n<think>a\nb</think>\n\nОтвет", "Ответ", "a\nb"},
		{"два блока", "<think>a</think><thinking>b</thinking>Ответ", "Ответ", "ab"},
		{"незакрытый блок", "<think>обрезали", "", "обрезали"},
		{"reasoning", "<reasoning>a</reasoning>Ответ", "Ответ", "a"},
		{"тег в середине не трогаем", "Ответ с кодом <think>x</think> внутри", "Ответ с кодом <think>x</think> внутри", ""},
		{"только блок", "<think>a</think>", "", "a"},
	}
	for _, c := range cases {
		body, think := SplitThink(c.in)
		if body != c.body || think != c.think {
			t.Errorf("%s: SplitThink(%q) = (%q, %q), want (%q, %q)", c.name, c.in, body, think, c.body, c.think)
		}
	}
}

func TestSplitThinkMessage(t *testing.T) {
	m := Message{Role: "assistant", Content: "<think>рассуждаю</think>\n\nОтвет"}
	SplitThinkMessage(&m)
	if m.Content != "Ответ" || m.ReasoningContent != "рассуждаю" {
		t.Fatalf("content=%q reasoning=%q", m.Content, m.ReasoningContent)
	}
	// Провайдер уже отдал reasoning отдельным полем — не перетираем.
	m = Message{Role: "assistant", Content: "<think>a</think>Ответ", ReasoningContent: "своё"}
	SplitThinkMessage(&m)
	if m.ReasoningContent != "своё" || m.Content != "Ответ" {
		t.Fatalf("content=%q reasoning=%q", m.Content, m.ReasoningContent)
	}
}

// splitAll прогоняет дельты через сплиттер и склеивает результат.
func splitAll(chunks []string) (body, think string) {
	var s ThinkSplitter
	var b, th strings.Builder
	for _, c := range chunks {
		pb, pt := s.Push(c)
		b.WriteString(pb)
		th.WriteString(pt)
	}
	fb, ft := s.Flush()
	b.WriteString(fb)
	th.WriteString(ft)
	return b.String(), th.String()
}

func TestThinkSplitterChunks(t *testing.T) {
	cases := []struct {
		name        string
		chunks      []string
		body, think string
	}{
		{
			name:   "теги разрезаны между дельтами",
			chunks: []string{"<thi", "nk>рассу", "ждаю</thi", "nk>\nОтв", "ет"},
			body:   "Ответ", think: "рассуждаю",
		},
		{
			name:   "обычный ответ без блоков",
			chunks: []string{"Привет, ", "мир!"},
			body:   "Привет, мир!", think: "",
		},
		{
			name:   "литеральный тег в середине ответа",
			chunks: []string{"Привет, <", "think>", " — но это не тег"},
			body:   "Привет, <think> — но это не тег", think: "",
		},
		{
			name:   "reasoning_content идёт мимо сплиттера",
			chunks: []string{"<think>a</think>", "Ответ"},
			body:   "Ответ", think: "a",
		},
	}
	for _, c := range cases {
		body, think := splitAll(c.chunks)
		if body != c.body || think != c.think {
			t.Errorf("%s: body=%q think=%q, want %q / %q", c.name, body, think, c.body, c.think)
		}
	}
}

func TestThinkSplitterFlush(t *testing.T) {
	// Поток оборвался на неполном теге: отдаём его текстом, а не reasoning.
	body, think := splitAll([]string{"<thi"})
	if body != "<thi" || think != "" {
		t.Fatalf("body=%q think=%q", body, think)
	}
	// Поток оборвался внутри thinking — это reasoning (как и у SplitThink).
	body, think = splitAll([]string{"<think>думаю до конца"})
	if body != "" || think != "думаю до конца" {
		t.Fatalf("body=%q think=%q", body, think)
	}
}
