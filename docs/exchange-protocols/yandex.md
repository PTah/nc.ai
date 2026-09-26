# Yandex AI Studio — протокол обмена

**Docs:**
- https://yandex.cloud/docs/ai-studio/concepts/openai-compatibility
- https://yandex.cloud/docs/iam/concepts/authorization/api-key
- https://yandex.cloud/docs/tutorials/ml-ai/ai-model-ide-integration

**Формат:** OpenAI Chat Completions (AI Studio compatible) + tool calling.

**Статус в NotCursor:** реализовано (`internal/llm/providers/yandex`), provider id `yandex`.

---

## Endpoint

```
Base URL: https://ai.api.cloud.yandex.net/v1
POST /chat/completions
GET  /models
```

### Headers

```
Authorization: Api-Key <YANDEX_API_KEY>
x-folder-id: <FOLDER_ID>
OpenAI-Project: <FOLDER_ID>
Content-Type: application/json
```

Ключ — **API-ключ сервисного аккаунта** (не IAM-токен и не OAuth). Scope:
`yc.ai.languageModels.execute`. Роль на каталог: `ai.languageModels.user`.

`FOLDER_ID` — ID каталога (folder) в Yandex Cloud, где включён AI Studio.

---

## Особенности

1. OpenAI-совместимый gateway AI Studio — тот же wire-format, что у DeepSeek/Qwen:
   `messages`, `tools`, `tool_calls`, `finish_reason`, SSE `stream: true`.
2. Модель в запросе — **URI**, не короткое имя:
   `gpt://<folder_id>/<model_id>/latest` (например `gpt://b1g…/yandexgpt/latest`).
   В Settings храним короткое имя (`yandexgpt`); клиент сам собирает URI из folder id.
3. Авторизация именно `Api-Key …` (не `Bearer`). Folder передаём и в `x-folder-id`,
   и в `OpenAI-Project` (как OpenAI SDK `project=`).
4. Ответ **не содержит** `usage.cost`. Стоимость считаем по тарифу в `costing` (approx USD).
5. Streaming SSE с `stream_options.include_usage`; при отказе gateway — fallback non-stream.
6. Баланса через API нет — смотреть биллинг Yandex Cloud.
7. Отдельной vision-модели YandexGPT нет; картинки в Auto-models идут на сильную текстовую
   модель (лучший effort) — для vision лучше другой провайдер.

### Рекомендуемые модели (tools)

| Model id (короткий) | Назначение |
|---|---|
| `yandexgpt` | balanced / Auto default |
| `yandexgpt-lite` | быстрый / дешёвый |
| `qwen3-235b-a22b-fp8` | coding / сложные задачи (хостится в AI Studio) |
| `gpt-oss-120b` | альтернативный large |

Полный URI в API: `gpt://<folder_id>/<model_id>/latest`.

---

## Пример запроса

```json
{
  "model": "gpt://b1gxxxxxxxx/yandexgpt/latest",
  "temperature": 0.3,
  "stream": false,
  "messages": [
    { "role": "system", "content": "…" },
    { "role": "user", "content": "…" }
  ],
  "tools": []
}
```

```bash
curl -sS https://ai.api.cloud.yandex.net/v1/chat/completions \
  -H "Authorization: Api-Key $YANDEX_API_KEY" \
  -H "x-folder-id: $YANDEX_FOLDER_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt://'"$YANDEX_FOLDER_ID"'/yandexgpt/latest",
    "messages": [{"role":"user","content":"Привет"}]
  }'
```

---

## Ошибки и биллинг

| HTTP | Смысл |
|---|---|
| 401 | неверный API-ключ |
| 403 | нет роли / неверный scope / нет доступа к folder |
| 404 | модель / URI не найдены |
| 429 | rate limit |
| 5xx | временный сбой |

Ключ: консоль Yandex Cloud → IAM → сервисный аккаунт → API-ключи.
Folder id: консоль → каталог → идентификатор (или `yc config get folder-id`).

---

## Реализация в NotCursor

- Клиент: `internal/llm/providers/yandex`
- Settings: `yandexApiKey` (keychain), `yandexFolderId`, `yandexModel`, `activeProvider=yandex`
- UI: Settings → Provider → Yandex AI Studio
- Auto-models: `yandexgpt` → `qwen3-235b-a22b-fp8` на сложных задачах
- Цены: `internal/costing/costing.go` (`builtinYandexSheet`)
