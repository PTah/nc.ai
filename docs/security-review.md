<!-- Created by nc.ai 1.1.15 on macbook 2026-11-11 21:31:31 -->
<!-- Edited by nc.ai 1.1.18 on new-admin-pc 2026-11-12 10:33:39 -->
<!-- Created by nc.ai 1.1.16 on macbook 2026-11-11 -->
# Security review с MCP: Semgrep и чеклист

NotCursor не встраивает статический анализ в ядро. Формальный разбор подключается **снаружи** — как
MCP-пресет «Semgrep (security review)» в Settings → Агент и MCP; пользователь сам решает, когда его
включить. Ниже — короткий путь, что агент делает после подключения и что всё равно надо проверить
руками.

## 1. Подключение (одна кнопка)

1. Settings → **Агент и MCP** → пресет **Semgrep (security review)** → «Применить».
2. NotCursor подтягивает `semgrep-mcp` через `uvx` (первый запуск скачивает пакет, ~20–60 с).
3. Нажмите **Проверить** и оставьте в чек-листе инструментов нужное (по умолчанию:
   `security_check`, `semgrep_scan`, `semgrep_scan_with_custom_rule`).

Проверка, что сервер жив:

```bash
uvx semgrep-mcp --version   # печатает версию и выходит
```

Ожидаемо: `semgrep-mcp 0.9.x`. Если `uvx` не найден — поставьте `uv` (`brew install uv`).

## 2. Что делает агент

- `security_check` — быстрый прогон готового набора правил по изменённым файлам (не по всему репо).
- `semgrep_scan` — тот же скан с явным `config` (например `p/default`), когда нужен другой набор.
- `semgrep_scan_with_custom_rule` — своя YAML-правило, когда шаблон нужен специфический.
- `semgrep_findings` — только если в env сервера задан `SEMGREP_APP_TOKEN` (Semgrep Cloud).

Вывод инструмента — конкретные находки `file:line` и id правила. Агент сводит их в `findings[]`
(`{category, severity, file, message}`), а не пересказывает простынёй в чате.

## 3. Чеклист после прогона

Сначала читаем находки, потом смотрим руками то, что анализатор не увидит:

| Что | Почему вручную |
|---|---|
| Границы доверия: откуда приходят данные (HTTP-параметр, файл, stdin), куда идут | Semgrep видит sink/source по правилам, но не вашу архитектуру |
| Секреты: `os.Getenv`, `.env`, лог `fmt.Printf("%v", cfg)` | Правило ловит константы, а не «секрет попал в лог» |
| Команды и пути: `exec.Command`, `os.Open` с внешней строкой | Нужен разбор, подставляется ли пользовательский ввод |
| Права: файлы `0644` на секретах, root в `Dockerfile`, лишние `sudo` | Часть правил это видит, часть нет |
| Дефолты: TLS-проверка, таймауты, ретраи, лимиты размера | «Нет таймаута» — находка по сути, а не по правилу |
| Зависимости: что реально тянется в сборку | Semgrep не заменяет `govulncheck` / `npm audit` |

Быстрые проверки для Go-репозитория:

```bash
go vet ./... && go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Ожидаемо: пусто или список с явными вердиктами «не применимо».

## 4. Заготовка `SECURITY.md` для продукта

Минимальный файл, который кладётся в корень репозитория (адаптировать под проект):

```markdown
# Security policy

## Поддерживаемые версии
Последняя версия ветки `master` (см. README).

## Как сообщить об уязвимости
Пишите приватно: <контакт>. Не открывайте публичный issue до ответа.
Ответ в течение 7 дней, статус — раз в неделю до закрытия.

## Что считается уязвимостью
- утечка токенов/ключей из хранилища приложения или логов;
- выполнение команд из непроверенного контента (промпт-инъекция → tool call);
- обход подтверждений для опасных инструментов (shell, ssh, delete);
- SSRF в средствах сети (fetch_url, MCP по HTTP, обновления).

## Что не считается
- ответы модели, не приводящие к действиям инструментов;
- падение на битых данных без последствий для пользователя (присылайте как баг).
```

## 5. Чего этот режим не делает

- Не заменяет ревью человеком и не блокирует коммиты.
- Не тянет CodeQL/Joern/CPG в `internal/` — это отдельные внешние серверы, если понадобятся.
- Не отправляет код в облако: `security_check`/`semgrep_scan` работают локально.

## Проверка 4624 (одна запись на пачку SSH-команд)

Агент держит одну SSH-сессию на хост, поэтому серия команд должна давать **один** вход в журнале
Windows, а не запись на каждую команду. Проверяется только на машине с SSH-сервером и правами
администратора (журнал Security иначе не читается):

```powershell
# 1. Сколько входов было до серии
$before = (Get-WinEvent -FilterHashtable @{LogName='Security'; Id=4624} -MaxEvents 200 |
  Where-Object { $_.Message -match 'Logon Type:\s+5' }).Count

# 2. Пять команд в одной сессии (брокер переиспользует соединение)
& "$HOME\CursorRules\scripts\Invoke-RemoteSession.ps1" -Target user@host -Command "hostname"
& "$HOME\CursorRules\scripts\Invoke-RemoteSession.ps1" -Target user@host -Command "whoami"
& "$HOME\CursorRules\scripts\Invoke-RemoteSession.ps1" -Target user@host -Command "uname -a"
& "$HOME\CursorRules\scripts\Invoke-RemoteSession.ps1" -Target user@host -Command "echo ok"
& "$HOME\CursorRules\scripts\Invoke-RemoteSession.ps1" -Target user@host -Command "echo \$\$"

# 3. Прирост записей: ожидается 1 (type 5 — network logon), а не 5
$after = (Get-WinEvent -FilterHashtable @{LogName='Security'; Id=4624} -MaxEvents 200 |
  Where-Object { $_.Message -match 'Logon Type:\s+5' }).Count
$after - $before
```

Если прирост больше единицы — сессия переоткрывается (проверить `ControlMaster`/`ControlPersist`
или, на Windows-брокере, живость консоли MCP).
