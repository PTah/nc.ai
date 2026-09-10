# Tools и Agent Loop

Это ядро агентного поведения NotCursor (как у Cursor / Cline).

---

## 1. Идея

Модель **не пишет файлы сама**. Она возвращает `tool_calls`.  
Go исполняет tools и возвращает результаты ролью `tool`. Цикл повторяется.

```
User → [messages + tools] → Provider
                              ↓
                         assistant (+ tool_calls?)
                              ↓
                    execute tools locally
                              ↓
                    append tool results
                              ↓
                         Provider again
                              ↓
                         final assistant text
```

---

## 2. Каноническое объявление tools (OpenAI-shape)

```json
{
  "type": "function",
  "function": {
    "name": "write_file",
    "description": "Create or overwrite a file in the workspace",
    "parameters": {
      "type": "object",
      "properties": {
        "path": { "type": "string" },
        "content": { "type": "string" }
      },
      "required": ["path", "content"]
    }
  }
}
```

---

## 3. Набор tools

### Файлы

| name | params | side effect |
|---|---|---|
| `read_file` | `path`, optional `start_line`/`end_line` | нет; строки с номерами `N\|` |
| `write_file` | `path`, `content` | пишет диск |
| `apply_patch` | `path`, `old_string`/`new_string` или `patch` | пишет диск |
| `delete_file` | `path` | удаляет файл/пустую папку |
| `move_file` | `from`, `to` | rename (`git mv` если tracked) |
| `list_dir` | `path` | нет |
| `find_files` | `query` | нечёткое имя |
| `glob` | `pattern`, optional `path`, `head_limit` | нет |
| `grep` | `query` (regex), optional `path`, `path_glob`, `head_limit`, `context`/`before`/`after`, `output_mode` | нет |

### Shell / jobs / UX

| name | params | notes |
|---|---|---|
| `run_terminal` | `command`, optional `cwd`, `timeout_sec`, `is_background`, `explanation` | HITL; фон → `command_status` |
| `command_status` | `id`, optional `wait_sec` | статус фоновой команды |
| `todo_write` | `todos[]`, `merge` | список в UI |
| `ask_user` | `question`, optional `options[]` | блокирующий вопрос |
| `read_lints` | `paths[]` | `go vet` для `.go` |
| `web_search` | `query` | публичный поиск, SSRF-guard |
| `fetch_url` | `url` | GET http(s), без LAN/loopback |

В **Plan mode** скрыты mutating tools (`write_file`, `apply_patch`, `delete_file`, `move_file`, `run_terminal`, `git_commit`/`push`, `ssh_*`).

### Git / Gitea

| name | params |
|---|---|
| `git_status` | optional `path` |
| `git_diff` | optional `path`, `staged` |
| `git_log` | optional `n`, `path` |
| `git_commit` | `message`, optional `paths[]` |
| `git_push` | optional `remote`, `branch` |

### SSH

| name | params |
|---|---|
| `ssh_keygen` | `name`, optional `type` (`ed25519` default) |
| `ssh_exec` | `host`, `command`, optional `user`, `port`, `key_name` |

---

## 4. Псевдокод loop (канон)

Решение «продолжать / стоп» — **только** по `tool_calls`, как в DeepSeek Thinking/Tool Calls sample.

```go
func (a *Agent) Run(ctx context.Context, userMsg string) error {
    messages := a.seedMessages(userMsg)
    for step := 0; step < a.MaxSteps; step++ {
        resp, err := a.Provider.ChatCompletion(ctx, &llm.ChatRequest{
            Messages:        messages,
            Tools:           a.ToolSpecs(),
            ToolChoice:      "auto",
            Thinking:        map[string]any{"type": "enabled"},
            ReasoningEffort: "high",
        })
        if err != nil {
            return err
        }
        msg := resp.Choices[0].Message
        messages = append(messages, msg) // content + reasoning_content + tool_calls
        if len(msg.ToolCalls) == 0 {
            return nil // финальный ответ
        }
        for _, call := range msg.ToolCalls {
            result := a.Tools.Execute(ctx, call)
            messages = append(messages, llm.ToolResultMessage(call.ID, result))
        }
    }
    return ErrMaxSteps
}
```

См. также таблицу `finish_reason` и §13 в [deepseek.md](./deepseek.md).


---

## 5. Правила безопасности

1. Все относительные пути резолвятся от workspace root.
2. Запрещён path escape (`..` за пределы root) без явного allow.
3. `run_terminal` / `ssh_exec` / `git_push` — потенциально опасны:
   - этап 1: разрешены в рамках workspace + user settings
   - этап 3: confirm UI для destructive ops
4. Секреты и `.env` не отправлять в промпт целиком без нужды.
5. Аргументы tool всегда JSON-parse + schema validate до исполнения.
6. Таймауты на shell/ssh обязательны.

---

## 6. События во фронтенд

Во время loop UI получает:

| event | payload |
|---|---|
| `chat:delta` | текстовая дельта |
| `chat:reasoning` | thinking delta (DeepSeek) |
| `tool:start` | name, args |
| `tool:end` | name, ok, summary |
| `chat:done` | finish_reason, usage |
| `chat:error` | message |

---

## 7. System prompt

См. `internal/agent/agent.go` (`SystemPrompt` + `PlanModePrompt`). Кратко: не выдумывать пути, патчить через tools, git только по явной просьбе, параллельные read-only tools, в Plan — без правок.

---

## 8. Тест-кейсы agent loop

1. User: «создай hello.txt с текстом hi» → `write_file` → подтверждение.
2. User: «покажи git status» → `git_status` → текст статуса.
3. User: «выполни Get-Date» → `run_terminal` → вывод PowerShell.
4. Tool arguments битый JSON → ошибка tool result, модель может повторить.
5. MaxSteps превышен → понятная ошибка в UI.
