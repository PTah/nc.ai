# ToDo: secrets hardening (Keychain + redact + revoke)

**Только private remotes (`home`, `kalinamall`). На `github` эти ToDo не пушим.**

Статус: **этап 1 сделан (v0.6.3)** — API-ключи в OS Secret Store; plaintext убран из `settings.json`. Этапы 2–4 ниже по-прежнему открыты.

Модель угроз NotCursor: украли `settings.json` / бэкап диска / утечка ключа в чат-лог / XSS в WebView. Мы **не** строим свой OpenID‑провайдер → BFF/DPoP/backchannel logout не тащим.

## Уже есть (не ломать)

- UI видит только `*KeySet`, не сам ключ
- `IsSecretPath` / `MaskSecrets` для workspace-файлов
- `internal/redact` на tool-результаты и agent events
- `fetch_url` / `web_search` — SSRF-guard (`internal/netx`)
- HITL (`ToolConfirm`) на опасные tools
- Git без хранения паролей в приложении (OS credential helper / `~/.ssh`)
- **v0.6.3:** `internal/secrets` — macOS Keychain / Windows Credential Manager / файл 0600 на Linux; миграция ключей из JSON при старте; Clear key / Clear all keys

## Слабое место (закрыто на этапе 1)

`DeepSeekAPIKey` / `ZaiAPIKey` / `OpenRouterAPIKey` больше не пишутся в `settings.json`. `GitPassword` при загрузке вычищается и не кладётся в Keychain (git — только OS helper).

---

## Этап 1 — «не дать украсть» — сделано

OS Secret Store вместо plaintext.

Пакет:

```
internal/secrets/
  store.go             // Get/Set/Delete; OS + file fallback
  keychain_darwin.go   // macOS Keychain (`security`)
  credman_windows.go   // Windows Credential Manager
  file_fallback.go     // Linux / fallback (0600)
```

- В `settings.json` — только флаги / метаданные, **без** значений ключей (`MarshalJSON` затирает поля)
- Миграция при старте: ключ из JSON → Secret Store → вычистить из JSON
- UI: Save / **Clear key** / **Clear all keys** / никогда не показывать сохранённый ключ обратно

## Этап 2 — «украденное бесполезно / меньше радиус»

Единый `internal/redact` (Bearer, `sk-…`, `api_key=…`) в:

- agent events (`error` / `delta` / `tool_end`) — частично уже есть
- chat archive / persist
- terminal spool, если туда попадает env

Дополнительно:

- Запрет tool `read_file` на AppData NotCursor / `settings.json`
- HTTP-клиенты: в сообщениях об ошибках **не** светить request headers / Authorization

DPoP для DeepSeek/Z.ai/OpenRouter **не делаем** — провайдеры не поддержат.

## Этап 3 — «погасить быстро»

- Кнопка **Clear all provider keys** — **сделано**
- После Clear — сброс in-memory LLM clients (`refreshProvider`) — **сделано**
- Подсказка: смените ключ на сайте провайдера (+ ссылки)
- Опционально: предупреждение, если ключ не использовался N дней

## Этап 4 — hardening агента

- Расширить HITL при необходимости (`ssh_exec`, массовый write, `git_push` — частично уже есть)
- Deny by default путей вне workspace
- SSRF-guard, если появится `fetch_url` / webhook tool (loopback, RFC1918, `169.254.169.254`) — уже есть для fetch/search

## Не делаем

- Свой BFF / OIDC / DPoP-провайдер
- Argon2 «для пользователей» (нет своей user DB)
- Шифрование всего `settings.json` одним паролем приложения (плохой UX, ключ всё равно в RAM)

## Связанное

- HITL / tools: `internal/tools`, `ToolConfirm` в config
- Маскирование файлов: `internal/workspace/secrets.go`
- Follow-ups агента: `docs/TODO-agent-followups.md`
- Прочие ToDo (private): `docs/TODO-cache-phase5.md`, `docs/TODO-rag-semantic-index.md`
