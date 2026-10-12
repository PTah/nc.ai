# MCP-серверы для кода (NotCursor)

Как программа работает с пресетами Settings → MCP (с 0.8.5.1).

## Автоустановка

Кнопка пресета вызывает `ApplyMCPPreset`:

1. `EnsureInstalled` — pip / npm -g / uvx prefetch / npx (без глобальной установки).
2. Пишет сервер в `settings.json` → `mcpServers` (command, args, enabledTools).
3. Пробует `Start` (initialize + tools/list).
4. В agent loop tools попадают как `mcp:<id>:<tool>` только из **enabledTools**.

| Пресет | Откуда | Установка | Запуск |
|---|---|---|---|
| Codebase Memory | [npm](https://www.npmjs.com/package/codebase-memory-mcp) | `npm install -g codebase-memory-mcp` | `codebase-memory-mcp` |
| Code Review Graph | [PyPI](https://pypi.org/project/code-review-graph/) | `pip install --user code-review-graph` | `code-review-graph serve` |
| CodeGraph MCP | [npm `@agntk/codegraph-mcp`](https://www.npmjs.com/package/@agntk/codegraph-mcp) | не нужно (npx) | `npx -y @agntk/codegraph-mcp` |
| Cognee | [docs](https://docs.cognee.ai/cognee-mcp/mcp-local-setup) | кэш `uvx` | `uvx cognee-mcp@latest` |

На машине нужны: **Node.js** (npm/npx), **Python 3.10+** (pip), **uv** (для Cognee).

## Порядок для агента

Системный промпт (если есть MCP tools) + подсказки пресетов:

1. Индекс / build графа (если ещё нет).
2. Запрос к графу / памяти (`search_graph`, `query_graph_tool`, …).
3. `read_file` по найденным путям.
4. `apply_patch` / правки.
5. Широкий `grep` — только если граф не помог.

Не встраиваем embeddings/Joern в ядро NotCursor — только внешний процесс MCP.
