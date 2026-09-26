# Техническое задание: NotCursor.ai

**Версия:** 0.1.0 (документ) · **состояние продукта:** 0.7.1
**Дата:** 2026-09-06 · **обновлено:** 2026-09-25
**Репозиторий:** https://github.com/PTah/nc.ai (публичное зеркало; приватные зеркала — в `git remote -v`)
**Статус:** этапы 1–4 закрыты (кроме подписей и macOS-артефактов), этап 5 не начат.
Сводка «что сделано» — §4, актуальные планы — [`docs/roadmap.md`](roadmap.md).

Легенда статусов: `[x]` сделано · `[~]` частично · `[ ]` не сделано.

---

## 1. Цель продукта

**NotCursor.ai** — лёгкий нативный IDE-агент (аналог Cursor), который:

- запускается за доли секунды;
- лёгкий релизный бинарь (порядок десятков МБ; размер может расти с функционалом);
- компилируется в **Windows `.exe`** и **macOS `.app`**;
- даёт UI «как в Cursor»: проекты, дерево файлов, чат с ИИ-агентом, терминал;
- выполняет действия агента локально: файлы, PowerShell/shell, Git/Gitea, SSH.

Стек: **Go (бэкенд)** + **React/TypeScript + Tailwind (фронтенд)** через **Wails v2** (системный WebView вместо Chromium).

---

## 2. Нефункциональные требования

| Требование | Целевое значение |
|---|---|
| Холодный старт | < 500 мс на типичном SSD (Windows/macOS) |
| Размер релизного бинарника | ориентир — десятки МБ без встроенных моделей; жёсткого потолка нет |
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
| `app.go`, `main.go` | Wails App, lifecycle, bindings |
| `internal/workspace` | проекты, дерево, поиск, иконки |
| `internal/tools` | реестр инструментов, executor, фоновые jobs, patch, todos, lints |
| `internal/shell` | PowerShell / bash / cmd через PTY |
| `internal/gitx` | git через системный git CLI (hide-window на Windows) |
| `internal/sshx` | SSH-сессии, ключи (`golang.org/x/crypto/ssh`) |
| `internal/llm` | канон (OpenAI Chat Completions + tools), SSE-хелперы, лимиты |
| `internal/llm/providers` | `deepseek`, `zai`, `openrouter`, `qwen`, `local` (OpenAI-совместимые), `anthropic` |
| `internal/agent` | agent loop, авто-роутинг модели, compact, stall-watchdog, todo-nudge |
| `internal/config` | settings.json, профили эндпоинтов (протокол, reasoning, num_ctx) |
| `internal/secrets` | Credential Manager / Keychain + file fallback |
| `internal/chatstore` | сессии чатов, архив, проектные бандлы, миграция |
| `internal/costing` | каталог цен, refresh у провайдеров, peak info |
| `internal/update` | автообновление с GitHub (exe + sha256) |
| `internal/rules` | `.cursorrules` / `.cursor/rules`, turn-rules |
| `internal/netx` | SSRF-защита, `fetch_url` |
| `internal/appmeta`, `dockicon`, `chime`, `redact`, `fsx`, `providernews` | метаданные, иконка в доке, звук, редакция секретов, атомарная запись, новости провайдеров |

Исходная таблица (этап 1) упоминала `internal/fsops`, `internal/gitea`, `go-git` — по факту
файловые операции живут в `internal/tools`, git — через системный `git` в `internal/gitx`,
а Gitea — как обычный git-remote (HTTP API не понадобился).

---

## 4. Этапы разработки

### Этап 1 — MVP «как Cursor» + DeepSeek — **закрыт**

- [x] ТЗ и документация протоколов (`/docs`)
- [x] Каркас Wails (Go + React TS + Tailwind)
- [x] Layout: projects / file tree / chat / terminal
- [x] DeepSeek provider: `POST https://api.deepseek.com/chat/completions`
- [x] Streaming SSE в UI — текстовые дельты и thinking идут в ленту; стримят все
  провайдеры: DeepSeek, Z.ai, Qwen, OpenRouter, Anthropic и Local / custom
- [x] Function calling / tools: read/write file, list dir, run shell
- [x] Локальный shell (PowerShell на Windows)
- [x] git: status, diff, commit, push (системный `git`, не go-git)
- [x] SSH: connect, exec, keypair, known_hosts
- [x] Хранение API key локально (Credential Manager / Keychain)

### Этап 2 — Паритет агента — **закрыт**

- [x] Полный toolset (search, apply_patch, multi-file edit, glob/grep/find_files, run jobs, web)
- [~] Diff preview + approve/reject — подтверждение вызовов (`toolConfirm`, allow/deny) есть,
  отдельного diff-вьюера нет
- [x] Контекст проекта (карта проекта, `@file`-хинты, поиск по дереву)
- [x] Несколько чатов / сессий (сессии, архив, переименование)
- [x] OpenRouter + OpenAI-совместимые + Ollama (профили Local / custom)

### Этап 3 — Провайдеры и качество — **закрыт**

- [x] Anthropic Messages API (адаптер в `internal/llm/providers/anthropic`, профиль `protocol: anthropic`)
- [x] Z.ai / GLM
- [~] Кэш контекста DeepSeek — `prompt_cache_hit/miss` учитываются в usage и цене,
  управление кэшем (стабильный префикс промпта) не делали
- [x] Политики безопасности tools (sandbox путей, подтверждение разрушительных вызовов)

### Этап 4 — Полировка продукта — **закрыт, кроме подписей**

- [x] Автообновления (GitHub releases + sha256), тема и шрифты, шорткаты
- [ ] macOS notarization / Windows code signing
- [x] Оптимизация размера бинарника (релизный exe ≈ 12 МБ, WebView системный)

