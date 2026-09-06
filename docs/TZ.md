# Техническое задание: NotCursor.ai

**Версия:** 0.1.0  
**Дата:** 2026-09-06  
**Репозиторий:** https://git.papatramp.ru/PapaTramp/nc.ai  
**Статус:** этап 1 — фундамент (DeepSeek + shell/git/ssh)

---

## 1. Цель продукта

**NotCursor.ai** — лёгкий нативный IDE-агент (аналог Cursor), который:

- запускается за доли секунды;
- весит порядка **15–20 МБ** в релизной сборке;
- компилируется в **Windows `.exe`** и **macOS `.app`**;
- даёт UI «как в Cursor»: проекты, дерево файлов, чат с ИИ-агентом, терминал;
- выполняет действия агента локально: файлы, PowerShell/shell, Git/Gitea, SSH.

Стек: **Go (бэкенд)** + **React/TypeScript + Tailwind (фронтенд)** через **Wails v2** (системный WebView вместо Chromium).

---

## 2. Нефункциональные требования

| Требование | Целевое значение |
|---|---|
| Холодный старт | < 500 мс на типичном SSD (Windows/macOS) |
| Размер релизного бинарника | 15–20 МБ (без встроенных моделей) |
| Платформы | Windows 10/11 x64, macOS (arm64/x64) |
| Сеть | Исходящие HTTPS к AI-провайдерам; SSH к серверам; Git remote (Gitea) |
| Офлайн | UI, FS, shell, git, ssh — без сети; чат ИИ требует ключ/endpoint |
| Секреты | API keys и SSH private keys только в локальном защищённом хранилище, не в репозитории |
| Лицензия кода проекта | определяется владельцем репозитория |

---

## 3. Архитектура

```
┌────────────────────────────────────────────────────────────┐
│  Frontend (Wails WebView)                                  │
│  React + TS + Tailwind + Xterm.js                          │
│  • Projects sidebar  • File tree  • Chat  • Terminal       │
└───────────────────────────┬────────────────────────────────┘
                            │ Wails bindings / Events (SSE-like)
┌───────────────────────────▼────────────────────────────────┐
│  Go App (Wails runtime)                                    │
│  ┌─────────────┐ ┌────────────┐ ┌──────────┐ ┌───────────┐ │
│  │ Agent Loop  │ │ Providers  │ │ Tools    │ │ Workspace │ │
│  │ (orchestr.) │ │ DeepSeek…  │ │ FS/Shell │ │ Projects  │ │
│  └──────┬──────┘ └─────▲──────┘ │ Git/SSH  │ └───────────┘ │
│         │              │        └──────────┘               │
│         └──────────────┘ tool_calls → execute → feedback   │
└────────────────────────────────────────────────────────────┘
```

### 3.1. Frontend

Один композиционный layout (как Cursor):

1. **Слева постоянно** — список проектов (recent / pinned / open).
2. **Слева опционально** — дерево файлов активного workspace.
3. **Центр** — чат с агентом (streaming, tool call cards, diffs).
4. **Снизу** — интегрированный терминал (**Xterm.js** + PTY через Go).
5. Позже: редактор файлов / diff viewer (этап 2+).

### 3.2. Backend (Go)

Модули:

| Пакет | Назначение |
|---|---|
| `internal/app` | Wails App struct, lifecycle, bindings |
| `internal/workspace` | проекты, пути, ignore, watch |
| `internal/fsops` | чтение/запись/list/search (через `os`/`io`) |
| `internal/shell` | PowerShell / bash / cmd через PTY (`os/exec`) |
| `internal/gitx` | commit/diff/push/pull через **go-git** + опционально system git |
| `internal/sshx` | SSH-сессии, ключи, known_hosts (`golang.org/x/crypto/ssh`) |
| `internal/llm` | единый клиент chat/completions + streaming |
| `internal/llm/providers` | DeepSeek (этап 1), далее OpenAI/OpenRouter/Anthropic/Ollama/Z.ai |
| `internal/agent` | agent loop: messages ↔ tools ↔ provider |
| `internal/tools` | tool registry: write_file, read_file, run_terminal, git_*, ssh_* |
| `internal/config` | settings.json + encrypted secrets |
| `internal/gitea` | remote helpers для Gitea (HTTP API / git remote) |

---

## 4. Этапы разработки

### Этап 1 (текущий) — MVP «как Cursor» + DeepSeek

- [ ] ТЗ и документация протоколов (`/docs`)
- [ ] Каркас Wails (Go + React TS + Tailwind)
- [ ] Layout: projects / file tree / chat / terminal
- [ ] DeepSeek provider: `POST https://api.deepseek.com/chat/completions`
- [ ] Streaming SSE в UI
- [ ] Function calling / tools: read/write file, list dir, run shell
- [ ] Локальный shell (PowerShell на Windows)
- [ ] go-git: status, diff, commit, push в Gitea
- [ ] SSH: connect, exec, generate keypair, known_hosts
- [ ] Хранение API key DeepSeek локально

### Этап 2 — Паритет агента

- [ ] Полный toolset (search, apply_patch, multi-file edit)
- [ ] Diff preview + approve/reject
- [ ] Контекст проекта (индексация, @file/@folder)
- [ ] Несколько чатов / сессий
- [ ] OpenRouter + OpenAI + Ollama

### Этап 3 — Провайдеры и качество

