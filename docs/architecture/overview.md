# Архитектура NotCursor.ai (черновик)

См. также [ТЗ](../TZ.md) и [протоколы](../exchange-protocols/README.md).

```
frontend/          React UI (projects, tree, chat, terminal, settings)
internal/
  app bindings     main/app.go (Wails façade)
  config/          settings + secrets
  workspace/       projects + sandbox FS
  llm/             canonical OpenAI types + Provider interface
  llm/providers/
    deepseek/      stage-1 provider
  tools/           tool specs + (soon) executors
  shell/           PowerShell / bash
  gitx/            go-git + Gitea push
  sshx/            ssh exec + keygen
  agent/           (next) tool loop orchestrator
```

Поток чата этапа 1:

`UI ChatOnce` → `App.ChatOnce` → `deepseek.Client.ChatCompletion` → ответ в UI.

Далее: streaming events + agent loop с `tools.Specs()`.
