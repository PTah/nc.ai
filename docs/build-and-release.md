<!-- Created by nc.ai 0.8.7.7 on Papasha 2026-10-16 20:38:11 -->
# Сборка, установка и релиз

Как из исходников получить установленное приложение и выпустить версию: сборка, установка
собранного билда туда, откуда приложение запускается, версия, артефакты и релиз.

## Что получится

| Что | Где |
|---|---|
| Собранный бинарь | `build/bin/NotCursor.exe` (Windows), `build/bin/NotCursor.app` (macOS) |
| Установленная копия | папка, из которой приложение реально запускается (например `C:\Soft`, `~/Apps`, `/Applications`) |
| Артефакты релиза | `<app>-X.Y.Z-windows-amd64.zip` + `.sha256` (macOS — `macos-*.zip`) |

## Что нужно заранее

- Go и Node.js + npm (версии — из `go.mod` и `frontend/package.json`);
- wails CLI: `go install github.com/wailsapp/wails/v3/cmd/wails@latest`;
- Windows: WebView2 Runtime (в актуальных сборках Windows уже стоит); macOS: Xcode Command Line
  Tools — нужны и для сборки, и для ad-hoc подписи `.app`;
- для публикации релиза: `gh` CLI с авторизацией (или веб-интерфейс GitHub), для зеркала — доступ
  к своему Gitea.

## Быстрый путь: одной командой

Windows:

```powershell
cd C:\repos\nc.ai
.\build.ps1 -NoRestart
```

macOS:

```bash
cd ~/repos/nc.ai
./build.sh --no-restart
```

**Что должно получиться:** строка вида `Built '…build/bin/NotCursor.exe' in 9s` и файл на диске.

**Проверка:**

```powershell
Get-Item build\bin\NotCursor.exe
```

```bash
ls -lh build/bin/NotCursor.app
```

## Установка собранного билда (без этого релиз не видно)

Приложение запускается **не из `build/bin`**, а из установленной копии. Поэтому «собрал» ≠ «обновил».

Windows — сразу собрать и положить в папку раздачи/установки:

```powershell
.\build.ps1 -CopyTo C:\Soft -NoRestart
```

macOS — заменить установленный бандл:

```bash
./build.sh --install-to ~/Apps --no-restart
```

Тонкость: если приложение уже запущено, копирование **поверх** exe падает («файл занят» —
Windows не даёт перезаписать запущенный образ). Правильный приём: сначала **переименовать** занятый
файл (переименование запущенного exe Windows разрешает), затем положить на его место новый. Так
`build.ps1` поступает со своей копией в `build/bin`; для папки установки это нужно делать так же.

Проверить, что установлено именно то, что собрано, — сравнить хеш установленного файла с хешем exe
**внутри** релизного архива:

```powershell
Get-FileHash C:\Soft\NotCursor.exe -Algorithm SHA256
Expand-Archive -Path build\dist\nc.ai-X.Y.Z-windows-amd64.zip -DestinationPath $env:TEMP\nc-zip
Get-FileHash $env:TEMP\nc-zip\NotCursor.exe -Algorithm SHA256
```

И убедиться, откуда приложение реально стартует:

```powershell
Get-Process NotCursor | Select-Object Id, Path, StartTime
```

## Версия

Версия и «что нового» живут в коде, их надо менять **одним коммитом** вместе с правкой:

| Файл | Что менять |
|---|---|
| `internal/appmeta/meta.go` | `Version` и списки `HighlightsRU` / `HighlightsEN` |
| `wails.json` | `productVersion` — `X.Y.Z.0` |
| `frontend/package.json`, `frontend/package-lock.json` | `version` (в lock — два места) |
| `README.md` | строка «Версия» |
| `docs/releases/vX.Y.Z.md` | заметка о релизе |

Пункт про **несколько папок и multitask** в списках «что нового» обязателен — на него опирается
тест `internal/appmeta`, без него сборка тестов падает.

## Релиз по шагам

Полный цикл удобно автоматизировать скриптом; в репозитории такого скрипта нет (локальные скрипты не
коммитятся), поэтому ниже — шаги, которые он выполняет.

1. **Версия и заметка** — правки из таблицы выше.
2. **Проверки** (см. раздел ниже) — все должны быть зелёными.
3. **Коммит и тег:**

```bash
git add README.md wails.json frontend/package.json frontend/package-lock.json \
        internal/appmeta/meta.go docs/releases/vX.Y.Z.md
git commit -m "feat: … (X.Y.Z)"
git tag -a vX.Y.Z -m "NotCursor X.Y.Z"
```

4. **Артефакты** — zip и sha256 **от exe внутри архива** (именно этот хеш проверяет обновление):

```powershell
New-Item -ItemType Directory -Force build\dist | Out-Null
Compress-Archive -Path build\bin\NotCursor.exe -DestinationPath build\dist\nc.ai-X.Y.Z-windows-amd64.zip -Force
(Get-FileHash build\bin\NotCursor.exe -Algorithm SHA256).Hash.ToLowerInvariant() |
  Set-Content build\dist\nc.ai-X.Y.Z-windows-amd64.zip.sha256 -NoNewline
```

