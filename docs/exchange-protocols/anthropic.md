# Anthropic Messages API — протокол и маппинг

**Docs:** https://docs.anthropic.com/en/api/messages  
**Важно:** это **не** OpenAI wire-format. NotCursor обязан конвертировать.

---

## Endpoint

```
POST https://api.anthropic.com/v1/messages
x-api-key: <ANTHROPIC_API_KEY>
anthropic-version: 2023-06-01
Content-Type: application/json
```

---

## Ключевые отличия от OpenAI

| Тема | OpenAI | Anthropic |
|---|---|---|
| System prompt | `messages[{role:system}]` | отдельное поле `system` |
| Roles в messages | system/user/assistant/tool | в основном `user` / `assistant` |
| Tools definition | `tools[].function.{name,parameters}` | `tools[].{name,input_schema}` |
| Tool call в ответе | `message.tool_calls[]` | content block `type: tool_use` |
| Tool result | `role: tool` | user content block `type: tool_result` |
| Max tokens | опционально | **`max_tokens` обязателен** |
| Streaming | SSE chat.completion.chunk | SSE events: `message_start`, `content_block_delta`, … |

---

## Пример запроса

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 4096,
  "system": "You are NotCursor coding agent.",
  "messages": [
    { "role": "user", "content": "Refactor auth.go" }
  ],
  "tools": [
    {
      "name": "read_file",
      "description": "Read a workspace file",
      "input_schema": {
        "type": "object",
        "properties": {
          "path": { "type": "string" }
        },
        "required": ["path"]
      }
    }
  ]
}
```

---

## Tool use / tool result

Ответ assistant (упрощённо):

```json
{
  "role": "assistant",
  "content": [
    {
      "type": "tool_use",
      "id": "toolu_01...",
      "name": "read_file",
      "input": { "path": "auth.go" }
    }
  ],
  "stop_reason": "tool_use"
}
```

Следующий user turn:

```json
{
  "role": "user",
  "content": [
    {
      "type": "tool_result",
      "tool_use_id": "toolu_01...",
      "content": "package auth\n..."
    }
  ]
}
```

---

## Маппинг OpenAI-канон → Anthropic

Выполняет `internal/llm/providers/anthropic/adapter.go`:

1. Вырезать `system` messages → поле `system`.
2. Склеить подряд идущие одинаковые roles при необходимости.
3. `tools[].function.parameters` → `tools[].input_schema`.
4. `assistant.tool_calls` → content blocks `tool_use` (`arguments` JSON-string → `input` object).
5. `role:tool` messages → `user` + `tool_result` blocks (можно группировать).
6. `finish_reason=tool_calls` ↔ `stop_reason=tool_use`.
7. Stream: транслировать `text_delta` / `input_json_delta` в канонические `StreamEvent`.

Обратный маппинг Anthropic → канон — зеркально, чтобы agent loop оставался единым.

---

## Streaming (кратко)

События:

- `message_start`
- `content_block_start`
- `content_block_delta` (`text_delta`, `input_json_delta`, …)
- `content_block_stop`
- `message_delta` (stop_reason, usage)
- `message_stop`

Парсер обязан собирать tool JSON по частям.

---

## Этап внедрения

Этап 3. До этого Claude доступен через OpenRouter (OpenAI-shaped), без прямого Anthropic адаптера.
