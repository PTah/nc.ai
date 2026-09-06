# Ollama — локальный OpenAI-совместимый endpoint

**Docs:** https://github.com/ollama/ollama/blob/main/docs/openai.md  
**Назначение:** офлайн / локальные модели без внешнего API key.

---

## Endpoint

```
POST http://localhost:11434/v1/chat/completions
Content-Type: application/json
```

Auth обычно не требуется (localhost). При remote Ollama — опциональный Bearer.

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

---

## Особенности

| Тема | Поведение |
|---|---|
| Модели | Локальные теги Ollama (`ollama list`) |
| Tools | Зависит от модели; не все поддерживают function calling |
| Latency | Холодный старт модели может быть долгим |
| Privacy | Данные не уходят в облако |

---

## Healthcheck

```
GET http://localhost:11434/api/tags
```

NotCursor перед выбором Ollama проверяет доступность демона и список моделей.

---

## Реализация

Этап 2: `internal/llm/providers/ollama`.  
В Settings: host (default `http://localhost:11434`), model tag.
