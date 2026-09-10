# ToDo: phase 5 — warm project-tree / inline-режим

**Только private remotes (`home`, `kalinamall`). На `github` эти ToDo не пушим.**

Статус:

- **Warm project-tree** — **сделано** (v0.5.25): сжатое дерево после system prompt (`ProjectMap`).
- **Редактор файла** — **сделано** (v0.6.0): клик в дереве открывает textarea + IDE-контекст (`active_file` / `cursor_line`).
- **Inline-режим (Ctrl+K)** — **отложено**.

## Что осталось

**Inline-режим (Ctrl+K / правки в файле)** — отдельный контекст и история от бокового чата, чтобы не смешивать длинный chat-history с точечными правками кода.

## Для чего

| Идея | Зачем |
|------|--------|
| Warm tree | Больше `prompt_cache_hit_tokens` и ориентация в архитектуре без RAG. |
| Inline отдельно | Не сбрасывать кэш чата правками «в файле» и наоборот. |

## Когда браться (inline)

- Когда появится продуктная нужда в Ctrl+K / inline-edit.

## Связанное

- RAG/эмбеддинги: `docs/TODO-rag-semantic-index.md`
- Secrets hardening: `docs/TODO-secrets-hardening.md`
