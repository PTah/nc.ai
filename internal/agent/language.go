package agent

import "unicode"

// Промпт просит отвечать на языке пользователя, но китайские модели (Qwen, GLM,
// DeepSeek) иногда всё равно уезжают в CJK. Тогда ответ переспрашиваем один раз.

const (
	// languageCJKMin — минимум иероглифов, чтобы считать ответ не на языке.
	languageCJKMin = 10
	// languageCJKRatio — доля CJK среди всех букв ответа.
	languageCJKRatio = 0.12
	// languageCyrMin — минимум кириллических букв в запросе пользователя.
	languageCyrMin = 4
)

// languageNudge — инструкция переписать финальный ответ на языке пользователя.
const languageNudge = "Rewrite the final answer in the user's language (the user writes Russian). " +
	"Do not use Chinese (CJK) characters anywhere, including the summary."

// letterCounts считает буквы: всего, кириллица, CJK (хань, кана, хангыль).
func letterCounts(s string) (letters, cyr, cjk int) {
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r):
			cjk++
		case r == 'ё' || r == 'Ё' || (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я'):
			cyr++
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters, cyr, cjk
}

// wrongLanguage сообщает, что ответ не на языке пользователя: запрос кириллицей,
// а в ответе заметная доля иероглифов.
func wrongLanguage(userText, answer string) bool {
	userLetters, userCyr, userCJK := letterCounts(userText)
	if userCJK > 0 || userLetters < languageCyrMin || userCyr*2 < userLetters {
		return false
	}
	letters, _, cjk := letterCounts(answer)
	if letters == 0 || cjk < languageCJKMin {
		return false
	}
	return float64(cjk)/float64(letters) >= languageCJKRatio
}