Проверка, что внутри архива лежит именно эта сборка (иначе обновление у людей будет **вечно**
предлагать одну и ту же версию):

```powershell
$t = Join-Path $env:TEMP 'nc-zip-check'
Remove-Item $t -Recurse -Force -ErrorAction SilentlyContinue
Expand-Archive build\dist\nc.ai-X.Y.Z-windows-amd64.zip $t -Force
Get-FileHash "$t\NotCursor.exe" -Algorithm SHA256
Remove-Item $t -Recurse -Force
```

Хеш обязан совпасть с содержимым `.sha256`. Ловушка ровно здесь: при повторной выкладке **тем же
номером** легко переиспользовать уже существующий архив — тогда внутри zip остаётся прежняя сборка,
а подпись пересчитывается от новой. Приложение ставит exe из архива, сверяет его с подписью, видит
расхождение и снова предлагает обновление: «скачать и перезапустить» не помогает, потому что архив
старый. Архив всегда собирать заново.

5. **GitHub Release** с обоими файлами:

```bash
gh release create vX.Y.Z --title "NotCursor X.Y.Z" --notes-file docs/releases/vX.Y.Z.md --latest \
   build/dist/nc.ai-X.Y.Z-windows-amd64.zip build/dist/nc.ai-X.Y.Z-windows-amd64.zip.sha256
```

**Релиз никогда не удаляйте и не пересоздавайте.** Сборки делаются с разных машин (Windows и macOS),
и релиз с одним номером наполняется с обеих сторон. Удаление релиза стирает чужие артефакты. Если
релиз с этим тегом уже есть — правьте заголовок с заметками и **доливайте свои файлы**:

```bash
gh release edit vX.Y.Z --title "NotCursor X.Y.Z" --notes-file docs/releases/vX.Y.Z.md --latest
gh release upload vX.Y.Z --clobber build/dist/nc.ai-X.Y.Z-windows-amd64.zip \
   build/dist/nc.ai-X.Y.Z-windows-amd64.zip.sha256
```

Для Gitea то же самое: `PATCH /api/v1/repos/{owner}/{repo}/releases/{id}` (заголовок и заметки) и
загрузка ассета с предварительным удалением одноимённого — иначе появится дубль.

6. **Установка билда** в папку, откуда приложение запускается (см. предыдущий раздел) и **тег в
   зеркала**, чтобы подпись сборки и релиз совпадали.

7. **Проверка релиза:**

```bash
gh release list --limit 3
gh release view vX.Y.Z --json tagName,name,assets
```

## Перезалив тем же номером

Если свежий свой билд оказался с багом, номер **не поднимают** — исправляют и перезаливают тот же:
пользователю понятно, какую версию считать рабочей, и в истории нет версий, которых никто не видел.

```bash
git tag -d vX.Y.Z
git push <remote> --delete vX.Y.Z          # в каждое зеркало
gh release delete vX.Y.Z --yes             # удалить релиз
# затем снова собрать, закоммитить, создать тег и релиз тем же номером
```

Тег удалять безопасно: коммит остаётся в истории ветки. Заметку `docs/releases/vX.Y.Z.md` правят
вместе с исправлением — она описывает то, что реально лежит в релизе.

## Проверки перед коммитом

```bash
go build ./...
go test ./...
npm --prefix frontend run build
```

Плюс кодировки, если правили скрипты (правило одинаково для всех платформ):

- `.ps1` — UTF-8 **с BOM**, переводы строк **CRLF**; `.sh` — UTF-8 **без BOM**, **LF**;
- markdown, JSON, Python — UTF-8 без BOM, переводы строк по `.editorconfig`.

## Грабли

| Симптом | Причина | Что делать |
|---|---|---|
| Правки «не видны» в приложении | запущена старая установленная копия, а собранный exe лежит в `build/bin` | поставить билд в папку запуска и перезапустить приложение (см. выше) |
| Сборка оборвала работу агента | `build.ps1` без `-NoRestart` закрывает и перезапускает приложение | для сборки всегда `-NoRestart`; перезапуск — отдельным шагом |
| `Copy-Item` не может перезаписать exe | файл занят запущенным процессом | переименовать занятый файл и положить новый поверх |
| `frontend/wailsjs/*` вечно «изменены» | `wails build` перезаписывает биндинги, различие только в переводах строк | `git checkout -- frontend/wailsjs`; реальные изменения `models.ts` коммитить |
| Тест `internal/appmeta` падает после бампа | из «что нового» пропал пункт про несколько папок и multitask | вернуть пункт |
| Обновление вечно предлагает одну и ту же версию | в релизном zip осталась прежняя сборка, а `.sha256` пересчитан от новой | пересобирать архив при каждой выкладке и сверять хеш exe внутри zip с подписью |
| Пропали артефакты другой платформы (macOS) | релиз удалили и создали заново при выкладке с другой машины | релиз не удалять: править заметки и доливать свои ассеты (`gh release upload --clobber`) |
| Скрипт `.ps1` не запускается после правки | срезан BOM | перезаписать файл как UTF-8 с BOM + CRLF |
