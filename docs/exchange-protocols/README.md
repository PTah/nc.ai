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

`ChatCompletionStream` — **реализовано у всех провайдеров**: DeepSeek, Z.ai, Qwen, OpenRouter и Local / custom разбираются общим кодом (`internal/llm/openai_stream.go`), Anthropic Messages — своим SSE-парсером (`internal/llm/providers/anthropic/stream.go`). Агент стримит дельты, собирает финальное сообщение и продолжает loop по `tool_calls`. Если эндпоинт не принимает `stream_options`, запрос автоматически откатывается на non-stream.

Общие правила поверх любого провайдера:

- **Инлайн-thinking**: ведущие блоки `<think>`, `<thinking>`, `<reasoning>` в `content` отделяются в reasoning — и в потоке дельт, и в собранном ответе (`internal/llm/think.go`). Литеральный тег в середине ответа не трогается.
- **Лимиты**: `429` превращается в `*llm.RateLimitError` с разбором `Retry-After` и `x-ratelimit-reset-*`; агент ждёт лимит (до 60 сек) и повторяет запрос сам, суточный лимит не ждёт (`internal/llm/ratelimit.go`).

Код в `internal/llm/providers/*` и `internal/agent` **следует документам этой папки**. Если поведение API изменилось — сначала обновить протокол, потом код.

## Документы

| Файл | Провайдер | Статус |
|---|---|---|
| [openai.md](./openai.md) | OpenAI (эталон) | спецификация |
| [deepseek.md](./deepseek.md) | DeepSeek | **этап 1 — первый endpoint** |
| [openrouter.md](./openrouter.md) | OpenRouter | **реализовано** |
| [qwen.md](./qwen.md) | Qwen (Alibaba Cloud / DashScope) | **реализовано** |
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
- Anthropic: https://docs.anthropic.com/en/api/messages
- Z.ai / BigModel: https://docs.bigmodel.cn/
- Ollama: https://github.com/ollama/ollama/blob/main/docs/openai.md
