// Created by nc.ai 1.2.17 on Papasha 2026-11-21 08:40:00
package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// chatTitleMaxLen — сколько символов оставляем в названии чата. Cursor держит
// названия примерно такой длины: видно тему, но вкладка не расползается.
const chatTitleMaxLen = 56

// chatTitleMinClause — короче этого обрезать по запятой бессмысленно: получится
// «обнови» вместо «обнови локальные файлы».
const chatTitleMinClause = 16

// chatTitleMinSentence — совсем короткое «ок.» слева от точки не считаем фразой.
const chatTitleMinSentence = 8

var (
	titleURL      = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+`)
	titleSpace    = regexp.MustCompile(`\s+`)
	titleMarkdown = strings.NewReplacer(
		"**", "", "__", "", "`", "", "~~", "",
		"![", " ", "](", " ", "##", "", "# ", "",
	)
	titleBullets = regexp.MustCompile(`(?m)^\s*(?:[-*•>]+|\d+[.)])\s+`)
)

// chatTitleFillers — приветствия и частицы в начале вопроса: темы в них нет
// («ок, тогда работаем…» → «тогда работаем…»). Срезаем только когда после слова
// идёт разделитель, иначе «Chatter»/«также» потеряют начало.
var chatTitleFillers = []string{
	"привет", "приветик", "здравствуй", "здравствуйте", "здорово", "хай",
	"hi", "hello", "hey", "ок", "ok", "окей", "ладно", "please", "пожалуйста",
}

// ShortChatTitle превращает первый вопрос пользователя в короткое имя чата так
// же, как это делает Cursor: без кода, ссылок и простыни текста — одна фраза по
// теме, обрезанная по границе слова.
func ShortChatTitle(text string) string {
	s := strings.TrimSpace(text)
	if s == "" {
		return ""
	}
	// Код и ссылки о теме вопроса ничего не говорят.
	if i := strings.Index(s, "```"); i > 0 {
		s = s[:i]
	}
	s = titleURL.ReplaceAllString(s, " ")
	s = titleMarkdown.Replace(s)
	s = titleBullets.ReplaceAllString(s, " ")
	s = titleSpace.ReplaceAllString(s, " ")
	s = stripChatFillers(strings.TrimSpace(s))
	s = firstChatSentence(s)
	s = strings.TrimSpace(strings.Trim(s, " .,;:!?…\"'«»()[]{}"))
	if s == "" {
		return ""
	}
	if len([]rune(s)) > chatTitleMaxLen {
		// Слишком длинная фраза: пробуем первый смысловой кусок до запятой/тире,
		// иначе режем по границе слова.
		if head := titleClause(s); head != "" {
			s = head
		}
		if r := []rune(s); len(r) > chatTitleMaxLen {
			s = string(r[:chatTitleMaxLen])
			if i := strings.LastIndexAny(s, " \t"); i >= chatTitleMinClause {
				s = s[:i]
			}
			s = strings.TrimRight(s, " .,;:!?…-—–")
			s = dropChatTitleTail(s)
		}
	}
	return strings.TrimSpace(s)
}

// stripChatFillers срезает приветствия/частицы в начале («ок, тогда…»), пока
// они стоят отдельным словом. Само слово внутри фразы не трогаем.
func stripChatFillers(s string) string {
	for {
		trimmed := false
		lower := strings.ToLower(s)
		for _, f := range chatTitleFillers {
			if !strings.HasPrefix(lower, f) {
				continue
			}
			rest := s[len(f):]
			r := []rune(rest)
			if len(r) == 0 {
				return ""
			}
			if unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]) {
				continue // «Chatter», «около» — это уже не приветствие
			}
			next := strings.TrimSpace(strings.TrimLeft(rest, " \t,:;!.—–-"))
			if next == "" {
				return ""
			}
			s = next
			trimmed = true
			break
		}
		if !trimmed {
			return s
		}
	}
}