- [ ] Anthropic Messages API (адаптер из внутреннего формата)
- [ ] Z.ai / GLM
- [ ] Кэш контекста (DeepSeek prompt cache)
- [ ] Политики безопасности tools (allowlist путей, confirm destructive)

### Этап 4 — Полировка продукта

- [ ] Автообновления, тема, шорткаты
- [ ] macOS notarization / Windows code signing
- [ ] Оптимизация размера бинарника

---

## 5. Провайдеры ИИ (сводка)

Подробные контракты: [`docs/exchange-protocols/`](./exchange-protocols/).

| Провайдер | Base URL | Endpoint | Формат |
|---|---|---|---|
| **DeepSeek** (этап 1) | `https://api.deepseek.com` | `POST /chat/completions` | OpenAI-совместимый |
| OpenRouter | `https://openrouter.ai/api/v1` | `POST /chat/completions` | OpenAI + extensions |
| OpenAI | `https://api.openai.com/v1` | `POST /chat/completions` | OpenAI |
| Anthropic | `https://api.anthropic.com` | `POST /v1/messages` | **свой** Messages API |
| Z.ai (GLM) | `https://open.bigmodel.cn/api/paas/v4` | `POST /chat/completions` | OpenAI-like |
| Ollama | `http://localhost:11434/v1` | `POST /chat/completions` | OpenAI-совместимый |

Внутренний канонический формат приложения — **OpenAI Chat Completions + tools**.  
Anthropic и прочие отличия конвертируются адаптерами в `internal/llm/providers`.

---

## 6. Agent loop (обязательный контракт)

1. UI отправляет user message + workspace context.
2. Go собирает `messages` + `tools` + `model`.
3. Provider отвечает текстом и/или `tool_calls`.
4. Go исполняет tools (FS / shell / git / ssh).
5. Результаты tools возвращаются ролью `tool`.
6. Цикл повторяется до `finish_reason=stop` или лимита шагов.
7. Streaming дельт уходит во фронтенд событиями Wails.

См. [`docs/exchange-protocols/tools-and-agent-loop.md`](./exchange-protocols/tools-and-agent-loop.md).

---

## 7. Инструменты агента (этап 1)

| Tool | Описание |
|---|---|
| `read_file` | Прочитать файл (path, optional offset/limit) |
| `write_file` | Создать/перезаписать файл |
| `list_dir` | Список каталога |
| `run_terminal` | Выполнить команду в shell/PowerShell workspace |
| `git_status` | Статус репозитория |
| `git_diff` | Diff (staged/unstaged/path) |
| `git_commit` | Commit через go-git |
| `git_push` | Push в remote (Gitea и др.) |
| `ssh_exec` | Команда на удалённом хосте |
| `ssh_keygen` | Генерация ключевой пары |

Все пути — относительно workspace root, с запретом выхода за sandbox (кроме явного user-approved path).

---

## 8. UI / UX требования (этап 1)

- Тёмная рабочая тема IDE (не маркетинговый лендинг).
- Projects sidebar всегда виден.
- File tree сворачиваемый.
- Chat: markdown, code blocks, карточки tool calls.
- Terminal: Xterm.js, resize, copy/paste, clear.
- Settings: DeepSeek API key, default model, shell path, Gitea remote defaults.

---

## 9. Конфигурация и секреты

Пути (черновик):

- Windows: `%APPDATA%/NotCursor/`
- macOS: `~/Library/Application Support/NotCursor/`

Файлы:

- `settings.json` — несекретные настройки
- `secrets.enc` — API keys (DPAPI на Windows / Keychain на macOS — этап 2; на этапе 1 допускается локальный encrypted file)
- `projects.json` — список проектов
- `ssh/` — ключи, созданные приложением (права 0600)

`.env` в репозиторий **не** коммитить. Пример: `.env.example`.

---

## 10. Сборка и дистрибуция

```bash
# Dev
wails dev

# Release Windows
wails build -platform windows/amd64

# Release macOS
wails build -platform darwin/universal
```

Целевой артефакт: один бинарник / `.app` без bundling Node runtime.

---

## 11. Критерии приёмки этапа 1

1. Приложение стартует на Windows через Wails.
2. Можно добавить проект (папку) и видеть дерево файлов.
3. В Settings сохраняется DeepSeek API key.
4. Чат стримит ответ от DeepSeek.
5. Агент через tools может изменить файл в проекте.
6. Терминал выполняет PowerShell-команду в корне проекта.
7. `git_commit` + `git_push` работают с Gitea remote.
8. SSH: генерация ключа + `ssh_exec` на тестовый хост.
9. Документация протоколов лежит в `/docs/exchange-protocols`.

---

## 12. Риски и решения

| Риск | Митигация |
|---|---|
| Anthropic ≠ OpenAI wire format | Внутренний OpenAI-канон + адаптер |
| PTY на Windows сложнее | `conpty` / wrapper; fallback на pipes |
| go-git ≠ 100% git CLI | Hybrid: go-git для обычных операций, shell git как fallback |
| Утечка API key | secrets store, redaction в логах |
| Размер > 20 МБ | strip, upx осторожно, без лишних CGO |

---

## 13. Глоссарий

- **Provider** — HTTP-клиент к LLM API.
- **Tool** — функция, которую модель может вызвать.
- **Agent loop** — цикл model ↔ tools до финального ответа.
- **Workspace** — корневая папка открытого проекта.
