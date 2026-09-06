# OpenAI Chat Completions — эталонный протокол

**Docs:** https://platform.openai.com/docs/api-reference/chat  
**Роль в NotCursor:** канонический внутренний формат + будущий прямой провайдер.

---

## Endpoint

```
POST https://api.openai.com/v1/chat/completions
Authorization: Bearer <OPENAI_API_KEY>
Content-Type: application/json
```

---

## Базовый payload

```json
{
  "model": "gpt-4.1",
  "messages": [
    { "role": "system", "content": "You are a coding agent." },
    { "role": "user", "content": "Fix the bug in main.go" }
  ],
  "stream": true,
  "tools": [],
  "tool_choice": "auto",
  "temperature": 0.2
}
```

---

## Messages

| role | Назначение |
|---|---|
| `system` | Инструкции агента |
| `user` | Сообщение пользователя / контекст |
| `assistant` | Ответ модели (`content` и/или `tool_calls`) |
| `tool` | Результат исполнения tool (`tool_call_id` + `content`) |

`content` может быть строкой или массивом content parts (`text`, `image_url`, `input_audio`, и т.д. — по актуальной схеме модели).

---

## Tools (function calling)

```json
{
  "type": "function",
  "function": {
    "name": "read_file",
    "description": "Read a file from the workspace",
    "parameters": {
      "type": "object",
      "properties": {
        "path": { "type": "string" }
      },
      "required": ["path"]
    }
  }
}
```

Ответ модели:

```json
{
  "role": "assistant",
  "content": null,
  "tool_calls": [
    {
      "id": "call_1",
      "type": "function",
      "function": {
        "name": "read_file",
        "arguments": "{\"path\":\"main.go\"}"
      }
    }
  ]
}
```

Обратная связь:

```json
{
  "role": "tool",
  "tool_call_id": "call_1",
  "content": "package main\n..."
}
```

---

## Streaming

`stream: true` → Server-Sent Events:

```
data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hi"},"index":0,"finish_reason":null}]}

data: [DONE]
```

Опционально:

```json
"stream_options": { "include_usage": true }
```

---

## finish_reason

- `stop` — готово
- `length` — упёрлись в max tokens
- `tool_calls` — нужно исполнить tools
- `content_filter` — фильтр контента

---

## Маппинг на внутренний канон NotCursor

Прямой 1:1. Структура `internal/llm.ChatRequest` / `ChatResponse` копирует OpenAI shapes.

Поля провайдер-специфичные (DeepSeek `thinking`, OpenRouter `provider`) кладутся в `Extra map[string]any` и сериализуются только нужным адаптером.
