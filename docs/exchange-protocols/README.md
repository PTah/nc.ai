# Протоколы обмена с AI-провайдерами

Каталог описывает **wire-format** запросов/ответов, которые использует NotCursor.ai.

## Внутренний канон

Приложение работает во **внутреннем формате OpenAI Chat Completions** (`messages`, `tools`, `tool_calls`, SSE `stream: true`).

Каждый провайдер реализует интерфейс:

```go
type Provider interface {
    Name() string
    ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    ChatCompletionStream(ctx context.Context, req *ChatRequest, out chan<- StreamEvent) error
}
```

Адаптеры в `internal/llm/providers/*` переводят канон ↔ нативный API провайдера.

## Документы

| Файл | Провайдер | Статус |
|---|---|---|
| [openai.md](./openai.md) | OpenAI (эталон) | спецификация |
| [deepseek.md](./deepseek.md) | DeepSeek | **этап 1 — первый endpoint** |
| [openrouter.md](./openrouter.md) | OpenRouter | спецификация |
| [anthropic.md](./anthropic.md) | Anthropic Claude | спецификация + маппинг |
| [zai-glm.md](./zai-glm.md) | Z.ai / BigModel GLM | спецификация |
| [ollama.md](./ollama.md) | Ollama (локально) | спецификация |
| [tools-and-agent-loop.md](./tools-and-agent-loop.md) | Function calling / agent loop | обязательно |

## Быстрая таблица endpoint'ов

| Провайдер | Base URL | Chat endpoint | Auth |
|---|---|---|---|
| DeepSeek | `https://api.deepseek.com` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| OpenAI | `https://api.openai.com/v1` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| OpenRouter | `https://openrouter.ai/api/v1` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| Anthropic | `https://api.anthropic.com` | `POST /v1/messages` | `x-api-key` + `anthropic-version` |
| Z.ai | `https://open.bigmodel.cn/api/paas/v4` | `POST /chat/completions` | `Authorization: Bearer <key>` |
| Ollama | `http://localhost:11434/v1` | `POST /chat/completions` | обычно без ключа |

## Источники (официальные)

- DeepSeek: https://api-docs.deepseek.com/
- OpenAI: https://platform.openai.com/docs/api-reference/chat
- OpenRouter: https://openrouter.ai/docs/api-reference/overview
- Anthropic: https://docs.anthropic.com/en/api/messages
- Z.ai / BigModel: https://docs.bigmodel.cn/
- Ollama: https://github.com/ollama/ollama/blob/main/docs/openai.md
