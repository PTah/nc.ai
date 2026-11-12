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

## Проверка 4624 (одна запись на пачку команд)

Проверено вживую 2026-11-12 на доменном контроллере `WIN-DC01` (10.0.0.1) учёткой
`b26\jdoe` из `D:\Soft\Git\logins` (админ, журнал Security читается).

**Результат замера:**

| Сценарий | Записей 4624 |
|---|---|
| 10 команд внутри одной `PSSession` (WinRM) | **0** новых |
| открытие самой сессии | 1–3 (type 3, `NtLmSsp`/`Kerberos`) |
| 10 отдельных `Invoke-Command -ComputerName` | **41** |

То есть «одна сессия на хост» работает: команды внутри неё не логинятся заново, а наивный вызов
«по одному соединению на шаг» даёт ~4 записи на команду. На Windows-хостах `sshd` не установлен
(порт 22 закрыт на всех из `logins`), поэтому путь там — WinRM-сессия, а не ssh.

Замер (Windows-клиент, журнал локализован — поэтому считаем по **XML-полям**, а не по тексту
сообщения; `Message -match 'Logon Type:'` на русской Windows не находит ничего):

```powershell
# креды из logins (пароль не печатать)
$p='D:\Soft\Git\logins'; $lines=Get-Content $p
$hdr=($lines[10].TrimStart('#') -split "`t") | ForEach-Object { $_.Trim() }
$pwIdx=[array]::IndexOf($hdr,'password')
$f=($lines | Where-Object { $_ -match '^HP-SERV' } | Select-Object -First 1) -split "`t"
$cred=New-Object System.Management.Automation.PSCredential($f[4].Trim(),
  (ConvertTo-SecureString $f[$pwIdx].Trim() -AsPlainText -Force))

$sb={ param($from) @(Get-WinEvent -FilterHashtable @{LogName='Security';Id=4624;StartTime=$from} |
  ForEach-Object { $d=@{}; ([xml]$_.ToXml()).Event.EventData.Data |
    ForEach-Object { $d[$_.Name]=$_.'#text' }
    if($d['TargetUserName'] -eq 'jdoe'){ [pscustomobject]@{ Time=$_.TimeCreated; Type=$d['LogonType']; Process=$d['LogonProcessName'] } } }) }

# 1. одна сессия + 10 команд
$s=New-PSSession -ComputerName 10.0.0.1 -Credential $cred
$a0=Invoke-Command -Session $s -ScriptBlock { (Get-Date).AddSeconds(-1) }
1..10 | ForEach-Object { Invoke-Command -Session $s -ScriptBlock { hostname } | Out-Null }
$a1=Invoke-Command -Session $s -ScriptBlock { (Get-Date).AddSeconds(-1) }

# 2. те же 10 команд отдельными соединениями
1..10 | ForEach-Object { Invoke-Command -ComputerName 10.0.0.1 -Credential $cred -ScriptBlock { hostname } | Out-Null }
$a2=Invoke-Command -Session $s -ScriptBlock { (Get-Date).AddSeconds(-1) }

$all=Invoke-Command -Session $s -ScriptBlock $sb -ArgumentList $a0
"в сессии: " + @($all | Where-Object { $_.Time -ge $a0 -and $_.Time -lt $a1 }).Count
"по одному соединению: " + @($all | Where-Object { $_.Time -ge $a1 -and $_.Time -lt $a2 }).Count
Remove-PSSession $s
```

Ожидаемо: первая строка `0`, вторая — десятки. Если в первой не ноль — сессия переоткрывается
(проверить живость брокера/консоли MCP, а не `ControlMaster` — на Windows OpenSSH мультиплексирование
не работает).

Мелочи, на которые легко попасться:

- Журнал на русской Windows локализован: `Logon Type` / `Account Name` в тексте нет, только
  `Тип входа` / `Имя учетной записи`. Считать по XML-полям (`LogonType`, `TargetUserName`,
  `LogonProcessName`) — они не переводятся.
- Аудит входа включён не везде: на `HP-SERV` (10.0.0.1) за сутки **ноль** событий 4624,
  на контроллере домена — тысячи. Проверять на DC или на хосте с включённым
  `Audit Logon` (Success).

## Размер и хранение журнала Security

Меняется **одной политикой на весь домен**, а не на каждой машине:
Computer Configuration → Policies → Administrative Templates → Windows Components →
Event Log Service → Security → «Specify the maximum log file size» и «Control the Event Log
behavior when the log file reaches its maximum size» (retention). На отдельной машине то же самое
делает `wevtutil sl Security /maxsize:<байт> /retention:<0|1|2>`.

При 2–3 тыс. записей 4624 в час на DC дефолтных 20 МБ хватает на ~10–15 часов — журнал
прокручивается быстрее суток. Рекомендация: `MaxSize` ≥ 512 МБ (524288 КБ) и retention = 0
(перезаписывать по мере заполнения), чтобы разбор инцидента всегда имел хотя бы сутки входов.
Замечание по применению: настройка кладётся в `registry.pol` GPO, поэтому править её надо через
GPMC на машине с доступом к SYSVOL (по сети от доменной учётки с отфильтрованным токеном GPMC
отдаёт Access Denied — это нормальная защита UAC).
- WinRM по IP требует явных кредов: без `-Credential` клиент ругается на TrustedHosts.

Если прирост больше единицы — сессия переоткрывается (проверить `ControlMaster`/`ControlPersist`
или, на Windows-брокере, живость консоли MCP).
