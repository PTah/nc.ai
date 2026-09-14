/** In-chat Ctrl-F: prefer CSS Custom Highlight API (no DOM mutation). */

export const FIND_MARK_CLASS = 'nc-find-mark'
export const FIND_CURRENT_CLASS = 'nc-find-current'

const HL_ALL = 'nc-find'
const HL_CUR = 'nc-find-current'

const SKIP_TAGS = new Set(['SCRIPT', 'STYLE', 'TEXTAREA', 'INPUT'])

type HighlightCtor = new (...ranges: AbstractRange[]) => {add(range: AbstractRange): void}
type HighlightsMap = {
  set(name: string, highlight: {add(range: AbstractRange): void}): void
  delete(name: string): void
}

function cssHighlights(): HighlightsMap | null {
  const css = globalThis.CSS as {highlights?: HighlightsMap} | undefined
  if (!css?.highlights) return null
  return css.highlights
}

function HighlightClass(): HighlightCtor | null {
  const H = (globalThis as {Highlight?: HighlightCtor}).Highlight
  return typeof H === 'function' ? H : null
}

function supportsCssHighlight(): boolean {
  return cssHighlights() != null && HighlightClass() != null
}

/** Unwrap leftover <mark> from older find implementations. */
function unwrapLegacyMarks(root: HTMLElement): void {
  const marks = root.querySelectorAll(`mark.${FIND_MARK_CLASS}`)
  if (marks.length === 0) return
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

export function clearChatFindMarks(root: HTMLElement): void {
  const map = cssHighlights()
  if (map) {
    map.delete(HL_ALL)
    map.delete(HL_CUR)
  }
  unwrapLegacyMarks(root)
}

/**
 * TreeWalker filter: prune closed <details> bodies (keep visible <summary>),
 * skip chrome like roles / find bar.
 */
function acceptFindNode(node: Node): number {
  if (node.nodeType === Node.ELEMENT_NODE) {
    const el = node as Element
    if (SKIP_TAGS.has(el.tagName)) return NodeFilter.FILTER_REJECT
    if (el.hasAttribute('hidden')) return NodeFilter.FILTER_REJECT
    if (el.classList.contains('nc-find-bar') || el.classList.contains('nc-role')) {
      return NodeFilter.FILTER_REJECT
    }
    if (el.tagName === 'DETAILS' && !el.hasAttribute('open')) {
      // Enter so <summary> is still searchable; body pruned below.
      return NodeFilter.FILTER_SKIP
    }
    const closed = el.closest('details:not([open])')
    if (closed) {
      if (el.tagName === 'SUMMARY' || el.closest('summary')) return NodeFilter.FILTER_SKIP
      return NodeFilter.FILTER_REJECT
    }
    return NodeFilter.FILTER_SKIP
  }
  if (node.nodeType !== Node.TEXT_NODE) return NodeFilter.FILTER_REJECT
  const p = (node as Text).parentElement
  if (p) {
    const closed = p.closest('details:not([open])')
    if (closed && !p.closest('summary')) return NodeFilter.FILTER_REJECT
  }
  const t = node.textContent
  if (!t || !t.trim()) return NodeFilter.FILTER_REJECT
  return NodeFilter.FILTER_ACCEPT
}

function collectMatchRanges(root: HTMLElement, query: string): Range[] {
  const q = query.trim()
  if (!q) return []

  const walker = document.createTreeWalker(
    root,
    NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT,
    {acceptNode: acceptFindNode},
  )
  const lowerQ = q.toLowerCase()
  const ranges: Range[] = []
  let n: Node | null
  while ((n = walker.nextNode())) {
    if (n.nodeType !== Node.TEXT_NODE) continue
    const textNode = n as Text
    const text = textNode.textContent || ''
    const lower = text.toLowerCase()
    let from = 0
    while (from < lower.length) {
      const idx = lower.indexOf(lowerQ, from)
      if (idx < 0) break
      const end = idx + q.length
      try {
        const range = document.createRange()
        range.setStart(textNode, idx)
        range.setEnd(textNode, end)
        ranges.push(range)
      } catch {
        /* node may be detached mid-walk */
      }
      from = idx + Math.max(1, q.length)
    }
  }
  return ranges
}

/** Legacy path: wrap matches in <mark> (only if CSS.highlights unavailable). */
function applyLegacyMarks(root: HTMLElement, query: string): HTMLElement[] {
  unwrapLegacyMarks(root)
  const q = query.trim()
  if (!q) return []

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
    const parts: {start: number; end: number}[] = []
    let from = 0
    while (from < lower.length) {
      const idx = lower.indexOf(lowerQ, from)
      if (idx < 0) break
      parts.push({start: idx, end: idx + q.length})
      from = idx + Math.max(1, q.length)
    }
    if (parts.length === 0) continue
    const frag = document.createDocumentFragment()
    let last = 0
    for (const r of parts) {
      if (r.start > last) frag.appendChild(document.createTextNode(text.slice(last, r.start)))
      const mark = document.createElement('mark')
      mark.className = FIND_MARK_CLASS
      mark.textContent = text.slice(r.start, r.end)
      frag.appendChild(mark)
      marks.push(mark)
      last = r.end
    }
    if (last < text.length) frag.appendChild(document.createTextNode(text.slice(last)))
    textNode.parentNode?.replaceChild(frag, textNode)
  }
  return marks
}

