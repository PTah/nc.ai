# DeepSeek API — протокол обмена (этап 1)

**Официальные docs:** https://api-docs.deepseek.com/  
**Совместимость:** OpenAI Chat Completions (+ расширения thinking / cache).  
**Статус в NotCursor:** первый подключенный провайдер.

---

## 1. Базовые адреса

| Параметр | Значение |
|---|---|
| Base URL (OpenAI-формат) | `https://api.deepseek.com` |
| Base URL alias | `https://api.deepseek.com/v1` (`/v1` — alias, не версия модели) |
| Chat endpoint | `POST /chat/completions` |
| Anthropic-совместимый base | `https://api.deepseek.com/anthropic` |
| Beta (FIM / prefix) | `https://api.deepseek.com/beta` |

Полный URL этапа 1:

```
POST https://api.deepseek.com/chat/completions
```

---

## 2. Аутентификация

```http
Authorization: Bearer <DEEPSEEK_API_KEY>
Content-Type: application/json
```

Ключ хранится только в локальном secrets store NotCursor. В логи никогда не писать.

---

## 3. Модели (актуально на 2026-09)

| Model ID | Назначение |
|---|---|
| `deepseek-v4-flash` | Быстрый / дешёвый кодинг |
| `deepseek-v4-pro` | Максимальное качество |
| `deepseek-v4-flash-vision-exp` | Экспериментальный vision |

> Устаревшие алиасы вроде `deepseek-chat` / `deepseek-reasoner` не использовать в новом коде.

Default в NotCursor (этап 1): `deepseek-v4-flash` (скорость) с возможностью выбрать `deepseek-v4-pro`.

---

## 4. Запрос (non-stream)

```json
{
  "model": "deepseek-v4-pro",
  "messages": [
    {
      "role": "system",
      "content": "Ты ИИ-агент NotCursor. Используй tools для работы с файлами, git и shell."
    },
    {
      "role": "user",
      "content": "Добавь валидацию email в index.js"
    }
  ],
  "stream": false,
  "thinking": { "type": "enabled" },
  "reasoning_effort": "high",
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "write_file",
        "description": "Записывает или перезаписывает локальный файл",
        "parameters": {
          "type": "object",
          "properties": {
            "path": { "type": "string", "description": "Путь относительно workspace" },
            "content": { "type": "string", "description": "Полное содержимое файла" }
          },
          "required": ["path", "content"]
        }
      }
    }
  ],
  "tool_choice": "auto"
}
```

### Поля, важные для агента

| Поле | Тип | Описание |
|---|---|---|
| `messages` | array | Роли: `system`, `user`, `assistant`, `tool` |
| `model` | string | ID модели |
| `stream` | bool | SSE-стриминг |
| `tools` | array | Function calling (до 128 функций) |
| `tool_choice` | `none` / `auto` / `required` / named | Политика вызова tools |
| `thinking` | `{ "type": "enabled"\|"disabled" }` | Режим рассуждения |
| `reasoning_effort` | `low` / `high` / `max` | Глубина thinking |
| `max_tokens` | int | Лимит выходных токенов |
| `temperature` | number 0..2 | Сэмплинг |
| `top_p` | number 0..1 | Nucleus sampling |
| `response_format` | `{ "type": "text"\|"json_object" }` | JSON mode |
| `user` / `user_id` | string | Изоляция KV-cache / safety |
| `stream_options.include_usage` | bool | Usage в последнем SSE-чанке |

`frequency_penalty` / `presence_penalty` — deprecated, не отправлять.

---

## 5. Роли сообщений

### system

```json
{ "role": "system", "content": "..." }
```

### user

`content` — string или массив parts (text / image / file для vision-моделей).

### assistant

Может содержать:

- `content` — текст ответа (nullable)
- `reasoning_content` — рассуждения (thinking mode)
- `tool_calls` — массив вызовов функций

