# NotCursor.ai

Лёгкий нативный AI-IDE агент (аналог Cursor) на **Wails + Go + React/TypeScript**.

- Agent loop + DeepSeek (function calling / tools)
- Файлы, редактор, дерево проекта
- PowerShell / shell + Xterm.js
- Git / Gitea (go-git)
- SSH exec + keygen
- Целевой размер релиза ~15–20 МБ

## Документация

- [ТЗ](docs/TZ.md)
- [Протоколы AI](docs/exchange-protocols/README.md)

## Dev

```bash
cd frontend && npm install && cd ..
wails dev
```

## Build

```bash
# Windows (на этой машине)
wails build -platform windows/amd64

# macOS — только на macOS-хосте (cross-compile из Windows Wails не поддерживает)
wails build -platform darwin/universal
# или:
wails build -platform darwin/arm64
wails build -platform darwin/amd64
```

Артефакты: `build/bin/`  
Текущий Windows-релиз: `build/bin/NotCursor.exe` (~16 МБ).

## Settings

В UI: DeepSeek API key, model, Git credentials, SSH keys.  
Ключи хранятся локально в `%APPDATA%/NotCursor/` (не в репозитории).

## Репозиторий

https://git.papatramp.ru/PapaTramp/nc.ai
