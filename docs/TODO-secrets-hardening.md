# ToDo: secrets hardening (Keychain + redact + revoke)

**Только private remotes (`home`, `kalinamall`). На `github` эти ToDo не пушим.**

Статус: **запланировано** (по мотивам [Habr / redb.Identity — три слоя защиты](https://habr.com/ru/articles/1079978/), адаптировано под desktop Wails, не под OIDC).

Модель угроз NotCursor: украли `settings.json` / бэкап диска / утечка ключа в чат-лог / XSS в WebView. Мы **не** строим свой OpenID‑провайдер → BFF/DPoP/backchannel logout не тащим.

## Уже есть (не ломать)

- UI видит только `*KeySet`, не сам ключ
- `IsSecretPath` / `MaskSecrets` для workspace-файлов
- `internal/redact` на tool-результаты и agent events
- `fetch_url` / `web_search` — SSRF-guard (`internal/netx`)
- HITL (`ToolConfirm`) на опасные tools
- Git без хранения паролей в приложении (OS credential helper / `~/.ssh`)

## Слабое место сейчас

`DeepSeekAPIKey` / `ZaiAPIKey` / `OpenRouterAPIKey` (и при наличии `GitPassword`) — **plaintext в `settings.json`**.

---

## Этап 1 — «не дать украсть» (главный win)

OS Secret Store вместо plaintext.

Предлагаемый пакет:

```
internal/secrets/
  store.go             // Get/Set/Delete(provider)
  keychain_darwin.go   // macOS Keychain
  credman_windows.go   // Windows Credential Manager
  file_fallback.go     // Linux / fallback (0600), только если нет keyring
```

- В `settings.json` — только флаги / метаданные, **без** значений ключей
- Миграция при старте: ключ из JSON → Secret Store → вычистить из JSON
- UI: Save / **Clear key** / никогда не показывать сохранённый ключ обратно
- То же для оставшихся секретов (`GitPassword`, если ещё пишется)

## Этап 2 — «украденное бесполезно / меньше радиус»

Единый `internal/redact` (Bearer, `sk-…`, `api_key=…`) в:

- agent events (`error` / `delta` / `tool_end`)
- chat archive / persist
- terminal spool, если туда попадает env

Дополнительно:

- Запрет tool `read_file` на AppData NotCursor / `settings.json`
- HTTP-клиенты: в сообщениях об ошибках **не** светить request headers / Authorization

DPoP для DeepSeek/Z.ai/OpenRouter **не делаем** — провайдеры не поддержат.

## Этап 3 — «погасить быстро»

- Кнопка **Clear all provider keys**
- После Clear — сброс in-memory LLM clients (`refreshProvider`)
- Подсказка: смените ключ на сайте провайдера (+ ссылки)
- Опционально: предупреждение, если ключ не использовался N дней

## Этап 4 — hardening агента

- Расширить HITL при необходимости (`ssh_exec`, массовый write, `git_push` — частично уже есть)
- Deny by default путей вне workspace
- SSRF-guard, если появится `fetch_url` / webhook tool (loopback, RFC1918, `169.254.169.254`)

## Не делаем

- Свой BFF / OIDC / DPoP-провайдер
- Argon2 «для пользователей» (нет своей user DB)
- Шифрование всего `settings.json` одним паролем приложения (плохой UX, ключ всё равно в RAM)

## Порядок внедрения

1. `internal/secrets` + миграция с `settings.json`
2. Clear key в Settings + тесты миграции
3. `redact` в emit / persist
4. Блок чтения AppData / settings агентом

## Связанное

- HITL / tools: `internal/tools`, `ToolConfirm` в config
- Маскирование файлов: `internal/workspace/secrets.go`
- Прочие ToDo (private): `docs/TODO-cache-phase5.md`, `docs/TODO-rag-semantic-index.md`
