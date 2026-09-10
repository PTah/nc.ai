# NotCursor.ai

Лёгкий нативный AI-IDE агент (аналог Cursor) на **Wails v2 + Go + React/TypeScript**.

**Версия UI/бинаря:** `0.6.2`

**Сборка Windows:** `build/bin/NotCursor.exe`

---

## Что умеет

### Провайдеры LLM

Переключение в **Settings → Provider**:

| Провайдер | Модели | Особенности |
|---|---|---|
| **DeepSeek** | `deepseek-v4-flash`, vision `deepseek-v4-flash-vision-exp` (до 14.09.2026 12:00 Beijing — ещё `deepseek-v4-pro`) | OpenAI-compatible API |
| **Z.ai** | `glm-4.5…5.3` (flash / pro / vision) | свободные flash-модели, живой список `/models`, проверка баланса |
| **OpenRouter** | произвольный `openrouter/…` | свой ключ/endpoint |

- Автоматический выбор модели: flash → pro → vision (по вложениям/шагам). После 14.09.2026 12:00 Beijing `v4-pro` выводится из списка, авто-выбор и расчёты идут по Flash.
- HTTP-ошибки провайдеров переводятся в человекочитаемые сообщения (402 → «Пополните баланс…», 429 → «лимит запросов…»).
- Для Z.ai: **баланс** и список моделей показываются в Settings.

### Agent loop

- Tool Calls + Thinking Mode по протоколам в `docs/exchange-protocols/`.
- Лимит шагов агента настраивается (**Settings → Agent**, по умолчанию 40); при упоре — soft wrap-up без tools.
- **Reconnect**: сетевые сбои, rate-limit (429/1302/1305), «провайдер недоступен» — автоматические повторы с backoff; после исчерпания — кнопка **Reconnect**.
- **Компакция истории**: старые tool-результаты сворачиваются (`[compacted] …`), последние 3 остаются полными — контекст не растёт бесконечно. Compact выполняется между ходами агента.
- **Prefix-cache LLM**: стабильные system/turn-rules для попадания в кэш; hit/miss % показывается в шапке чата (chat · total).
- **Очередь сообщений**: отправка в чат во время активного прогона ставит сообщение в очередь и отправляет после завершения текущего.

### Tools

| Tool | Назначение |
|---|---|
| `read_file` / `write_file` / `apply_patch` / `delete_file` / `move_file` | чтение (с номерами строк) / запись / патч / удаление / rename в sandbox |
| `list_dir` / `find_files` / `glob` / `grep` | обход, нечёткое имя, glob (`**/*.go`), содержимое (regex как ripgrep) |
| `run_terminal` / `command_status` | shell; `cwd`, фон (`is_background`) |
| `todo_write` / `ask_user` / `read_lints` | список задач, вопрос пользователю, `go vet` |
| `web_search` / `fetch_url` | веб (SSRF-guard: без LAN/loopback/metadata) |
| `git_*` | status / diff / log / commit (`paths`) / push через **системный `git`** |
| `ssh_*` | exec + keygen по ключам из **`~/.ssh`** |

- `read_file` при not-found подсказывает соседние файлы (siblings).
- Пути песочницы относительно корня проекта.

### Чат UX

- **Plan / Act** в шапке чата: Plan — только исследование и план, без правок и shell.
- Клик по файлу в дереве открывает **редактор** (не дамп в чат); курсор уходит в IDE-контекст агента.
- **Todo-список** агента над тредом, когда вызван `todo_write`.
- Thinking-блоки и завершённые tools после финального ответа группируются в один блок **Thinking & Explored**.
- **Вкладки чатов**: `+` создаёт новую сессию, старые сохраняются; переименование по двойному клику.
- **Archive**: кнопка в шапке чата переносит чат в `%APPDATA%/NotCursor/chat_archive/<имя>.json`.
- **Архивы чатов**: список сохранённых сессий открывается из UI; архивный чат читается в режиме просмотра (read-only) и не попадает в активные.
- Персистентность на проект в `%APPDATA%/NotCursor/chats/`.
- Счётчики $: чат · всего + cache hit/miss (peak/off-peak учитывается корректно; прайс DeepSeek обновляется автоматически).
- Проверка прайсов: DeepSeek и OpenRouter — ежедневно после 13:05 Beijing, z.ai — еженедельно; результат появляется в чате как системное сообщение (`Система: проверка цен … выполнена — …`).