```json
{
  "role": "assistant",
  "content": null,
  "tool_calls": [
    {
      "id": "call_abc123",
      "type": "function",
      "function": {
        "name": "write_file",
        "arguments": "{\"path\":\"index.js\",\"content\":\"...\"}"
      }
    }
  ]
}
```

> `arguments` — **строка JSON**, не объект. Перед исполнением парсить и валидировать.

### tool

```json
{
  "role": "tool",
  "tool_call_id": "call_abc123",
  "content": "OK: wrote 128 bytes to index.js"
}
```

---

## 6. Ответ (non-stream)

```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "created": 1725600000,
  "model": "deepseek-v4-pro",
  "system_fingerprint": "...",
  "choices": [
    {
      "index": 0,
      "finish_reason": "tool_calls",
      "message": {
        "role": "assistant",
        "content": null,
        "reasoning_content": "...",
        "tool_calls": [
          {
            "id": "call_abc123",
            "type": "function",
            "function": {
              "name": "write_file",
              "arguments": "{\"path\":\"index.js\",\"content\":\"console.log(1)\"}"
            }
          }
        ]
      }
    }
  ],
  "usage": {
    "prompt_tokens": 120,
    "completion_tokens": 40,
    "total_tokens": 160,
    "prompt_cache_hit_tokens": 80,
    "prompt_cache_miss_tokens": 40,
    "completion_tokens_details": {
      "reasoning_tokens": 12
    }
  }
}
```

### `finish_reason`

| Значение | Действие NotCursor |
|---|---|
| `tool_calls` | Исполнить **все** `message.tool_calls`, дописать `role=tool`, повторить запрос |
| `stop` | Если `tool_calls` **не пустой** — то же, что `tool_calls`. Если пустой — финальный текст, выход из loop |
| `length` | Ответ обрезан; если есть `tool_calls` — исполнить, иначе показать частичный текст + предупреждение |
| `content_filter` | Ошибка безопасности, loop стоп |
| `insufficient_system_resource` | Ошибка инфраструктуры, loop стоп |

**Канон решения (как в официальном Tool Calls / Thinking sample):**

```
messages.append(assistant_message)          // целиком: content + reasoning_content + tool_calls
if assistant_message.tool_calls is empty:
    stop  # финальный ответ
else:
    execute tools → append role=tool → continue
```

`finish_reason` **нельзя** использовать как «выходим из loop», если `tool_calls` уже есть. DeepSeek часто отдаёт промежуточный текст (`content`) вместе с вызовом tool.

---

## 7. Streaming (SSE)

Запрос: `"stream": true`.

Формат чанков — OpenAI-совместимый:

```
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: [DONE]
```

Для tool calls дельты приходят в `delta.tool_calls[]` с инкрементальным `function.arguments`.

Правила парсера NotCursor:

1. Читать SSE line-by-line (`data: ...`).
2. Игнорировать keep-alive / пустые строки.
3. На `data: [DONE]` закрывать stream.
4. Аккумулировать `tool_calls` по `index`.
5. Usage брать из последнего чанка (если `stream_options.include_usage=true`).

---

## 8. Prompt caching

DeepSeek возвращает:

- `prompt_cache_hit_tokens`
- `prompt_cache_miss_tokens`

Для агента выгодно держать стабильный длинный `system` + описания tools в начале контекста (префикс кэшируется).

---

## 9. Thinking mode

Thinking **включён по умолчанию** на `deepseek-v4-flash` / `deepseek-v4-pro`. Default effort — `high`.

```json
"thinking": { "type": "enabled" },
"reasoning_effort": "high"
```

В ответе:

- `message.reasoning_content` — chain-of-thought (nullable)
- `message.content` — ответ пользователю (на промежуточных tool-ходах часто `""` / `null` — это норма)
- UI показывает reasoning отдельным сворачиваемым блоком

В thinking mode параметры `temperature` / `top_p` / `presence_penalty` / `frequency_penalty` **игнорируются** (ошибки нет).

