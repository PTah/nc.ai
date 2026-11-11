<!-- Edited by nc.ai 1.1.15 on macbook 2026-11-11 21:46:26 -->
# Протоколы обмена с AI-провайдерами

Каталог описывает **wire-format** запросов/ответов, которые использует NotCursor.ai.

## Внутренний канон

Приложение работает во **внутреннем формате OpenAI Chat Completions** (`messages`, `tools`, `tool_calls`, SSE `stream: true`).

Каждый провайдер реализует интерфейс:

```go
type Provider interface {
    Name() string
    ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
}
```

`ChatCompletionStream` — **реализовано у всех провайдеров**: DeepSeek, Z.ai, Qwen, Yandex, OpenRouter и Local / custom разбираются общим кодом (`internal/llm/openai_stream.go`), Anthropic Messages — своим SSE-парсером (`internal/llm/providers/anthropic/stream.go`). Агент стримит дельты, собирает финальное сообщение и продолжает loop по `tool_calls`. Если эндпоинт не принимает `stream_options`, запрос автоматически откатывается на non-stream.

Общие правила поверх любого провайдера:

- **Инлайн-thinking**: ведущие блоки `<think>`, `<thinking>`, `<reasoning>` в `content` отделяются в reasoning — и в потоке дельт, и в собранном ответе (`internal/llm/think.go`). Литеральный тег в середине ответа не трогается.
- **Лимиты**: любой `429` (и Z.ai `1302`) превращается в `*llm.RateLimitError` — у **всех** провайдеров,
  через общий `llm.NewRateLimitError`. Разбираются `Retry-After` (секунды и HTTP-дата),
  `x-ratelimit-reset-*` (длительность `6m0s` **или** unix-время сброса), `anthropic-ratelimit-*-reset`
  (RFC3339) и остаток из `x-ratelimit-remaining-*`; из тела берётся `limit_source` (OpenRouter:
  `requests` / `tokens` / `credits`) и печатается как «лимит по токенам». Агент ждёт лимит (до 60 сек)
  и повторяет запрос сам, суточный/длинный лимит не ждёт — отдаёт ошибку с подсказкой сброса
  (`internal/llm/ratelimit.go`).
- **Фолбэк**: `fallbackProvider` + `fallbackModel` в `settings.json` — при длинном лимите (ожидание
  больше минуты) или перегрузке (503/529) прогон один раз переключается на резерв
  (`agent.Runner.switchToFallback`): в чат уходит `notice`, `Provider`/`ProviderID`/`runModel`
  заменяются, запрос повторяется. Второго переключения нет — защита от цикла. Local в резерв не
  подставляется, если в запросе есть изображения.

Код в `internal/llm/providers/*` и `internal/agent` **следует документам этой папки**. Если поведение API изменилось — сначала обновить протокол, потом код.

## Документы

| Файл | Провайдер | Статус |
|---|---|---|
| [openai.md](./openai.md) | OpenAI (эталон) | спецификация |
| [deepseek.md](./deepseek.md) | DeepSeek | **этап 1 — первый endpoint** |
| [openrouter.md](./openrouter.md) | OpenRouter | **реализовано** |
| [qwen.md](./qwen.md) | Qwen (Alibaba Cloud / DashScope) | **реализовано** |
- [kimi.md](kimi.md) — Kimi / Moonshot AI
| [yandex.md](./yandex.md) | Yandex AI Studio | **реализовано** |
| [anthropic.md](./anthropic.md) | Anthropic Claude | **реализовано** (профиль `protocol: anthropic`) |
| [zai-glm.md](./zai-glm.md) | Z.ai / BigModel GLM | спецификация |
| [ollama.md](./ollama.md) | Local / Ollama / LM Studio (OpenAI-compat) | **реализовано** |
| [tools-and-agent-loop.md](./tools-and-agent-loop.md) | Function calling / agent loop | обязательно |

## Быстрая таблица endpoint'ов

| Провайдер | Base URL | Chat endpoint | Auth |
|---|---|---|---|
| DeepSeek | `https://api.deepseek.com` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| OpenAI | `https://api.openai.com/v1` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| OpenRouter | `https://openrouter.ai/api/v1` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| Qwen (DashScope) | `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| Yandex AI Studio | `https://ai.api.cloud.yandex.net/v1` | `POST /chat/completions` | `Authorization: Api-Key <key>` + `x-folder-id` |
| Anthropic | `https://api.anthropic.com` | `POST /v1/messages` | `x-api-key` + `anthropic-version` |
| Z.ai | `https://open.bigmodel.cn/api/paas/v4` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| Local | `http://127.0.0.1:11434/v1` (настраивается) | `POST /chat/completions` | опциональный Bearer |
| Custom-профиль с `protocol: anthropic` | `https://api.anthropic.com` (или Selora, Atria) | `POST /v1/messages` | `x-api-key` + `anthropic-version` (и Bearer для роутеров) |

Профили **Local / custom** принимают любой OpenAI-совместимый хост, поэтому публичные
роутеры бесплатных тарифов (Atria, Routeway, Selora, ShareLLM, Vireonix, OdiRouter)
заводятся без кода: Settings → Local → Base URL + ключ. Формат общения переключается
полем `protocol` (Chat Completions / Anthropic Messages).

## Источники (официальные)

- DeepSeek: https://api-docs.deepseek.com/
- OpenAI: https://platform.openai.com/docs/api-reference/chat
- OpenRouter: https://openrouter.ai/docs/api-reference/overview
- Qwen (DashScope): https://www.alibabacloud.com/help/en/model-studio/compatibility-of-openai-with-dashscope
- Yandex AI Studio: https://yandex.cloud/docs/ai-studio/concepts/openai-compatibility
- Anthropic: https://docs.anthropic.com/en/api/messages
- Z.ai / BigModel: https://docs.bigmodel.cn/
- Ollama: https://github.com/ollama/ollama/blob/main/docs/openai.md
