package llm

import "strings"

// Часть OpenAI-совместимых эндпоинтов (агрегаторы из бесплатных тарифов,
// Muse Glimmer, MiniMax M3, Qwen3 под vLLM) отдаёт «размышления» не отдельным
// полем reasoning_content, а инлайном в content: <think>…</think>ответ.
// Если это не развести, thinking попадает в текст ответа, в проверку языка и в
// историю чата. Ниже — разбор таких блоков: одним куском (SplitThink) и в
// потоке дельт (ThinkSplitter).

type thinkTag struct{ open, close string }

var thinkTags = []thinkTag{
	{"<think>", "</think>"},
	{"<thinking>", "</thinking>"},
	{"<reasoning>", "</reasoning>"},
}

// matchThinkOpen распознаёт открывающий тег в начале строки.
func matchThinkOpen(s string) (openLen int, closeTag string, ok bool) {
	for _, t := range thinkTags {
		if strings.HasPrefix(s, t.open) {
			return len(t.open), t.close, true
		}
	}
	return 0, "", false
}

// prefixOfThinkOpen — s может оказаться началом открывающего тега («<», «<thi»),
// значит решение откладываем: тег может прийти следующей дельтой.
func prefixOfThinkOpen(s string) bool {
	if s == "" {
		return true
	}
	for _, t := range thinkTags {
		if len(s) < len(t.open) && strings.HasPrefix(t.open, s) {
			return true
		}
	}
	return false
}

// SplitThink отделяет ведущие thinking-блоки от ответа. Если блоков нет,
// content возвращается без изменений (незакрытый блок — модель обрезали —
// считается reasoning'ом целиком).
func SplitThink(content string) (body, think string) {
	rest := content
	var th strings.Builder
	found := false
	for {
		trimmed := strings.TrimLeft(rest, " \t\r\n")
		openLen, closeTag, ok := matchThinkOpen(trimmed)
		if !ok {
			break
		}
		after := trimmed[openLen:]
		if idx := strings.Index(after, closeTag); idx >= 0 {
			th.WriteString(after[:idx])
			rest = after[idx+len(closeTag):]
		} else {
			th.WriteString(after)
			rest = ""
		}
		found = true
	}
	if !found {
		return content, ""
	}
	return strings.TrimLeft(rest, " \t\r\n"), th.String()
}

// SplitThinkMessage переносит инлайновый thinking из content в reasoning.
func SplitThinkMessage(m *Message) {
	if m == nil {
		return
	}
	body, think := SplitThink(m.Content)
	if think == "" {
		return
	}
	m.Content = body
	if strings.TrimSpace(m.ReasoningContent) == "" {
		m.ReasoningContent = think
	}
}

const (
	thinkProbe = iota // ищем открывающий тег в начале ответа
	thinkIn           // внутри thinking-блока
	thinkBody         // обычный текст ответа
)

// ThinkSplitter — SplitThink для потока: часть тега может прийти в следующей
// дельте, поэтому до решения держим небольшой буфер. После первого куска
// обычного текста теги больше не ищем — код или пример с <think> внутри
// ответа не пострадает.
type ThinkSplitter struct {
	state    int
	probe    string // накопленный префикс до решения
	tail     string // хвост thinking'а (в нём ищем закрывающий тег)
	closeTag string
}

// Push обрабатывает очередную дельту, возвращая текст ответа и thinking.
func (s *ThinkSplitter) Push(chunk string) (body, think string) {
	if chunk == "" {
		return "", ""
	}
	switch s.state {
	case thinkBody:
		return chunk, ""
	case thinkIn:
		s.tail += chunk
		return s.drainThink()
	}
	s.probe += chunk
	trimmed := strings.TrimLeft(s.probe, " \t\r\n")
	if openLen, closeTag, ok := matchThinkOpen(trimmed); ok {
		s.state = thinkIn
		s.closeTag = closeTag
		s.tail = trimmed[openLen:]
		s.probe = ""
		return s.drainThink()
	}
	if prefixOfThinkOpen(trimmed) {
		return "", ""
	}
	s.state = thinkBody
	out := s.probe
	s.probe = ""
	return out, ""
}

// Flush закрывает поток: незавершённый thinking-блок уходит в reasoning,
// нераспознанный префикс — обратно в текст ответа.
func (s *ThinkSplitter) Flush() (body, think string) {
	switch s.state {
	case thinkIn:
		think, s.tail, s.state = s.tail, "", thinkBody
		return "", think
	case thinkProbe:
		body, s.probe, s.state = s.probe, "", thinkBody
		return body, ""
	}
	return "", ""
}

func (s *ThinkSplitter) drainThink() (body, think string) {
	if idx := strings.Index(s.tail, s.closeTag); idx >= 0 {
		think = s.tail[:idx]
		rest := s.tail[idx+len(s.closeTag):]
		s.tail, s.closeTag, s.state = "", "", thinkBody
		return strings.TrimLeft(rest, " \t\r\n"), think
	}
	// Держим хвост, в котором может начинаться закрывающий тег. Длину
	// считаем в рунах: байтовый срез разрезал бы многобайтовый символ.
	keep := len(s.closeTag) - 1
	if keep < 0 {
		keep = 0
	}
	runes := []rune(s.tail)
	if len(runes) > keep {
		cut := len(runes) - keep
		think = string(runes[:cut])
		s.tail = string(runes[cut:])
	}
	return "", think
}