### Этап 5 — Субагенты (вложенные прогоны) — **не начат**

- [ ] Планировка — [`docs/roadmap.md`](roadmap.md)
- [ ] Инструмент `task` + вложенный прогон (read-only, последовательно)
- [ ] События и карточка подагента в ленте
- [ ] Конфигурация, лимиты шагов, отмена вместе с родителем
- [ ] Выбор модели для подагента (Auto/Lite)

### Что осталось за рамками этапов

Порядок и состав следующих шагов зафиксированы в [`docs/roadmap.md`](roadmap.md)
(раздел «Ближайшие шаги»).

- **MCP** — [`docs/mcp.md`](mcp.md) пока черновик дизайна, кода нет.
- **Провайдер и модель на каждый чат** — сейчас провайдер один на приложение.
- **macOS-артефакты релиза** — 0.7.0–0.7.2 опубликованы без `macos-arm64` /
  `macos-universal` (собираются только на macOS-хосте).
- **diff viewer / LSP**, разбор `frontend/src/App.tsx` на хуки.

---

## 5. Провайдеры ИИ (сводка)

Подробные контракты: [`docs/exchange-protocols/`](./exchange-protocols/).

| Провайдер | Base URL | Endpoint | Формат | Статус |
|---|---|---|---|---|
| **DeepSeek** | `https://api.deepseek.com` | `POST /chat/completions` | OpenAI-совместимый | работает, стримит |
| Z.ai (GLM) | `https://open.bigmodel.cn/api/paas/v4` | `POST /chat/completions` | OpenAI-like | работает, стримит |
| OpenRouter | `https://openrouter.ai/api/v1` | `POST /chat/completions` | OpenAI + extensions | работает, стримит |
| Qwen (DashScope) | `https://dashscope…/compatible-mode/v1` | `POST /chat/completions` | OpenAI-совместимый | работает, стримит |
| Local / custom | любой OpenAI-совместимый хост (`http://127.0.0.1:11434/v1`, LAN, публичные роутеры бесплатных тарифов) | `POST /chat/completions` | OpenAI-совместимый | работает, стримит |
| Anthropic | `https://api.anthropic.com` или роутер (Selora, Atria) | `POST /v1/messages` | **свой** Messages API (профиль `protocol: anthropic`) | работает, стримит |
| OpenAI | `https://api.openai.com/v1` | `POST /chat/completions` | OpenAI | работает как профиль Local / custom |

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

## 7. Инструменты агента (фактический состав)

| Tool | Описание |
|---|---|
| `read_file` / `write_file` | чтение и запись файла (offset/limit при чтении) |
| `list_dir` / `glob` / `find_files` / `grep` | навигация по дереву и поиск (по имени и по содержимому) |
| `apply_patch` / `delete_file` / `move_file` | точечные правки, удаление, переименование |
| `run_terminal` / `command_status` | команда в shell/PowerShell workspace; фоновые jobs и опрос |
| `get_env_info` | ОС, arch, shell, версии go/node/python (запускается скрыто) |
| `read_lints` | `go vet` / диагностика по изменённым файлам |
| `todo_write` | план прогона (ToDo-панель в ленте) |
| `ask_user` / `web_search` / `fetch_url` | вопрос пользователю, поиск в вебе, чтение URL (с SSRF-защитой) |
| `git_status` / `git_diff` / `git_log` | состояние репозитория |
| `git_commit` / `git_push` | коммит (только указанные файлы) и push в remote |
| `ssh_exec` / `ssh_keygen` | команда на удалённом хосте, генерация ключа |

Все пути — относительно workspace root, с запретом выхода за sandbox; разрушительные вызовы
можно подтверждать вручную (`toolConfirm` в настройках).

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

Этап 1 принят начиная с 0.6.x — все пункты ниже выполняются в 0.7.1:

1. Приложение стартует на Windows через Wails. ✅
2. Можно добавить проект (папку) и видеть дерево файлов. ✅
3. В Settings сохраняется DeepSeek API key. ✅
4. Чат стримит ответ от DeepSeek. ✅
5. Агент через tools может изменить файл в проекте. ✅
6. Терминал выполняет PowerShell-команду в корне проекта. ✅
7. `git_commit` + `git_push` работают с Gitea remote. ✅
8. SSH: генерация ключа + `ssh_exec` на тестовый хост. ✅
9. Документация протоколов лежит в `/docs/exchange-protocols`. ✅

---

## 12. Риски и решения

| Риск | Митигация | Состояние |
|---|---|---|
| Anthropic ≠ OpenAI wire format | Внутренний OpenAI-канон + адаптер | закрыто: `internal/llm/providers/anthropic` |
| PTY на Windows сложнее | `conpty` / wrapper; fallback на pipes | закрыто (терминал работает) |
| go-git ≠ 100% git CLI | Hybrid: go-git для обычных операций, shell git как fallback | закрыто иначе: везде системный `git` (`internal/gitx`) |
| Утечка API key | secrets store, redaction в логах | закрыто (Credential Manager / Keychain, `internal/redact`) |
| Размер сильно вырос | strip, без лишних CGO; UPX — осторожно | держим: релизный exe ≈ 12 МБ |
| Лимиты бесплатных тарифов (5 RPM / 200 RPD) | собственный разбор `429` + ожидание | закрыто: `internal/llm/ratelimit.go` |

---

## 13. Глоссарий

- **Provider** — HTTP-клиент к LLM API.
- **Tool** — функция, которую модель может вызвать.
- **Agent loop** — цикл model ↔ tools до финального ответа.
- **Workspace** — корневая папка открытого проекта.
