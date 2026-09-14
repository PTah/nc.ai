/** DOM helpers for in-chat Ctrl-F find (marks survive until cleared / re-applied). */

export const FIND_MARK_CLASS = 'nc-find-mark'
export const FIND_CURRENT_CLASS = 'nc-find-current'

export function clearChatFindMarks(root: HTMLElement): void {
  const marks = root.querySelectorAll(`mark.${FIND_MARK_CLASS}`)
  const parents = new Set<Node>()
  marks.forEach((mark) => {
    const parent = mark.parentNode
    if (!parent) return
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark)
    parent.removeChild(mark)
    parents.add(parent)
  })
  parents.forEach((parent) => parent.normalize())
}

const SKIP_TAGS = new Set(['SCRIPT', 'STYLE', 'TEXTAREA', 'INPUT'])

/**
 * TreeWalker filter over SHOW_ELEMENT | SHOW_TEXT.
 * Rejecting a whole element prunes its subtree, so collapsed <details>
 * (thinking / tool groups) are never traversed — that keeps big sessions fast.
 */
function acceptFindNode(node: Node): number {
  if (node.nodeType === Node.ELEMENT_NODE) {
    const el = node as Element
    if (SKIP_TAGS.has(el.tagName)) return NodeFilter.FILTER_REJECT
    if (el.hasAttribute('hidden')) return NodeFilter.FILTER_REJECT
    if (el.classList.contains('nc-find-bar') || el.classList.contains('nc-role')) {
      return NodeFilter.FILTER_REJECT
    }
    const closed = el.closest('details:not([open])')
    if (closed && !el.closest('summary')) return NodeFilter.FILTER_REJECT
    return NodeFilter.FILTER_SKIP
  }
  if (node.nodeType !== Node.TEXT_NODE) return NodeFilter.FILTER_REJECT
  const t = node.textContent
  if (!t || !t.trim()) return NodeFilter.FILTER_REJECT
  return NodeFilter.FILTER_ACCEPT
}

export type ChatFindResult = {count: number; marks: HTMLElement[]}

/**
 * Wrap case-insensitive matches of `query` in <mark>. Returns the marks so the
 * caller can move the current highlight without re-walking the DOM.
 */
export function applyChatFindMarks(root: HTMLElement, query: string): ChatFindResult {
  clearChatFindMarks(root)
  const q = query.trim()
  if (!q) return {count: 0, marks: []}

  const walker = document.createTreeWalker(
    root,
    NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT,
    {acceptNode: acceptFindNode},
  )
  const textNodes: Text[] = []
  let n: Node | null
  while ((n = walker.nextNode())) {
    if (n.nodeType === Node.TEXT_NODE) textNodes.push(n as Text)
  }

  const lowerQ = q.toLowerCase()
  const marks: HTMLElement[] = []

  for (const textNode of textNodes) {
    const text = textNode.textContent || ''
    const lower = text.toLowerCase()
    const ranges: {start: number; end: number}[] = []
    let from = 0
    while (from < lower.length) {
      const idx = lower.indexOf(lowerQ, from)
      if (idx < 0) break
      ranges.push({start: idx, end: idx + q.length})
      from = idx + Math.max(1, q.length)
    }
    if (ranges.length === 0) continue

    const frag = document.createDocumentFragment()
    let last = 0
    for (const r of ranges) {
      if (r.start > last) {
        frag.appendChild(document.createTextNode(text.slice(last, r.start)))
      }
      const mark = document.createElement('mark')
      mark.className = FIND_MARK_CLASS
      mark.textContent = text.slice(r.start, r.end)
      frag.appendChild(mark)
      marks.push(mark)
      last = r.end
    }
    if (last < text.length) {
      frag.appendChild(document.createTextNode(text.slice(last)))
    }
    textNode.parentNode?.replaceChild(frag, textNode)
  }

  if (marks.length === 0) return {count: 0, marks}
  return {count: marks.length, marks}
}

/**
 * Marks the match at `currentIndex` (mod count) as current and scrolls it into
 * view. Cheap: only touches classes, no DOM walk.
 */
export function focusChatFindMark(marks: HTMLElement[], currentIndex: number): void {
  if (marks.length === 0) return
  const i = ((currentIndex % marks.length) + marks.length) % marks.length
  marks.forEach((m, idx) => {
    m.classList.toggle(FIND_CURRENT_CLASS, idx === i)
  })
  marks[i].scrollIntoView({block: 'center', inline: 'nearest', behavior: 'smooth'})
}
