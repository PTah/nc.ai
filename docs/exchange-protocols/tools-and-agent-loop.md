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

## 3. Набор tools этапа 1

### Файлы

| name | params | side effect |
|---|---|---|
| `read_file` | `path`, optional `offset`, `limit` | нет |
| `write_file` | `path`, `content` | пишет диск |
| `list_dir` | `path` | нет |
| `search_files` | `query`, optional `glob` | нет (этап 1.1) |

### Shell

| name | params | notes |
|---|---|---|
| `run_terminal` | `command`, optional `cwd`, `timeout_sec` | PowerShell на Windows |

### Git / Gitea

| name | params |
|---|---|
| `git_status` | optional `path` |
| `git_diff` | optional `path`, `staged` |
| `git_commit` | `message`, optional `paths[]` |
| `git_push` | optional `remote`, `branch` |

### SSH

| name | params |
|---|---|
| `ssh_keygen` | `name`, optional `type` (`ed25519` default) |
| `ssh_exec` | `host`, `command`, optional `user`, `port`, `key_name` |

---

## 4. Псевдокод loop

```go
func (a *Agent) Run(ctx context.Context, userMsg string) error {
    messages := a.seedMessages(userMsg)
    for step := 0; step < a.MaxSteps; step++ {
        resp, err := a.Provider.Chat(ctx, &llm.ChatRequest{
            Model:    a.Model,
            Messages: messages,
            Tools:    a.ToolSpecs(),
            Stream:   true,
        })
        if err != nil {
            return err
        }
        messages = append(messages, resp.AssistantMessage)
        if len(resp.AssistantMessage.ToolCalls) == 0 {
            return nil // final answer
        }
        for _, call := range resp.AssistantMessage.ToolCalls {
            result := a.Tools.Execute(ctx, call)
            messages = append(messages, llm.ToolMessage(call.ID, result))
        }
    }
    return ErrMaxSteps
}
```

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

## 7. System prompt (черновик этапа 1)

```
Ты — агент NotCursor.ai. Работай в текущем workspace.
Используй tools для чтения/правки файлов, shell, git и ssh.
Не выдумывай содержимое файлов — читай через tools.
После изменений кратко объясни, что сделал.
Для git push указывай remote/branch явно, если они не очевидны.
```

---

## 8. Тест-кейсы agent loop

1. User: «создай hello.txt с текстом hi» → `write_file` → подтверждение.
2. User: «покажи git status» → `git_status` → текст статуса.
3. User: «выполни Get-Date» → `run_terminal` → вывод PowerShell.
4. Tool arguments битый JSON → ошибка tool result, модель может повторить.
5. MaxSteps превышен → понятная ошибка в UI.
