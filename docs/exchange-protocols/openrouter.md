# OpenRouter — протокол обмена

**Docs:** https://openrouter.ai/docs/api-reference/overview  
**Формат:** OpenAI Chat Completions + расширения маршрутизации.

---

## Endpoint

```
POST https://openrouter.ai/api/v1/chat/completions
Authorization: Bearer <OPENROUTER_API_KEY>
Content-Type: application/json
HTTP-Referer: https://notcursor.ai   # рекомендуется
X-Title: NotCursor.ai                # рекомендуется
```

---

## Особенности

1. `model` — строка вида `vendor/model` (например `deepseek/deepseek-chat`, `anthropic/claude-sonnet-4`).
2. Можно передать fallback-модели / provider preferences.
3. Streaming и tools — в целом как у OpenAI.
4. Один API key даёт доступ к множеству моделей разных вендоров.

---

## Пример

```json
{
  "model": "deepseek/deepseek-chat-v3",
  "messages": [
    { "role": "user", "content": "Explain this diff" }
  ],
  "stream": true,
  "tools": [],
  "provider": {
    "order": ["DeepSeek", "Together"],
    "allow_fallbacks": true
  }
}
```

> Точные имена моделей и поле `provider` сверять с актуальной OpenRouter docs/models list на момент реализации.

---

## Ошибки и биллинг

- 402 / credit errors — закончился баланс OpenRouter
- Модель может быть временно недоступна → использовать fallbacks
- Usage в ответе может включать native + OpenRouter fees

---

## Реализация в NotCursor

Этап 2+: `internal/llm/providers/openrouter`.  
В UI — выбор модели из списка OpenRouter, ключ в secrets.