### Когда слать `reasoning_content` обратно

Источник: [Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode/) и [Tool Calls](https://api-docs.deepseek.com/guides/tool_calls).

| Следующий запрос содержит `tools`? | `reasoning_content` прошлых assistant-ходов |
|---|---|
| **Да** (agent loop Always) | **Обязательно** вернуть полностью. Иначе HTTP 400: *reasoning_content in thinking mode must be passed back* |
| Нет | Можно не слать; если слать — API игнорирует |

Практическое правило NotCursor: **agent loop всегда шлёт `tools` → всегда round-trip `reasoning_content`**. Проще всего:

```
messages.append(response.choices[0].message)
```

то есть assistant-сообщение целиком: `role`, `content`, `reasoning_content`, `tool_calls`.

---

## 10. Ошибки HTTP

Типичные коды:

| Code | Смысл |
|---|---|
| 400 | Невалидный JSON / параметры |
| 401 | Неверный API key |
| 402 | Недостаточно баланса |
| 429 | Rate limit |
| 500 / 503 | Сервер / перегрузка |

Тело ошибки обычно:

```json
{
  "error": {
    "message": "...",
    "type": "...",
    "code": "..."
  }
}
```

NotCursor должен показать понятное сообщение в чате и не ретраить 401/400 без изменения запроса.

---

## 11. Минимальный curl для smoke-test

```bash
curl https://api.deepseek.com/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${DEEPSEEK_API_KEY}" \
  -d "{
    \"model\": \"deepseek-v4-flash\",
    \"messages\": [
      {\"role\": \"user\", \"content\": \"ping\"}
    ],
    \"stream\": false
  }"
```

---

## 12. Реализация в коде

Код **обязан** следовать этому документу, а не наоборот.

| Файл | Роль |
|---|---|
| `internal/llm/providers/deepseek/client.go` | `POST /chat/completions`, thinking default `enabled`, effort `high` |
| `internal/llm/message_json.go` | `content` string\|null\|parts; `arguments` string\|object; round-trip `reasoning_content` |
| `internal/agent/protocol.go` | `DecideNext`: loop пока `tool_calls` не пуст |
| `internal/agent/agent.go` | канон хода: append assistant целиком → tools → `role=tool` → снова API |
| `internal/config` | API key, model |

Этап 1: non-stream. SSE — этап 2 (`ChatCompletionStream`).

---

## 13. Канон хода агента (каждый момент)

Один user-вопрос = один **turn**. Внутри turn — N **sub-turn** к API.

```
T0  UI → Go.RunAgent(userText)
T1  messages = [system, …history, user]
T2  POST /chat/completions
      model, messages, tools, tool_choice=auto,
      thinking.enabled, reasoning_effort=high, stream=false
T3  HTTP 200 → choice.message
T4  append message КАК ЕСТЬ (content + reasoning_content + tool_calls)
T5  UI: reasoning (если есть), content (если есть)
T6  DecideNext:
      tool_calls не пуст → T7
      content_filter / insufficient_system_resource → ошибка, стоп
      иначе → T9 финал
T7  для каждого tool_call:
      UI tool_start → Execute → UI tool_end
      append {role:tool, tool_call_id, content}
T8  goto T2  (те же tools; reasoning_content предыдущих assistant обязателен)
T9  UI chat:done. content — ответ пользователю.
    Пустой content на промежуточном sub-turn (есть tool_calls) — норма.
    Пустой content на финале (нет tool_calls) — ошибка в UI.
```

Запрещено в loop:

- выходить по `finish_reason=stop`, если есть `tool_calls`;
- выкидывать `reasoning_content` из history, пока в запросе есть `tools`;
- вставлять фейковый `role=user` «ответь текстом» вместо продолжения sub-turn;
- слать `thinking: disabled` в agent loop (ломает tool follow-up на V4).

