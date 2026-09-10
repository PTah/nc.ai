# Local — OpenAI-совместимый LAN / localhost

**Назначение:** офлайн и LAN-модели без облачного API (Ollama, LM Studio, vLLM, LocalAI, Open WebUI и т.п.).

**Код:** `internal/llm/providers/local`  
**Settings:** Provider → Local (LAN / Ollama)

---

## Endpoint

```
POST {baseURL}/chat/completions
Content-Type: application/json
Authorization: Bearer <token>   # только если задан токен
```

Типичные base URL:

| Сервер | Base URL |
|---|---|
| Ollama | `http://127.0.0.1:11434/v1` |
| LM Studio | `http://127.0.0.1:1234/v1` |
| LAN Ollama | `http://192.168.x.x:11434/v1` |

Auth обычно не требуется на localhost. На remote — опциональный Bearer.

---

## Пример

```json
{
  "model": "qwen2.5-coder:14b",
  "messages": [
    { "role": "system", "content": "You are NotCursor local agent." },
    { "role": "user", "content": "List TODOs in the project" }
  ],
  "stream": false,
  "tools": []
}
```

Поля `thinking` / `reasoning_effort` **не** отправляются (многие локальные серверы их отвергают).

---

## Список моделей

1. `GET {baseURL}/models` → `data[].id`
2. Если пусто/ошибка и base оканчивается на `/v1`: `GET {origin}/api/tags` (Ollama) → `models[].name`

В UI: select из списка + поле **Custom model id** + кнопка Refresh models.

---

## Особенности

| Тема | Поведение |
|---|---|
| Модели | Теги сервера; ручной id всегда допустим |
| Tools | Зависит от модели; не все поддерживают function calling |
| Auto-models | Выключен — всегда выбранная модель |
| Latency | Холодный старт может быть долгим (timeout клиента 300s) |
| Cost | $0 (локально) |
| Privacy | Данные не уходят в облако |

---

## Healthcheck

```
GET http://127.0.0.1:11434/api/tags
```

или

```
GET http://127.0.0.1:11434/v1/models
```

В приложении: Settings → Test connect (Local).
