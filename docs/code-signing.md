<!-- Created by nc.ai 0.8.8.5 on new-admin-pc 2026-10-19 11:41:20 -->
# Подпись Windows-сборки

## Зачем

Неподписанный `NotCursor.exe` у свежесобранной сборки имеет нулевую репутацию — отсюда
ложные срабатывания антивируса (`Trojan:Win32/Bearfoos.A!ml`) и предупреждения SmartScreen.
Основной триггер `Bearfoos.A!ml` — «троянный» паттерн самообновления; он уже убран:
подмена exe идёт тем же `NotCursor.exe` с флагом `--apply-update`, без PowerShell
`-ExecutionPolicy Bypass` и временных `.ps1`.

Подпись бывает двух видов:

| Вид | Публичное распространение | Свой парк машин |
|-----|---------------------------|-----------------|
| CA-сертификат (OV/EV) | да, даёт репутацию | да |
| **Self-signed** (бесплатно) | **нет** — «неизвестный издатель» | **да**, если установлен корневой серт |

Платного CA-сертификата нет — поэтому используем **self-signed** для своих машин.

## Подписать сборку (self-signed)

```powershell
# 1) собрать
.\build.ps1 -NoRestart

# 2) подписать (сертификат создастся один раз и переиспользуется)
.\scripts\Sign-Windows.ps1 -TrustLocal

# 3) упаковать уже подписанный exe
$ver = '0.8.8.5'
Compress-Archive build\bin\NotCursor.exe "build\dist\nc.ai-$ver-windows-amd64.zip" -Force
(Get-FileHash build\bin\NotCursor.exe -Algorithm SHA256).Hash.ToLowerInvariant() |
  Set-Content "build\dist\nc.ai-$ver-windows-amd64.zip.sha256" -NoNewline
```

- `-TrustLocal` добавляет сертификат в `Cert:\CurrentUser\Root` этой машины, чтобы подпись
  сразу считалась доверенной.
- Публичная часть выгружается в `build\dist\NotCursor-selfsigned.cer`.

## Доверие на других машинах (свой парк)

Один раз на каждой целевой машине:

```powershell
Import-Certificate -FilePath NotCursor-selfsigned.cer -CertStoreLocation Cert:\LocalMachine\Root
```

После этого `NotCursor.exe` считается подписанным доверенным издателем, Defender/SmartScreen
не ругаются. Сертификат действует 5 лет.

## Публичное распространение

Self-signed не даёт репутации для чужих машин. Для публики:
1. убран «троянный» апдейтер (сделано) — это снимает основную причину `Bearfoos.A!ml`;
2. при срабатывании — прислать exe в Microsoft как ложное срабатывание:
   <https://www.microsoft.com/en-us/wdsi/filesubmission> → «Software developer» →
   «Incorrectly detected» (по каждому релизу, т.к. хеш меняется).
