# Z.ai / BigModel (GLM) — протокол обмена

**Docs:** https://docs.bigmodel.cn/  
**Формат:** близкий к OpenAI Chat Completions.

---

## Endpoint (типичный)

```
POST https://open.bigmodel.cn/api/paas/v4/chat/completions
Authorization: Bearer <ZAI_API_KEY>
Content-Type: application/json
```

> Точный base path и имена моделей сверять с актуальной документацией BigModel/Z.ai на момент кодинга (возможны региональные зеркала).

---

## Пример

```json
{
  "model": "glm-4.5",
  "messages": [
    { "role": "system", "content": "You are a coding agent." },
    { "role": "user", "content": "Write a Go HTTP handler" }
  ],
  "stream": true,
  "temperature": 0.3
}
```

---

## Function calling

Большинство GLM chat endpoints поддерживают OpenAI-like `tools` / `tool_calls`.  
Перед включением в agent loop — прогнать smoke-test конкретной модели:

1. tools объявлены
2. модель возвращает `tool_calls`
3. принимает `role: tool`

Если модель не умеет tools — использовать только chat mode без агента.

---

## Реализация

Этап 3: `internal/llm/providers/zai`.
