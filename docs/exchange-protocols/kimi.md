# Kimi (Moonshot AI) — протокол обмена

**Docs:** https://platform.kimi.ai/docs/api/overview  
**Формат:** OpenAI Chat Completions (+ Responses / Anthropic Messages как альтернативы).  
**Статус в NotCursor:** реализовано (`internal/llm/providers/kimi`), provider id `kimi`.

## Endpoint

```
Base URL (global): https://api.moonshot.ai/v1
POST /chat/completions
GET  /models
GET  /users/me/balance
```

Ключи Global и CN **не** взаимозаменяемы. В приложении по умолчанию global.

### Headers

```
Authorization: Bearer <MOONSHOT_API_KEY>
Content-Type: application/json
```

## Особенности

1. OpenAI-совместимый wire-format: `messages`, `tools`, `tool_calls`, SSE stream.
2. K3 / K3.1: thinking всегда включён; шлём `reasoning_effort` (`low`/`high`/`max`), **не** `thinking{}`.
3. В ответе / истории — `reasoning_content` (Preserved Thinking: возвращать как есть).
4. Баланс: `GET /users/me/balance` → `available_balance` / `voucher_balance` / `cash_balance` (USD).
5. Стоимость считаем по sheet в `costing` (usage.cost API не отдаёт).

### Модели

| Model id | Назначение |
|---|---|
| `kimi-k3.1-pro` | flagship / Auto complex / vision |
| `kimi-k3.1-pro-highspeed` | быстрее, Auto default |
| `kimi-k3` | предыдущий флагман |
| `kimi-k2.7-code` | coding (K2; новым аккаунтам с 31.10.2026 недоступна) |