// firstChatSentence оставляет первую фразу: до точки/!/?/многоточия/перевода
// строки, если она заканчивается границей слова.
func firstChatSentence(s string) string {
	r := []rune(s)
	for i, c := range r {
		boundary := false
		switch c {
		case '\n':
			boundary = true
		case '.', '!', '?', '…':
			boundary = i == len(r)-1 || r[i+1] == ' ' || r[i+1] == '\t'
		}
		if boundary {
			if i >= chatTitleMinSentence {
				return string(r[:i])
			}
			return s
		}
	}
	return s
}

// titleClause берёт первый кусок длинной фразы до запятой, двоеточия или тире —
// «обнови локальные файлы, скажи что там да…» → «обнови локальные файлы».
func titleClause(s string) string {
	r := []rune(s)
	for i, c := range r {
		switch c {
		case ',', ':', ';', '—', '–':
			if i < chatTitleMinClause {
				continue
			}
			return strings.TrimSpace(string(r[:i]))
		case '-':
			if i >= chatTitleMinClause && (r[i-1] == ' ' || r[i+1] == ' ') {
				return strings.TrimSpace(string(r[:i]))
			}
		}
	}
	return ""
}

// chatTitleTailWords — служебные слова, на которых некрасиво обрывать имя:
// «…сравни с репозиторием чтобы» → «…сравни с репозиторием».
var chatTitleTailWords = map[string]bool{
	"и": true, "в": true, "во": true, "с": true, "со": true, "на": true,
	"по": true, "для": true, "что": true, "чтобы": true, "как": true,
	"или": true, "а": true, "но": true, "не": true, "из": true, "от": true,
	"the": true, "and": true, "to": true, "for": true, "of": true, "with": true,
}

// dropChatTitleTail убирает с конца обрезанного имени одиночное служебное слово.
func dropChatTitleTail(s string) string {
	for {
		i := strings.LastIndexAny(s, " ")
		if i <= 0 {
			return s
		}
		tail := strings.ToLower(strings.Trim(s[i+1:], " .,;:!?…-—–"))
		if !chatTitleTailWords[tail] {
			return s
		}
		s = strings.TrimRight(s[:i], " .,;:!?…-—–")
	}
}

// isDefaultChatTitle — имя, которое чат получил при создании («Chat», «Chat 7»,
// пусто). Только такое имя можно заменить автоматическим названием по вопросу;
// имя, выбранное человеком, не трогаем никогда.
func isDefaultChatTitle(title string) bool {
	t := strings.TrimSpace(title)
	if t == "" {
		return true
	}
	lower := strings.ToLower(t)
	if !strings.HasPrefix(lower, "chat") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(lower, "chat"))
	if rest == "" {
		return true
	}
	n, err := strconv.Atoi(rest)
	return err == nil && n > 0
}

// SuggestChatTitle — короткое имя чата по первому вопросу (для вкладки). Ничего
// не сохраняет: решает и сохраняет вызывающая сторона.
func (a *App) SuggestChatTitle(text string) string {
	return ShortChatTitle(text)
}

// autoTitleSession заменяет дефолтное имя чата коротким названием из первого
// вопроса. Ошибки здесь не критичны: имя — удобство, прогон из-за него не падает.
func (a *App) autoTitleSession(projectKey, sessionID, userMessage string) {
	if a == nil || a.chats == nil || projectKey == "" || sessionID == "" {
		return
	}
	title := ShortChatTitle(userMessage)
	if title == "" {
		return
	}
	sess, err := a.chats.Get(projectKey, sessionID)
	if err != nil || sess == nil || sess.ID != sessionID {
		return
	}
	if !isDefaultChatTitle(sess.Title) {
		return
	}
	sess.Title = title
	if err := a.chats.SaveSession(projectKey, sess); err != nil {
		a.logAgentLine(fmt.Sprintf("chat title: не сохранилось (%s): %v", sessionID, err))
	}
}
