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
  "stream": true,
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
| Tools | Зависит от модели. Если модель пишет JSON tool-call в `content` вместо `tool_calls`, агент поднимает его в structured calls (`PromoteTextToolCalls`) |
| Stream | Local-провайдер использует SSE (`stream: true`); токены сразу идут в UI. При promote text→tool_calls UI снимает черновик (`delta_clear`) |
| Auto-models | Выключен — всегда выбранная модель |
| Latency | Холодный старт может быть долгим (см. ниже); HTTP timeout у non-stream — 300s, stream — без Timeout (отмена через ctx) |
| Cost | $0 (локально) |
| Privacy | Данные не уходят в облако |

---

## Холодный старт и `OLLAMA_KEEP_ALIVE`

По умолчанию Ollama **выгружает** модель из VRAM через ~5 минут простоя (`OLLAMA_KEEP_ALIVE=5m`). Следующий запрос снова грузит веса (секунды–десятки секунд) — это и есть «холодный старт».

На Windows-сервере с Ollama:

```powershell
[Environment]::SetEnvironmentVariable("OLLAMA_KEEP_ALIVE", "30m", "Machine")
# или "-1" — не выгружать
```

Потом полностью выйти из Ollama (трей) и запустить снова.

---

## Диагностика GPU / `size_vram`

```bash
# Что сейчас в VRAM
curl http://127.0.0.1:11434/api/ps
```

Смотри `size_vram`: **> 0** — модель на GPU; **0** — по сути CPU (очень медленно). Параллельно на сервере: `nvidia-smi -l 1` — во время генерации GPU-Util должен вырасти.

Замер скорости (без агента):

```bash
curl http://127.0.0.1:11434/api/generate -d "{\"model\":\"qwen2.5-coder:7b\",\"prompt\":\"Привет\",\"stream\":false}"
```

`tok/s ≈ eval_count / eval_duration * 1e9`. Для 7B Q4 на нормальной видеокарте ожидай десятки tok/s; <10 — почти наверняка CPU или холодный load.

---

## Несколько локальных серверов

В Settings → Provider выбирается каждый профиль как `Local: <имя>`.

В `settings.json`:

```json
{
  "activeProvider": "local:home-gpu",
  "localEndpoints": [
    {
      "id": "default",
      "name": "Local",
      "baseUrl": "http://127.0.0.1:11434/v1",
      "model": "qwen2.5-coder:7b"
    },
    {
      "id": "home-gpu",
      "name": "Ollama @ home",
      "baseUrl": "http://192.168.128.5:11434/v1",
      "model": "qwen2.5-coder:14b"
    }
  ]
}
```

Токены — в секрет-сторе под `local` (default) и `local:<id>`. GUI: Add / Duplicate / Delete.

Старые плоские `localBaseUrl` / `localModel` мигрируют в endpoint `default` при загрузке.

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
