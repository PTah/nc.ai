# ToDo: agent follow-ups (после v0.6.2)

**Только private remotes (`home`, `kalinamall`). На `github` эти ToDo не пушим.**

Шлифовка tools, не новый продуктный контур. Claritas как источник ядра агента исчерпан.

| Что | Откуда | Зачем | Когда |
|---|---|---|---|
| `command_kill` | фон-jobs (Cursor/Windsurf) | Остановить сервер, запущенный через `run_terminal` + `is_background` | Если фон реально мешает (нет PID/`Process.Kill` сейчас) |
| Несколько hunks в одном `apply_patch` | Cline | Несколько SEARCH/REPLACE за один вызов, меньше шагов на файл | По желанию; сейчас один блок на вызов |

Не делать из этого списка: MCP, субагенты, браузер, RAG, Ctrl+K.

Связанное: secrets — `docs/TODO-secrets-hardening.md` (этап 1 сделан в v0.6.3).
