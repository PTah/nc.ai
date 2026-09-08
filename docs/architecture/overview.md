# Архитектура NotCursor.ai

См. также [ТЗ](../TZ.md) и [протоколы](../exchange-protocols/README.md).

```text
frontend/                  React UI (projects, tree, chat, tabs, terminal, settings)
internal/
  app bindings             app.go / main.go — Wails façade, lifecycle, layout/geometry
  config/                  settings store (окно, layout, тема, провайдеры, ключи)
  workspace/               projects + sandbox FS + guard секретов
  fsx/                     атомарная запись файлов (temp + rename + .bak)
  llm/                     canonical OpenAI types, Provider interface, общий shared слой
  llm/providers/
    deepseek/              DeepSeek (OpenAI-compatible)
    zai/                   Z.ai (glm-*, /models, balance, err mapping)
  tools/                   registry + executor (read/write/list/search/terminal/git/ssh)
  agent/                   tool loop + autoroute + retry + компакция истории
  shell/                   PowerShell / oneshot (скрытое окно)
  gitx/                    OS git (status/diff/commit/push)
  sshx/                    SSH exec + keygen (known_hosts/TOFU)
  rules/                   загрузчик Cursor rules (global + project, рекурсивно)
  chatstore/               multi-session persistence + archive
  costing/                 token → USD (peak/off-peak)
```

## Поток чата (текущий, v0.5.x)

1. UI формирует user message (текст + опционально вложения/картинки).
2. `App.RunAgentWithAttachments` → `agent.Runner`.
3. Runner резолвит модель (flash/pro/vision), применяет Cursor rules + компакцию истории.
4. LLM-провайдер возвращает `tool_calls` или финальный текст.
5. Tools исполняются локально (sandbox FS, PowerShell, git, ssh), результат — обратно в LLM.
6. UI получает события `delta/reasoning/tool_start/tool_end/done/error/reconnect/persist`.
7. Итоговый транскрипт (items) пишет фронт, LLM-history — бэк в `chatstore`.

## Ключевые решения

- **Провайдеры взаимозаменяемы** через общий OpenAI-compat wire-формат (`llm`), различия — в клиентах.
- **Безопасность секретов**: `read_file` не отдаёт `.env`/ключи модели; значения `KEY=…` маскируются.
- **SSH без `InsecureIgnoreHostKey`**: `known_hosts` + trust-on-first-use.
- **Надёжная персистентность**: атомарная запись JSON с `.bak`.
- **Агент умеет продолжать**: автоповторы transient-ошибок + ручной `Reconnect`.
