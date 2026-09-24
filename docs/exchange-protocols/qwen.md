# Qwen (Alibaba Cloud Model Studio / DashScope) — протокол обмена

**Docs:** https://www.alibabacloud.com/help/en/model-studio/compatibility-of-openai-with-dashscope
**Формат:** OpenAI Chat Completions (compatible-mode) + tool calling.

**Статус в NotCursor:** реализовано (`internal/llm/providers/qwen`), provider id `qwen`.

---

## Endpoint

```
Base URL (intl): https://dashscope-intl.aliyuncs.com/compatible-mode/v1
Base URL (cn):   https://dashscope.aliyuncs.com/compatible-mode/v1
POST /chat/completions
GET  /models
```

### Headers

```
Authorization: *** <DASHSCOPE_API_KEY>
Content-Type: application/json
```

---

## Особенности

1. Полностью OpenAI-совместимый `compatible-mode` — тот же wire-format, что у DeepSeek/OpenRouter:
   `messages`, `tools`, `tool_calls`, `finish_reason`.
2. Регион: `intl` (международный, по умолчанию) или `cn` (mainland China). Хранится в
   `settings.json` как `qwenEndpoint`.
3. Tool calling работает «из коробки» — нужны модели с поддержкой tools (`qwen-max`, `qwen-plus`,
   `qwen-turbo`, `qwen3-coder-*`).
4. Ответ **не содержит** `usage.cost` (в отличие от OpenRouter). Стоимость считаем по
   собственному тарифу в `costing` (approx USD / 1M).
5. Streaming SSE: агент использует `stream: true` с `stream_options.include_usage`
   (usage приходит последним чанком).
6. Баланса через API нет — смотреть консоль Model Studio.

### Рекомендуемые модели (tools)

| Model id | Назначение |
|---|---|
| `qwen-plus` | balanced / Auto default |
| `qwen-turbo` | самый быстрый и дешёвый |
| `qwen-max` | flagship reasoning / Auto complex |
| `qwen3-coder-plus` | coding, сильный |
| `qwen3-coder-flash` | coding, быстрый |
| `qwen-vl-max`, `qwen-vl-plus` | vision (картинки) |

---

## Пример запроса

```json
{
  "model": "qwen-plus",
  "temperature": 0.2,
  "stream": false,
  "messages": [
    { "role": "system", "content": "…" },
    { "role": "user", "content": "…" }
  ],
  "tools": []
}
```

---

## Ошибки и биллинг

| HTTP | Смысл |
|---|---|
| 401 | неверный ключ |
| 402 / 403 | нет средств / квота исчерпана / доступ запрещён |
| 429 | rate limit |
| 5xx | временный сбой |

Ключ создаётся в **Alibaba Cloud Model Studio (DashScope)** → API-KEY.

---

## Реализация в NotCursor

- Клиент: `internal/llm/providers/qwen`
- Settings: `qwenApiKey`, `qwenModel`, `qwenEndpoint`, `activeProvider=qwen`
- UI: Settings → Provider → Qwen (DashScope); top-bar model
- Auto-models: `qwen-plus` → `qwen-max`; images → `qwen-vl-max`
- Цены: `internal/costing/costing.go` (`builtinQwenSheet`)
