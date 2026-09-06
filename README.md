# NotCursor.ai

Лёгкий нативный AI-IDE агент (аналог Cursor) на **Wails v2 + Go + React/TypeScript**.

**Версия UI/бинаря:** `0.1.7`  
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
- Версия продукта в свойствах exe: `wails.json` → `info.productVersion` (сейчас `0.1.7`)
- Внутри приложения: анимированный mark в топбаре (`BrandMark`, кадры `frontend/public/brand/frame-1…6.png`, 300 мс)
- **macOS Dock:** пока агент активен — цикл `frame1…frame6` через `NSApp.applicationIconImage` (`internal/dockicon`, interval 300 мс); по стопу возвращается default. На Windows — no-op stub
- Пересборка ассетов: `python scripts/build_brand_assets.py`

### DeepSeek / agent loop
- Провайдер: OpenAI-compatible `POST https://api.deepseek.com/chat/completions`
- Модели: `deepseek-v4-flash`, `deepseek-v4-pro`, vision `deepseek-v4-flash-vision-exp`
- Thinking Mode + Tool Calls по протоколу `docs/exchange-protocols/deepseek.md`:
  - assistant message пишется as-is (`content` + `reasoning_content` + `tool_calls`)
  - цикл продолжается, пока есть `tool_calls` (не выходим на `finish_reason=stop`, если tools есть)
  - `thinking: enabled`, `reasoning_effort: high`
- Лимит шагов агента: **40** (раньше 12); при упоре — soft wrap-up: финальный ответ **без tools**, без дубля ошибки в UI

### Tools
| Tool | Назначение |
|---|---|
| `read_file` / `write_file` | чтение/запись в sandbox workspace |
| `list_dir` / `search_files` | обход и поиск |
| `run_terminal` | скрытый PowerShell (`CREATE_NO_WINDOW`), UTF-8 / CP866 |
| `git_*` | status / diff / commit / push (go-git + credentials) |
| `ssh_*` | exec + keygen |

- `read_file` при not-found подсказывает соседние файлы (siblings)
- Пути песочницы относительно корня проекта

### Чат UX
- Thinking-блоки, карточки tools, группировка завершённых tools (`Explored · N files read…`)
- Скролл треда, Clear / Stop
- Персистентность чата на проект в `%APPDATA%/NotCursor/chats/`
- Настройки UI: Hide files / Hide terminal / Hide settings — сохраняются

### Multitasking (вкладки чата)
- Несколько независимых сессий на проект (`internal/chatstore`)
- UI: вкладки `+` / `×`, отдельные истории и busy-статус
- События агента помечены `sessionId` — параллельные прогоны не смешивают ответы
- Миграция со старого single-chat JSON → multi-session bundle (**с записью на диск** и стабильным session id; иначе List/Get расходились и UI показывал пустой чат)

### Вложения и vision (скриншоты / файлы)
- **Ctrl+V**, drag-drop, кнопка **Attach**
- Картинки → `data:` URL → multimodal `image_url` → автопереключение на vision-модель
- Текстовые файлы → inline в user message
- Бинарники → подсказка положить в workspace и читать tools
- Как это работает: модель «видит» пиксели через multimodal content, не через отдельный OCR

### Прочее в кодовой базе
- `internal/costing` — расчёт $ по usage DeepSeek (заготовка под счётчик стоимости в UI, ещё не в шапке)
- Shell/терминал скрыт по умолчанию; Xterm стартует только при показе панели
- API key / git / SSH локально в AppData, не в репозитории

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
- Git username / password|token (Gitea)
- SSH keygen + список ключей
- Показ терминала / дерева файлов

Ключи: `%APPDATA%/NotCursor/` (не коммитить).

---

## Структура (актуально)

```
app.go                 # Wails façade / bindings
frontend/src/          # React UI (чат, вкладки, вложения)
internal/agent/        # agent loop + attachments + wrap-up
internal/llm/          # типы, multimodal JSON, DeepSeek client
internal/tools/        # registry + executor
internal/workspace/    # проекты, FS sandbox
internal/chatstore/    # multi-session persistence
internal/shell/        # PowerShell / oneshot
internal/sshx/         # SSH
internal/config/       # settings store
internal/costing/      # token → USD (WIP UI)
docs/                  # ТЗ + протоколы
```

---

## TODO / дальше (черновик)

- [ ] Счётчик стоимости в UI (`internal/costing`)
- [ ] Streaming SSE ответов
- [ ] Diff viewer / встроенный редактор
- [ ] macOS `.app` на mac-хосте
- [ ] Больше провайдеров LLM
- [ ] После стабилизации — вычистить устаревшие заметки из README
