# Z.ai / BigModel (GLM) — протокол обмена

**Docs:** https://docs.z.ai/ · DevPack: https://docs.z.ai/devpack/latest-model  
**Формат:** OpenAI-compatible Chat Completions.

---

## Endpoints

| Режим | Base URL | Когда |
|---|---|---|
| **Pay-as-you-go** (default в NotCursor) | `https://api.z.ai/api/paas/v4` | Обычный баланс / OpenAI SDK |
| Coding Plan | `https://api.z.ai/api/coding/paas/v4` | GLM Coding Plan subscription |
| Anthropic-compatible | `https://api.z.ai/api/anthropic` | Claude Code / Goose |
| China BigModel | `https://open.bigmodel.cn/api/paas/v4` | Mainland |

```
POST {base}/chat/completions
Authorization: Bearer <ZAI_API_KEY>
Content-Type: application/json
```

---

## Модели (DevPack)

| ID | Назначение |
|---|---|
| `glm-5.3` | Default / text-only |
| `glm-5.3-flash` | Экономия + multimodal (vision) |
| `glm-4.7-flash` | Быстрые тесты |
| остальные `glm-5.*` / `glm-4.*` | По необходимости |

Default: `glm-5.3`. Temperature по умолчанию `0.2`.

---

## Список моделей

```
GET {base}/models
Authorization: Bearer <ZAI_API_KEY>
```

Ответ (OpenAI-compatible):

```json
{
  "object": "list",
  "data": [
    { "id": "glm-5.3", "object": "model", "owned_by": "z-ai" },
    { "id": "glm-5.3-flash", "object": "model", "owned_by": "z-ai" }
  ]
}
```

В NotCursor: `ListZaiModels()` → dropdown; без ключа / при ошибке — fallback ids.

---

## Пример (Pay-as-you-go)

```bash
curl -X POST "https://api.z.ai/api/paas/v4/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "glm-5.3",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

---

## Function calling / Thinking

OpenAI-like `tools` / `tool_calls`.  
`thinking: {"type": "enabled"|"disabled"}` (GLM-4.5+).  
`reasoning_effort` для GLM-5.2+ (`max` / `high` / `low`).

---

## Реализация

`internal/llm/providers/zai` — default base = pay-as-you-go URL.  
В Settings: переключатель Pay-as-you-go / Coding Plan.
