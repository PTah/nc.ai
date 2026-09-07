# OpenRouter — протокол обмена

**Docs:** https://openrouter.ai/docs/api_reference/overview  
**Формат:** OpenAI Chat Completions + app attribution headers + prepaid credits.

**Статус в NotCursor:** реализовано (`internal/llm/providers/openrouter`), provider id `openrouter`.

---

## Endpoint

```
Base URL: https://openrouter.ai/api/v1
POST /chat/completions
GET  /models?supported_parameters=tools
GET  /credits   # prepaid balance (иногда нужен management key)
```

### Headers

```
Authorization: Bearer <OPENROUTER_API_KEY>
Content-Type: application/json
HTTP-Referer: https://git.papatramp.ru/PapaTramp/nc.ai
X-OpenRouter-Title: NotCursor.ai
X-Title: NotCursor.ai   # legacy alias, still accepted
```

`HTTP-Referer` + title — для ranking/attribution на openrouter.ai (не обязательны для inference).

---

## Особенности

1. `model` — `vendor/model` (+ опциональный суффикс маршрутизации: `:floor`, `:nitro`, …).
2. `:floor` — выбрать самый дешёвый live-провайдер для модели.
3. Prepaid: пополнение кредитов на openrouter.ai/credits (не monthly plan).
4. В ответе `usage.cost` — фактическая стоимость в USD credits; NotCursor использует её в `costing.Cost` при наличии.
5. Streaming SSE поддерживается API, но в агенте пока `stream: false` (как у DeepSeek/Z.ai).
6. Tools — OpenAI-shape; для agent loop нужны модели с `tools` в `supported_parameters`.

### Рекомендуемые coding-модели (tools)

| Model id | Назначение |
|---|---|
| `qwen/qwen3-coder-flash:floor` | дефолт / Auto default |
| `qwen/qwen3-coder:floor` | Auto complex / long-run |
| `qwen/qwen3-vl-8b-instruct` | vision |
| `qwen/qwen3-coder-plus`, `…-next`, `…-30b-a3b-instruct` | ручной выбор |

`qwen/qwen-2.5-coder-32b-instruct` **без tools** — не для agent loop.

---

## Пример запроса

```json
{
  "model": "qwen/qwen3-coder-flash:floor",
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
| 402 | нет кредитов |
| 429 | rate limit |
| 5xx | временный сбой / upstream |

Баланс: `available = total_credits - total_usage` из `GET /credits`.

---

## Реализация в NotCursor

- Клиент: `internal/llm/providers/openrouter`
- Settings: `openrouterApiKey`, `openrouterModel`, `activeProvider=openrouter`
- UI: Settings → Provider → OpenRouter; top-bar model + bal
- Auto-models: flash:floor → coder:floor; images → qwen3-vl
