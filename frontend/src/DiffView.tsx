// Created by nc.ai 1.1.18 on new-admin-pc 2026-11-12 10:31:21
import type {JSX} from 'react'

/**
 * Unified diff (git_diff / apply_patch) looks like this; anything else stays a
 * plain <pre>. Держим проверку рядом с рендером, чтобы оба не разъезжались.
 */
export function looksLikeDiff(s: string): boolean {
  return /^diff --git /m.test(s) || /^@@ /m.test(s) || /^\+\+\+ /m.test(s)
}

/**
 * Diff viewer: подсвечивает добавленные/удалённые строки вместо серой стены.
 * Вынесено из App.tsx (там 10k строк) — см. docs/roadmap.md, пункт про распил.
 */
export function DiffBlock({text}: {text: string}): JSX.Element {
  const lines = text.split('\n')
  return (
    <pre className="nc-tool-pre nc-diff">
      {lines.map((ln, i) => {
        let cls = 'nc-diff-ctx'
        if (ln.startsWith('diff --git') || ln.startsWith('index ') || ln.startsWith('---') || ln.startsWith('+++')) {
          cls = 'nc-diff-meta'
        } else if (ln.startsWith('@@')) {
          cls = 'nc-diff-hunk'
        } else if (ln.startsWith('+')) {
          cls = 'nc-diff-add'
        } else if (ln.startsWith('-')) {
          cls = 'nc-diff-del'
        }
        return (
          <span key={i} className={cls}>
            {ln + '\n'}
          </span>
        )
      })}
    </pre>
  )
}
