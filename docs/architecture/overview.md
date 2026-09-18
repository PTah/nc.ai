# Архитектура NotCursor.ai

См. также [ТЗ](../TZ.md) и [протоколы](../exchange-protocols/README.md).

```text
frontend/                  React UI (projects, tree, chat, tabs, terminal, settings)
internal/
  app bindings             app.go / main.go — Wails façade, lifecycle, layout/geometry
  config/                  settings store (окно, layout, тема, провайдеры, ключи)
  workspace/               projects + sandbox FS + guard секретов
  fsx/                     атомарная запись файлов (temp + rename + .bak)
  llm/                     canonical OpenAI types, Provider interface, общий shared слой
  llm/providers/
    deepseek/              DeepSeek (OpenAI-compatible)
    zai/                   Z.ai (glm-*, /models, balance, err mapping)
  tools/                   registry + executor (read/write/patch/delete/grep/terminal/git/ssh/web)
  agent/                   tool loop + autoroute + retry + компакция + plan mode
  netx/                    SSRF-safe HTTP + web_search
  redact/                  маскирование секретов в tool output / events
  shell/                   oneshot (PowerShell/-lc, пайпы) + интерактивный PTY (go-pty)
  gitx/                    OS git (status/diff/commit/push)
  sshx/                    SSH exec + keygen (known_hosts/TOFU)
  rules/                   загрузчик Cursor rules (global + project, рекурсивно)
  chatstore/               multi-session persistence + archive
  costing/                 token → USD (peak/off-peak)
```

## Поток чата (текущий, v0.5.x)

1. UI формирует user message (текст + опционально вложения/картинки).
2. `App.RunAgentWithAttachments` → `agent.Runner`.
3. Runner резолвит модель (flash/pro/vision), применяет Cursor rules + компакцию истории.
4. LLM-провайдер возвращает `tool_calls` или финальный текст.
5. Tools исполняются локально (sandbox FS, PowerShell, git, ssh), результат — обратно в LLM.
6. UI получает события `delta/reasoning/tool_start/tool_end/done/error/reconnect/persist/progress`.
7. Итоговый транскрипт (items) пишет фронт, LLM-history — бэк в `chatstore`.

### Сессия, проект и очередь (важно)

Бэкенд ведёт **одну активную сессию** (`a.sessionID`): агент всегда работает в том чате, который
открыт в UI. Отсюда правила, которые держат проекты раздельно:

- черновик, вложения, очередь вопросов и todo-список живут **в состоянии сессии** (ключ — `sessionId`),
  а не в общем состоянии приложения;
- вопрос, набранный во время работы агента, встаёт в очередь **своей** сессии и снимается из неё
  только после того, как запуск реально стартовал (иначе сообщение можно потерять);
- очередь дренится, только когда её сессия активна и не занята — то есть после возврата в тот проект;
- запуск фиксирует «свой» проект на всё время работы (`projKey`), и запись истории/usage/транскрипта
  идёт в файл проекта-владельца сессии (`chatstore.FindProject`), даже если пользователь уже смотрит
  другой проект.
- **инструменты тоже пиннятся на проект запуска**: `tools.Registry.ForRun(root)` отдаёт копию реестра
  со своим `WSRoot`, `workspace.Manager.Scoped(root)`, `gitx.Service.WithRoot(root)` и собственным
  `TodoStore`. Без этого `read_file`/`write_file`/`run_terminal`/`git_*` уходили в ту папку, которую
  пользователь открыл второй, пока агент ещё работал.

## Терминал: две модели исполнения

| Режим | Frontend | Backend | Процесс |
|---|---|---|---|
| Интерактивная сессия | xterm.js: `term.onData → TerminalWrite`, `TerminalResize`, подписка на событие `terminal:data` | `App.StartTerminal` / `StopTerminal` / `ensureTerminal` → `shell.Session` (go-pty) | PTY/ConPTY: shell в корне проекта, стрим вывода событием `terminal:data`, стоп = kill процесса |
| One-shot (поле + **Run**) | `RunShell(cmd)` → печать `> cmd`, `stdout`, `stderr`, `[exit N]` в тот же xterm | `shell.Run` | один процесс без PTY: PowerShell `-NoLogo -NoProfile -NonInteractive -Command`, POSIX `-lc`, cmd `/C`; stdin закрыт, пайпы, timeout 60 с |

Общее для обоих режимов:

- `cwd` = корень активного workspace (`ws.ActiveRoot()`);
- shell резолвится из настроек (`shell.ResolveShell`), тот же путь отдаётся tool'у `run_terminal`;
- PowerShell-ветка one-shot добавляет UTF-8 prelude (`chcp 65001`, `OutputEncoding`);
- агент использует one-shot-путь (`run_terminal`), интерактивный PTY — только для пользователя в UI.

## Ключевые решения

- **Провайдеры взаимозаменяемы** через общий OpenAI-compat wire-формат (`llm`), различия — в клиентах.
- **Безопасность секретов**: `read_file` не отдаёт `.env`/ключи модели; значения `KEY=…` маскируются.
- **SSH без `InsecureIgnoreHostKey`**: `known_hosts` + trust-on-first-use.
- **Ничего не теряется при сбое**: атомарная запись JSON с `.bak`.
- **Агент умеет продолжать**: автоповторы transient-ошибок + ручной `Reconnect`.
