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
| Lemonade (AMD) | `http://127.0.0.1:8000/api/v1` |
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

Ответ thinking-моделей Ollama приходит в `delta.reasoning` (не-стрим: `message.reasoning`) — приложение читает это поле и показывает как «размышления». Если модель успела подумать, но не выдала текст, чат не остаётся пустым: приложение один раз просит ответить текстом, а затем объясняет причину.

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
| Auto-models | По умолчанию **вкл**: из каталога берутся tool-capable coder-модели, на простой шаг — меньшая. Если выбор отличается от модели в Settings, в чат уходит пояснение (в Settings сохраняется выбранная Auto модель) |
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

## Контекст (`num_ctx`, `OLLAMA_CONTEXT_LENGTH`)

Ollama в OpenAI-совместимом режиме (`/v1/chat/completions`) поле `options.num_ctx` **не принимает** (ollama#5356) — контекст задаётся на сервере:

```powershell
[Environment]::SetEnvironmentVariable("OLLAMA_CONTEXT_LENGTH", "8192", "Machine")
```

Затем полностью выйти из Ollama и запустить снова. Маленькое окно (дефолт 4096) — частая причина «пустого ответа»: сервер молча обрезает промпт, и модель отвечает ничем.

В Settings → Local есть поле **Context** — в запрос оно **не отправляется**: нестандартные поля (`options.num_ctx`) строгие OpenAI-серверы (Lemonade, vLLM, LM Studio) могут отвергнуть с 400, а Ollama в OpenAI-режиме его игнорирует. Значение нужно приложению, чтобы считать заполнение: в статус-баре видно `ctx 12.4k / 32k`, а когда промпт подошёл к 85 % окна, в чат приходит предупреждение (если окно не задано — предупреждение срабатывает на 8k).

Серверы без Ollama-эндпоинтов (Lemonade, LM Studio, vLLM) распознаются автоматически: по 404 на `/api/ps` приложение перестаёт дёргать `/api/ps` и `/api/generate` и пингует только `/v1/chat/completions`, поэтому в шапке не появляется ложное «Local down».

Если места всё равно не хватает: `Custom lite` (короткий промпт, ~12 инструментов) и галочка **«Не подключать проектные правила»** — правила проекта могут занимать десятки КБ.

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
      "model": "qwen2.5-coder:14b",
      "numCtx": 8192
    }
  ]
}
```

Токены — в секрет-сторе под `local` (default) и `local:<id>`. GUI: Add / Duplicate / Delete.

Простыми словами (Lite, Auto-models, индикатор Custom): [../local-models-ru.md](../local-models-ru.md).

Старые плоские `localBaseUrl` / `localModel` мигрируют в endpoint `default` при загрузке.

### Custom lite + Auto-models

**Custom lite** — опциональная галочка (по умолчанию **выкл**). Если включена:
- короткий system prompt;
- ~12 tools (файлы/поиск/git read/ask_user) — без shell/ssh/web/commit;
- урезанная project map.

Без lite — полный system + полный набор tools (как у облачных провайдеров). На qwen2.5-coder 7B/14B лучше держать lite **выкл**.

На Custom при сбое (битый tool JSON) агент один раз повторяет ход с **read-only** tools (`read_file`/`grep`/…), без `write_file`. Повтор включает только если в прогоне ещё не выполнено ни одного инструмента: законный финальный ответ после чтения файлов больше не принимается за догадку. `tool_choice=required` не используется — слабые модели из‑за него сыплют JSON в чат.

**Auto-models** (если включён): из каталога Ollama берёт модели с capability `tools`, coder-имена; на простые задачи — меньший размер (7b), на сложные/длинный прогон — больший (14b+). Модели без `tools` (часто `deepseek-coder-v2`) не выбираются.

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
