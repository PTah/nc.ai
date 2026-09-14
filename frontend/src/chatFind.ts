/** DOM helpers for in-chat Ctrl-F find (marks survive until cleared / re-applied). */

export const FIND_MARK_CLASS = 'nc-find-mark'
export const FIND_CURRENT_CLASS = 'nc-find-current'

export function clearChatFindMarks(root: HTMLElement): void {
  const marks = root.querySelectorAll(`mark.${FIND_MARK_CLASS}`)
  marks.forEach((mark) => {
    const parent = mark.parentNode
    if (!parent) return
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark)
    parent.removeChild(mark)
    parent.normalize()
  })
}

function acceptTextNode(node: Node): number {
  const p = node.parentElement
  if (!p) return NodeFilter.FILTER_REJECT
  if (p.closest('script, style, textarea, input, .nc-find-bar, .nc-role')) {
    return NodeFilter.FILTER_REJECT
  }
  if (p.closest(`mark.${FIND_MARK_CLASS}`)) return NodeFilter.FILTER_REJECT
  const t = node.textContent
  if (!t || !t.trim()) return NodeFilter.FILTER_REJECT
  return NodeFilter.FILTER_ACCEPT
}

/**
 * Wrap case-insensitive matches of `query` in <mark>. Returns total match count.
 * Marks the match at `currentIndex` (mod count) as current and scrolls it into view.
 */
export function applyChatFindMarks(
  root: HTMLElement,
  query: string,
  currentIndex: number,
): number {
  clearChatFindMarks(root)
  const q = query.trim()
  if (!q) return 0

  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode: acceptTextNode,
  })
  const textNodes: Text[] = []
  let n: Node | null
  while ((n = walker.nextNode())) {
    textNodes.push(n as Text)
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

  if (marks.length === 0) return 0

  const i = ((currentIndex % marks.length) + marks.length) % marks.length
  const cur = marks[i]
  cur.classList.add(FIND_CURRENT_CLASS)
  cur.scrollIntoView({block: 'center', inline: 'nearest', behavior: 'smooth'})
  return marks.length
}
