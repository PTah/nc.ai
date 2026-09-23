package agent

import (
	"encoding/json"
	"fmt"

	"notcursor.ai/app/internal/llm"
)

// contextUnknownWarnTokens — порог предупреждения, когда размер контекста
// сервера неизвестен (у Ollama по умолчанию 4096).
const contextUnknownWarnTokens = 8192

// contextPayload — заполнение контекста для индикатора в UI.
func contextPayload(used, limit int) string {
	b, _ := json.Marshal(map[string]int{"used": used, "limit": limit})
	return string(b)
}

// contextFillNoticeUnknown — предупреждение, когда окно контекста не задано.
func contextFillNoticeUnknown(promptTokens int, kind string) string {
	return fmt.Sprintf(
		"Local: промпт ~%d токенов, а окно контекста в настройках не задано — неизвестно, режет ли сервер историю. Что делать: %s.",
		promptTokens, localRaiseContextAdvice(kind))
}

// Подсказки для локальных серверов (Ollama, LM Studio, llama.cpp). Вынесены
// отдельно, чтобы проверялись тестами без запуска полного прогона агента.

// localRaiseContextAdvice — как поднять окно контекста на конкретном сервере:
// у Ollama это переменная окружения, у Lemonade/LM Studio/vLLM — настройки
// самого сервера при загрузке модели.
func localRaiseContextAdvice(kind string) string {
	if kind == "openai" {
		return "окно задаётся на стороне сервера при загрузке модели (Lemonade, LM Studio, vLLM) — поднимите его там и укажите то же число в Settings → Local → Context"
	}
	return "поднимите контекст сервера (OLLAMA_CONTEXT_LENGTH) и укажите то же число в Settings → Local → Context"
}

// contextFillNotice — предупреждение, что промпт подошёл к окну контекста.
func contextFillNotice(promptTokens, numCtx int, kind string) string {
	return fmt.Sprintf(
		"Local: промпт ~%d токенов при контексте %d — сервер может обрезать историю (и отвечать пусто). Что делать: %s.",
		promptTokens, numCtx, localRaiseContextAdvice(kind))
}

// emptyAnswerNudge — что попросить у модели после пустого ответа.
func emptyAnswerNudge(numCtx int, sawThinking bool) string {
	msg := "Your previous answer came back empty. Reply with plain text (no tool calls) using the files you already read. Answer in the user's language."
	if sawThinking {
		msg = "All of your previous output went into the thinking block and the text answer is empty. Write the final answer as plain text now (no tool calls). Answer in the user's language."
	}
	if numCtx > 0 {
		msg += fmt.Sprintf(" The server context window is %d tokens; if the data does not fit, say exactly what is missing.", numCtx)
	}
	return msg
}

// emptyAnswerError — диагностика, если ответ так и остался пустым.
func emptyAnswerError(finish string, numCtx int, kind string, sawThinking bool, promptTokens int) string {
	msg := fmt.Sprintf("пустой финальный ответ (finish=%s)", finish)
	switch {
	case sawThinking:
		msg += " — модель ушла в размышления и не выдала текст (thinking-only)"
	case numCtx > 0 && promptTokens >= numCtx:
		msg += fmt.Sprintf(" — промпт ~%d токенов при контексте %d: сервер обрезал историю (%s)", promptTokens, numCtx, localRaiseContextAdvice(kind))
	case promptTokens > 0:
		msg += fmt.Sprintf(" — промпт ~%d токенов, проверьте окно контекста сервера", promptTokens)
	}
	return msg + ". Что делать: начать новый чат (история короче), поднять окно модели или взять модель поменьше."
}

// usagePromptTokens — сколько токенов занял промпт (0, если провайдер не сообщил).
func usagePromptTokens(u *llm.Usage) int {
	if u == nil || u.PromptTokens < 0 {
		return 0
	}
	return u.PromptTokens
}

// localModelNoticeText объясняет, почему шаг ушёл на другую локальную модель.
func localModelNoticeText(preferred, chosen, reason string) string {
	return fmt.Sprintf(
		"Local+Auto-models: выбранная в Settings модель %s не подошла для этого шага — взял %s (%s) и сохранил её активной. "+
			"Работать строго на %s — выключите Auto-models в Settings.",
		preferred, chosen, reason, preferred)
}
