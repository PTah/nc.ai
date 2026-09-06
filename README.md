# NotCursor.ai

Лёгкий нативный AI-IDE агент (аналог Cursor) на **Wails + Go + React/TypeScript**.

- Быстрый старт, тонкий бинарник (~15–20 МБ)
- Windows `.exe` / macOS `.app`
- Агент: файлы, PowerShell, Git/Gitea, SSH
- Первый LLM-провайдер: **DeepSeek**

## Документация

- [Техническое задание](docs/TZ.md)
- [Протоколы обмена с AI](docs/exchange-protocols/README.md)

## Стек

| Слой | Технология |
|---|---|
| Shell app | Wails v2 |
| Backend | Go 1.22+ |
| Frontend | React + TypeScript + Vite + Tailwind |
| Terminal UI | Xterm.js |
| Git | go-git |
| SSH | golang.org/x/crypto/ssh |

## Dev

Требования: Go, Node.js, Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

```bash
# frontend deps
cd frontend && npm install && cd ..

# hot reload
wails dev

# release
wails build
```

## Конфигурация

Скопируйте `.env.example` → локальные secrets / Settings UI.  
API keys **не коммитить**.

## Репозиторий

https://git.papatramp.ru/PapaTramp/nc.ai