### Cursor Rules

Подгружаются и добавляются в системный промпт:

- глобально: `~/.cursor/rules/*.mdc|md` (рекурсивно), `~/.cursorrules`, `AGENTS.md`;
- на проект: `<project>/.cursor/rules/*` (рекурсивно), `<project>/.cursorrules`.

В **Settings → Cursor Rules** виден список загруженных правил и кнопка **Reload**.

### Окно / layout / тема

- Геометрия окна, ширины панелей, высота терминала и **высота окна ввода** — сохраняются.
- Ресайз: projects / files / settings / терминал / поле ввода.
- Темы **Dark** / **Light**.
- Горячие клавиши: **Ctrl+Alt+T** — показать/скрыть терминал, **Ctrl+Shift+S** — открыть/закрыть Settings.

### Безопасность

- **Секреты не уходят в LLM**: `.env*`, `id_rsa`, `*.pem`, `*.key`, `*secret*` и т.п. при `read_file` заменяются заглушкой; значения `KEY=…`/`TOKEN=…` маскируются в `***`.
- SSH: проверка по `~/.ssh/known_hosts` + TOFU (изменение ключа = отказ).
- `git commit` стейджит только tracked-изменения (`git add -u`).
- JSON-файлы чатов/настроек пишутся атомарно (temp + rename) с ротационным `.bak`.

---

## Скриншоты → агент

1. UI читает файл/clipboard → `data:image/png;base64,…`.
2. Backend собирает user message с `image_url`.
3. Запрос уходит в vision-модель.
4. Дальше обычный tool loop.

---

## Документация

- [ТЗ](docs/TZ.md)
- [Архитектура](docs/architecture/overview.md)
- [Протоколы AI](docs/exchange-protocols/README.md)

---

## Dev

```bash
cd frontend && npm install && cd ..
wails dev
```

## Build

```powershell
# Windows (перезапускает NotCursor.exe после сборки):
.\build.ps1
# только сборка:
.\build.ps1 -NoRestart
```

```bash
# macOS — только на macOS-хосте (перезапускает .app после сборки):
./build.sh
# только сборка:
./build.sh --no-restart
# fat binary:
./build.sh --universal
```

Артефакты: `build/bin/`.
Windows: `build/bin/NotCursor.exe`. macOS: `build/bin/NotCursor.app`.

## Commit + push

```powershell
.\commit.ps1 "feat: описание изменений"
```

```bash
./commit.sh "feat: описание изменений"
```

---

## Settings (UI)

- **Provider**: DeepSeek / Z.ai / OpenRouter (+ ключ, модель, vision-модель).
- **Agent**: лимит шагов, Plan mode, confirm dangerous tools.
- **Interface**: тема, показ терминала / дерева / настроек.
- **Cursor Rules**: список + reload.
- **Git & SSH**: без логинов в UI — системный `git` + `~/.ssh`.

Данные: `%APPDATA%/NotCursor/` (не коммитить).

---

## Структура

```
app.go                 # Wails façade / bindings
main.go                # окно: размер/позиция/maximised
frontend/src/          # React UI (чат, вкладки, вложения, темы)
internal/agent/        # agent loop + retry + компакция + plan
internal/llm/          # типы, общий OpenAI-compat слой, providers/deepseek, providers/zai
internal/tools/        # registry + executor
internal/netx/         # SSRF-safe HTTP
internal/redact/       # маскирование секретов в выводе tools
internal/workspace/    # проекты, FS sandbox, guard секретов
internal/workspace/    # проекты, FS sandbox, guard секретов
internal/chatstore/    # multi-session persistence + archive
internal/rules/        # загрузчик Cursor rules
internal/shell/        # PowerShell / oneshot
internal/gitx/         # OS git (status/diff/commit/push)
internal/sshx/         # SSH (~/.ssh, known_hosts)
internal/config/       # settings store (окно, layout, тема, провайдеры)
internal/costing/      # token → USD (peak/off-peak)
internal/fsx/          # атомарная запись файлов
docs/                  # ТЗ + архитектура + протоколы
```

---

## TODO / дальше

- [ ] Streaming SSE ответов
- [ ] Полноценный diff viewer / LSP (сейчас простой редактор + `go vet`)
- [ ] macOS `.app` на mac-хосте
- [ ] Разбиение `frontend/src/App.tsx` на хуки