export type ChatFindHit =
  | {kind: 'range'; range: Range}
  | {kind: 'mark'; el: HTMLElement}

export type ChatFindResult = {count: number; hits: ChatFindHit[]}

/**
 * Find matches. Prefer CSS Custom Highlight (zero DOM writes). Falls back to
 * <mark> wrapping only when Highlight API is missing.
 */
export function applyChatFindMarks(root: HTMLElement, query: string): ChatFindResult {
  clearChatFindMarks(root)
  const q = query.trim()
  if (!q) return {count: 0, hits: []}

  if (supportsCssHighlight()) {
    const ranges = collectMatchRanges(root, q)
    const map = cssHighlights()
    const H = HighlightClass()
    if (map && H && ranges.length > 0) {
      map.set(HL_ALL, new H(...ranges))
    }
    return {count: ranges.length, hits: ranges.map((range) => ({kind: 'range', range}))}
  }

  const marks = applyLegacyMarks(root, q)
  return {count: marks.length, hits: marks.map((el) => ({kind: 'mark', el}))}
}

function scrollHitIntoView(hit: ChatFindHit, smooth: boolean): void {
  const behavior: ScrollBehavior = smooth ? 'smooth' : 'auto'
  if (hit.kind === 'mark') {
    hit.el.scrollIntoView({block: 'center', inline: 'nearest', behavior})
    return
  }
  const range = hit.range
  try {
    const rects = range.getClientRects()
    const rect = rects.length > 0 ? rects[0] : range.getBoundingClientRect()
    if (!rect || (rect.width === 0 && rect.height === 0 && rect.top === 0)) {
      const node = range.startContainer
      const el = node.nodeType === Node.TEXT_NODE ? node.parentElement : (node as Element)
      el?.scrollIntoView({block: 'center', inline: 'nearest', behavior})
      return
    }
    // Prefer scrolling the chat container rather than window (Wails).
    const chat = (range.commonAncestorContainer as Node).parentElement?.closest('.nc-thread') as HTMLElement | null
    if (chat) {
      const chatRect = chat.getBoundingClientRect()
      const target = rect.top - chatRect.top - chatRect.height / 2 + chat.scrollTop + rect.height / 2
      chat.scrollTo({top: Math.max(0, target), behavior})
      return
    }
    const node = range.startContainer
    const el = node.nodeType === Node.TEXT_NODE ? node.parentElement : (node as Element)
    el?.scrollIntoView({block: 'center', inline: 'nearest', behavior})
  } catch {
    /* ignore */
  }
}

let lastCurrentMark: HTMLElement | null = null

/**
 * Move "current" highlight. Cheap for CSS path (one Highlight replace).
 * Pass scroll:false while the user is still typing.
 */
export function focusChatFindHit(
  hits: ChatFindHit[],
  currentIndex: number,
  opts?: {scroll?: boolean; smooth?: boolean},
): void {
  if (hits.length === 0) return
  const i = ((currentIndex % hits.length) + hits.length) % hits.length
  const hit = hits[i]
  const scroll = opts?.scroll !== false
  const smooth = Boolean(opts?.smooth)

  if (hit.kind === 'range') {
    const map = cssHighlights()
    const H = HighlightClass()
    if (map && H) {
      map.set(HL_CUR, new H(hit.range))
    }
  } else {
    if (lastCurrentMark && lastCurrentMark !== hit.el) {
      lastCurrentMark.classList.remove(FIND_CURRENT_CLASS)
    }
    hit.el.classList.add(FIND_CURRENT_CLASS)
    lastCurrentMark = hit.el
  }

  if (scroll) scrollHitIntoView(hit, smooth)
}

/** @deprecated use focusChatFindHit */
export function focusChatFindMark(
  marks: HTMLElement[],
  currentIndex: number,
  opts?: {scroll?: boolean; smooth?: boolean},
): void {
  focusChatFindHit(
    marks.map((el) => ({kind: 'mark' as const, el})),
    currentIndex,
    opts,
  )
}
