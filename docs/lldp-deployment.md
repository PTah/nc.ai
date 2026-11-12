<!-- Edited by nc.ai 1.2.2 on new-admin-pc 2026-11-12 15:01:15 -->
<!-- Created by nc.ai 1.2.2 on new-admin-pc 2026-11-12 -->

# LLDP на клиентских машинах (NetLynx)

Пакет — `WinLLDPService-x64.msi` (WinLLDPService 17.10.20.2236, x64). Нужен на клиентах для
дополнительных удобств сервиса NetLynx.

## Шара

`\\heap.example.com\dc-share` (HEAP, Synology; «Share for domain policies»).

| Кто | Права |
|---|---|
| Администраторы домена | чтение/запись |
| `CONTOSO\Компьютеры домена` | только чтение |

Пакет лежит в корне: `\\heap.example.com\dc-share\WinLLDPService-x64.msi` (700416 байт).

## Как развёрнуто

Старая GPO **«Install LLDP agent»** (`{49FA48AF-…}`) публиковала MSI через Software Installation с
источником `\\b26\NETLOGON\lldp\` — клиентам он недоступен, пакет не устанавливался, а на каждом
`gpupdate /force` CSE Software Installation уходил в мёртвый путь (событие 8016): `gpupdate` висел
минутами, дочерние `svchost` жгли CPU.

Перевести её на новую шару нельзя: источник пакета хранится в бинарном `.aas` (формат IXOS), а
GPMC-командлеты (`Get-GPO`, `Set-GPPermissions`) из сессии отдают Access Denied — доступ к SYSVOL
по SMB у доменной учётки отфильтрован.

Поэтому:

1. Старая GPO — **computer-side disabled** (AD `flags` = 2). `gpupdate /force` на HP-SERV: 8 секунд
   вместо 5+ минут.
2. Создана GPO **«Install LLDP agent (heap share)»** (`{2584B3A1-5C65-408A-B209-F30E1964B84D}`):
   линк на корне домена (order 2), `gPCMachineExtensionNames` = Scripts CSE
   (`{42B5FAAE-6536-11D2-AE5A-0000F87571E3}{40B66650-4972-11D1-A7CA-0000F87571E3}`),
   GPT version 3 = AD versionNumber 3.
   Внутри — startup script `Machine\Scripts\Startup\install-lldp.cmd`:

   ```bat
   @echo off
   sc query WinLLDPService >nul 2>&1 && exit /b 0
   if not exist "\\heap.example.com\dc-share\WinLLDPService-x64.msi" exit /b 0
   msiexec /i "\\heap.example.com\dc-share\WinLLDPService-x64.msi" /qn /norestart
   ```

   Установка идёт при **загрузке** (startup script), не по `gpupdate`.
3. Делегирование GPO: `Прошедшие проверку` → Read + Write + Apply (выставлено в GPMC).

## Проверено (2026-11-12, HP-SERV)

- GPO видна в `gpresult /r /scope:computer` → применяется.
- Чтение шары из контекста **SYSTEM** (как у стартового скрипта):
  `dir \\heap.example.com\dc-share` → в списке `WinLLDPService-x64.msi`.
- Установка через шару: `msiexec /i \\heap.example.com\dc-share\WinLLDPService-x64.msi /qn` →
  служба `WinLLDPService` в состоянии **RUNNING**.

Замечание по диагностике: доступ к шаре **не** проверять из WinRM-сессии — там сетевой
отфильтрованный токен, и UNC отвечает «путь не существует», хотя SYSTEM и интерактивная учётка
работают. Проверять через задачу `schtasks /ru SYSTEM` или с консоли.

## Осталось

- Перезагрузить один клиент и убедиться: `sc query WinLLDPService` → RUNNING (скрипт отработает на
  загрузке).
- GPO привязана к корню домена (как и старая) — значит LLDP ставится и на серверы. Если нужно
  только на рабочие станции, перенести линк на OU с компьютерами.

## Как откатить

- Вернуть старую GPO: AD `flags` = 0 (вернётся зависание `gpupdate` — сначала починить источник).
- Удалить новую: снять линк с корня домена и удалить объект
  `CN={2584B3A1-5C65-408A-B209-F30E1964B84D},CN=Policies,CN=System,DC=b26,DC=example,DC=ru`
  вместе с папкой `C:\Windows\SYSVOL\domain\Policies\{2584B3A1-…}`.
- На уже установленных машинах: `msiexec /x {ProductCode}` или удаление «WinLLDPService» из
  «Программы и компоненты».
