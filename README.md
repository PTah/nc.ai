# NotCursor.ai

Лёгкий нативный AI-IDE агент (аналог Cursor) на **Wails v2 + Go + React/TypeScript**.

**Версия UI/бинаря:** `0.4.5`  
**Репозиторий:** https://git.papatramp.ru/PapaTramp/nc.ai  
**Артефакт Windows:** `build/bin/NotCursor.exe`

---

## Что уже сделано (журнал работ, черновик)

Документируем всё по ходу. После готовности продукта лишнее подчистим.

### Стек и каркас
- Wails v2: Go backend + React/TS frontend (системный WebView, без Electron)
- Layout «как Cursor»: проекты | дерево файлов (опц.) | чат | настройки (опц.) | терминал (опц.)
- Размер релиза не жёстко ограничен (сейчас порядка десятков МБ; по мере функций может расти); macOS **не** кросс-компилируется с Windows (нужен macOS-хост)
- Бренд-иконка: `build/appicon.png` + multi-size `build/windows/icon.ico` (16…256, для списка в Explorer)
- Версия продукта в свойствах exe: `wails.json` → `info.productVersion`
- Внутри приложения: анимированный mark в топбаре (`BrandMark`, кадры `frontend/public/brand/frame-1…6.png`, ~560 мс)
- **macOS Dock:** пока агент активен — цикл `frame1…frame6` через `NSApp.applicationIconImage` (`internal/dockicon`, interval 600 мс); по стопу возвращается default. На Windows — no-op stub
- Пересборка ассетов: `python scripts/build_brand_assets.py`

### Окно, layout, тема
- Геометрия главного окна (размер, позиция, maximised) сохраняется при закрытии → `%APPDATA%/NotCursor/settings.json`
- Ширины панелей (projects / files / settings) и высота терминала — ресайз за край + persist
- Темы **Dark** / **Light** (светлая в духе Cursor); выбор в Settings → Interface

### DeepSeek / agent loop
- Провайдер: OpenAI-compatible `POST https://api.deepseek.com/chat/completions`
- Модели: `deepseek-v4-flash`, `deepseek-v4-pro`, vision `deepseek-v4-flash-vision-exp`
- Thinking Mode + Tool Calls по протоколу `docs/exchange-protocols/deepseek.md`:
  - assistant message пишется as-is (`content` + `reasoning_content` + `tool_calls`)
  - цикл продолжается, пока есть `tool_calls` (не выходим на `finish_reason=stop`, если tools есть)
  - `thinking: enabled`, `reasoning_effort: high`
- Лимит шагов агента: **40**; при упоре — soft wrap-up: финальный ответ **без tools**, без дубля ошибки в UI

### Tools
| Tool | Назначение |
|---|---|
| `read_file` / `write_file` | чтение/запись в sandbox workspace |
| `list_dir` / `search_files` | обход и поиск |
| `run_terminal` | скрытый PowerShell (`CREATE_NO_WINDOW`), UTF-8 / CP866 |
| `git_*` | status / diff / commit / push через **системный `git`** (credential helper / GCM / SSH) |
| `ssh_*` | exec + keygen по ключам из **`~/.ssh`** (как в Cursor; пароли в приложении не хранятся) |

- `read_file` при not-found подсказывает соседние файлы (siblings)
- Пути песочницы относительно корня проекта

### Чат UX
- Thinking-блоки, карточки tools, группировка завершённых tools (`Explored · N files read…`)
- Скролл треда, Clear / Stop
- Персистентность чата на проект в `%APPDATA%/NotCursor/chats/`
- Настройки UI: Hide files / Hide terminal / Hide settings — сохраняются
- После финального ответа Thinking и tools сжимаются (closed summary / Explored), как в Cursor
- Счётчики $: **чат** · **всего** (live во время run + persist; per-chat в session JSON)

### Multitasking (вкладки чата)
- Несколько независимых сессий на проект (`internal/chatstore`)
- UI: вкладки `+` / `×`, отдельные истории и busy-статус
- События агента помечены `sessionId` — параллельные прогоны не смешивают ответы
- Миграция со старого single-chat JSON → multi-session bundle (**с записью на диск** и стабильным session id)

### Вложения и vision (скриншоты / файлы)
- **Ctrl+V**, drag-drop, кнопка **Attach**
- Картинки → `data:` URL → multimodal `image_url` → автопереключение на vision-модель
- Текстовые файлы → inline в user message
- Бинарники → подсказка положить в workspace и читать tools

### Прочее
- `internal/costing` — USD из `usage` DeepSeek: cache hit/miss + completion, peak/off-peak (Beijing)
- Shell/терминал скрыт по умолчанию; Xterm стартует только при показе панели
- API key локально в AppData, не в репозитории; Git/SSH — через ОС, не через Settings

---

## Как «видит» скриншоты агент

1. UI читает файл/clipboard → `data:image/png;base64,...`
2. Backend собирает user message:
   ```json
   [
     {"type": "text", "text": "…"},
     {"type": "image_url", "image_url": {"url": "data:…", "detail": "original"}}
   ]
   ```
3. Запрос уходит в `deepseek-v4-flash-vision-exp`
4. Дальше обычный tool loop (файлы, shell, git…)

---

## Документация

- [ТЗ](docs/TZ.md)
- [Протоколы AI](docs/exchange-protocols/README.md) (DeepSeek, tools-and-agent-loop, …)

---

## Dev

```bash
cd frontend && npm install && cd ..
wails dev
```

## Build

```bash
# Windows (на этой машине)
wails build -platform windows/amd64

# macOS — только на macOS-хосте
wails build -platform darwin/universal
# или:
wails build -platform darwin/arm64
wails build -platform darwin/amd64
```

Артефакты: `build/bin/`  
Windows: `build/bin/NotCursor.exe`.

---

## Settings (UI)

- DeepSeek API key, model (в т.ч. vision)
- Theme: Dark / Light
- Показ терминала / дерева файлов / панели Settings
- Git & SSH: **без логинов в UI** — системный `git` + `~/.ssh` / ssh-agent (подсказка в панели)

Данные: `%APPDATA%/NotCursor/` (не коммитить).

---

## Структура (актуально)

```
app.go                 # Wails façade / bindings
main.go                # окно: размер/позиция/maximised из settings
frontend/src/          # React UI (чат, вкладки, вложения, темы)
internal/agent/        # agent loop + attachments + wrap-up
internal/llm/          # типы, multimodal JSON, DeepSeek client
internal/tools/        # registry + executor
internal/workspace/    # проекты, FS sandbox
internal/chatstore/    # multi-session persistence
internal/shell/        # PowerShell / oneshot
internal/gitx/         # OS git (status/diff/commit/push)
internal/sshx/         # SSH (~/.ssh)
internal/config/       # settings store (окно, layout, theme, API)
internal/costing/      # token → USD
docs/                  # ТЗ + протоколы
```

---

## TODO / дальше (черновик)

- [ ] Streaming SSE ответов
- [ ] Diff viewer / встроенный редактор
- [ ] macOS `.app` на mac-хосте
- [ ] Больше провайдеров LLM
- [ ] После стабилизации — вычистить устаревшие заметки из README
