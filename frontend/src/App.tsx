import {FormEvent, MouseEvent as ReactMouseEvent, useCallback, useEffect, useRef, useState} from 'react'
import {Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import './App.css'
import {
  AppInfo,
  ChatOnce,
  ClearChat,
  DeleteChatSession,
  GetSettings,
  GetCursorRules,
  GetUsageStats,
  ListChatSessions,
  ListDir,
  NewChatSession,
  OpenProject,
  PickProjectDir,
  ReadFile,
  ReloadCursorRules,
  RenameChatSession,
  RunAgentWithAttachments,
  RunShell,
  SaveDeepSeekKey,
  SaveDeepSeekModel,
  SaveAgentMaxSteps,
  SaveComposerHeight,
  SaveShowTerminal,
  SaveShowFiles,
  SaveShowSettings,
  SaveLayoutSizes,
  SaveTheme,
  SaveChat,
  SaveChatSession,
  LoadChat,
  StartTerminal,
  StopAgent,
  StopTerminal,
  SwitchChatSession,
  TerminalWrite,
  ListProjects,
} from '../wailsjs/go/main/App'
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime'
import BrandMark from './BrandMark'
import Markdown from './Markdown'

type Project = { name: string; path: string; opened?: string }
type FileEntry = { name: string; path: string; isDir: boolean }
type ChatItem =
  | { kind: 'user' | 'assistant' | 'system' | 'reasoning'; content: string; attachments?: ChatAttPreview[] }
  | { kind: 'tool'; name: string; args: string; result?: string; phase: 'running' | 'done'; ok?: boolean }
  | { kind: 'file'; path: string; content: string }

type ChatAttPreview = {
  name: string
  mime?: string
  isImage: boolean
  dataUrl?: string
}

type PendingAtt = ChatAttPreview & {
  id: string
  text?: string
}

type ChatSessionMeta = {
  id: string
  title: string
}

type AgentEvent = {
  type: string
  content?: string
  name?: string
  ok?: boolean
  sessionId?: string
}

type UsageSnapshot = {
  costUsd: number
  inputTokens: number
  outputTokens: number
  chatCostUsd: number
  chatInputTokens: number
  chatOutputTokens: number
}

type RuleInfo = {
  source: string
  path: string
  name: string
  description: string
  alwaysApply: boolean
  globs: string
  content: string
}

type RulesBundle = {
  globalDir: string
  projectDir: string
  global: RuleInfo[]
  project: RuleInfo[]
}

const emptyUsage: UsageSnapshot = {
  costUsd: 0,
  inputTokens: 0,
  outputTokens: 0,
  chatCostUsd: 0,
  chatInputTokens: 0,
  chatOutputTokens: 0,
}

function asList<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
}

function normalizeRules(b: Partial<RulesBundle> | null | undefined): RulesBundle {
  return {
    globalDir: String(b?.globalDir || ''),
    projectDir: String(b?.projectDir || ''),
    global: asList(b?.global),
    project: asList(b?.project),
  }
}

function pickNum(u: Record<string, unknown>, ...keys: string[]): number {
  for (const k of keys) {
    const v = u[k]
    if (v == null || v === '') continue
    const n = typeof v === 'number' ? v : Number(v)
    if (Number.isFinite(n)) return n
  }
  return 0
}

function eventText(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'string') return v
  if (typeof v === 'object') {
    try {
      return JSON.stringify(v)
    } catch {
      return ''
    }
  }
  return String(v)
}

function usageFromUnknown(raw: unknown): UsageSnapshot | null {
  let u: Record<string, unknown> | null = null
  if (typeof raw === 'string') {
    const t = raw.trim()
    if (!t || t === '[object Object]') return null
    try {
      const parsed = JSON.parse(t) as unknown
      if (parsed && typeof parsed === 'object') u = parsed as Record<string, unknown>
    } catch {
      return null
    }
  } else if (raw && typeof raw === 'object') {
    u = raw as Record<string, unknown>
  }
  if (!u) return null
  return {
    costUsd: pickNum(u, 'costUsd', 'CostUsd', 'CostUSD'),
    inputTokens: pickNum(u, 'inputTokens', 'InputTokens'),
    outputTokens: pickNum(u, 'outputTokens', 'OutputTokens'),
    chatCostUsd: pickNum(u, 'chatCostUsd', 'ChatCostUsd', 'ChatCostUSD'),
    chatInputTokens: pickNum(u, 'chatInputTokens', 'ChatInputTokens'),
    chatOutputTokens: pickNum(u, 'chatOutputTokens', 'ChatOutputTokens'),
  }
}

function previewLen(text: string, max = 160): string {
  const t = text.replace(/\s+/g, ' ').trim()
  if (t.length <= max) return t
  return t.slice(0, max) + '…'
}

function fmtUsd(c: number): string {
  return `$${Number(c || 0).toFixed(4)}`
}

function toolTitle(name: string, phase: 'running' | 'done', ok?: boolean): string {
  const labels: Record<string, string> = {
    read_file: 'Reading file',
    write_file: 'Writing file',
    list_dir: 'Listing directory',
    search_files: 'Searching files',
    run_terminal: 'Running terminal',
    git_status: 'Git status',
    git_diff: 'Git diff',
    git_commit: 'Git commit',
    git_push: 'Git push',
    ssh_exec: 'SSH exec',
    ssh_keygen: 'SSH keygen',
  }
  const base = labels[name] || name
  if (phase === 'running') return `${base}…`
  if (ok === false) return `${base} failed`
  return base
}

function ToolCard({item}: {item: Extract<ChatItem, {kind: 'tool'}>}) {
  const title = toolTitle(item.name, item.phase, item.ok)
  const hasBody = Boolean(item.args || item.result)
  return (
    <div className={`nc-msg tool ${item.phase}${item.ok === false ? ' fail' : ''}`}>
      <div className="nc-tool-line">
        <span className="nc-tool-icon">{item.phase === 'running' ? '◉' : item.ok === false ? '✗' : '✓'}</span>
        <span className="nc-tool-title">{title}</span>
        <span className="nc-tool-name">{item.name}</span>
      </div>
      {hasBody && (
        <details className="nc-tool-details">
          <summary>подробности</summary>
          {item.args ? <pre className="nc-tool-pre"><span className="nc-k">args</span>{'\n'}{item.args}</pre> : null}
          {item.result != null ? <pre className="nc-tool-pre"><span className="nc-k">result</span>{'\n'}{item.result}</pre> : null}
        </details>
      )}
    </div>
  )
}

function summarizeTools(tools: Extract<ChatItem, {kind: 'tool'}>[]): string {
  const counts: Record<string, number> = {}
  let failed = 0
  for (const t of tools) {
    counts[t.name] = (counts[t.name] || 0) + 1
    if (t.ok === false) failed++
  }
  const labels: Record<string, [string, string]> = {
    read_file: ['file read', 'files read'],
    list_dir: ['dir listed', 'dirs listed'],
    write_file: ['file written', 'files written'],
    search_files: ['search', 'searches'],
    run_terminal: ['command', 'commands'],
    git_status: ['git status', 'git status'],
    git_diff: ['git diff', 'git diffs'],
    git_commit: ['commit', 'commits'],
    git_push: ['push', 'pushes'],
    ssh_exec: ['ssh', 'ssh'],
    ssh_keygen: ['key', 'keys'],
  }
  const parts: string[] = []
  for (const [name, n] of Object.entries(counts)) {
    const [one, many] = labels[name] || [name, name]
    parts.push(`${n} ${n === 1 ? one : many}`)
  }
  let s = `Explored · ${parts.join(', ')}`
  if (failed) s += ` · ${failed} failed`
  return s
}

type DisplayRow =
  | { key: string; kind: 'item'; item: ChatItem }
  | { key: string; kind: 'tool_group'; tools: Extract<ChatItem, {kind: 'tool'}>[]; summary: string }
  | { key: string; kind: 'process'; rows: DisplayRow[] }

function ToolGroup({summary, tools}: {summary: string; tools: Extract<ChatItem, {kind: 'tool'}>[]}) {
  return (
    <details className="nc-msg tool-group">
      <summary className="nc-tool-group-sum">
        <span className="nc-tool-icon">✓</span>
        <span className="nc-tool-title">{summary}</span>
        <span className="nc-tool-name">{tools.length} steps</span>
      </summary>
      <div className="nc-tool-group-body">
        {tools.map((t, idx) => <ToolCard key={idx} item={t} />)}
      </div>
    </details>
  )
}

function ThinkingBlock({content, collapsed}: {content: string; collapsed?: boolean}) {
  const [open, setOpen] = useState(!collapsed)
  const prevCollapsed = useRef(collapsed)
  useEffect(() => {
    if (collapsed !== prevCollapsed.current) {
      prevCollapsed.current = collapsed
      setOpen(!collapsed)
    }
  }, [collapsed])
  return (
    <details
      className="nc-msg reasoning"
      open={open}
      onToggle={(e) => setOpen((e.currentTarget as HTMLDetailsElement).open)}
    >
      <summary className="nc-think-sum">Thinking</summary>
      <pre className="nc-think-body">{content}</pre>
    </details>
  )
}

/** Collapse consecutive completed tools; when compact, even a single tool becomes a summary group. */
function buildDisplayRows(items: ChatItem[], compact = false): DisplayRow[] {
  const rows: DisplayRow[] = []
  let i = 0
  const list = asList(items)
  while (i < list.length) {
    const m = list[i]
    if (m.kind === 'tool' && m.phase === 'done') {
      const group: Extract<ChatItem, {kind: 'tool'}>[] = []
      while (i < list.length && list[i].kind === 'tool' && (list[i] as Extract<ChatItem, {kind: 'tool'}>).phase === 'done') {
        group.push(list[i] as Extract<ChatItem, {kind: 'tool'}>)
        i++
      }
      if (!compact && group.length === 1) {
        rows.push({key: `t-${i - 1}`, kind: 'item', item: group[0]})
      } else {
        rows.push({key: `g-${i - group.length}`, kind: 'tool_group', tools: group, summary: summarizeTools(group)})
      }
      continue
    }
    rows.push({key: `i-${i}`, kind: 'item', item: m})
    i++
  }
  return collapseProcess(rows, compact)
}

function isProcessRow(r: DisplayRow): boolean {
  return r.kind === 'tool_group' || (r.kind === 'item' && r.item.kind === 'reasoning')
}

/** Merge consecutive Thinking + Explored rows into one block once the run is done. */
function collapseProcess(rows: DisplayRow[], compact: boolean): DisplayRow[] {
  if (!compact) return rows
  const out: DisplayRow[] = []
  let i = 0
  while (i < rows.length) {
    if (isProcessRow(rows[i])) {
      const group: DisplayRow[] = []
      while (i < rows.length && isProcessRow(rows[i])) {
        group.push(rows[i])
        i++
      }
      out.push({key: `proc-${i}`, kind: 'process', rows: group})
      continue
    }
    out.push(rows[i])
    i++
  }
  return out
}

function summarizeProcess(rows: DisplayRow[]): {title: string; steps: number} {
  let reasoning = 0
  let steps = 0
  for (const r of rows) {
    if (r.kind === 'tool_group') {
      steps += r.tools.length
    } else if (r.kind === 'item' && r.item.kind === 'reasoning') {
      reasoning++
    }
  }
  let title = 'Thinking'
  if (steps > 0) title = reasoning > 0 ? 'Thinking & Explored' : 'Explored'
  return {title, steps}
}

function ProcessGroup({rows}: {rows: DisplayRow[]}) {
  const {title, steps} = summarizeProcess(rows)
  return (
    <details className="nc-msg process-group">
      <summary className="nc-tool-group-sum">
        <span className="nc-tool-icon">✓</span>
        <span className="nc-tool-title">{title}</span>
        <span className="nc-tool-name">{steps > 0 ? `${steps} steps` : 'thought'}</span>
      </summary>
      <div className="nc-process-body">
        {rows.map((r, i) => {
          if (r.kind === 'tool_group') {
            return <ToolGroup key={i} summary={r.summary} tools={r.tools} />
          }
          if (r.kind === 'item' && r.item.kind === 'reasoning') {
            return <ThinkingBlock key={i} content={r.item.content} collapsed />
          }
          return null
        })}
      </div>
    </details>
  )
}

function parseAgentEvent(...args: unknown[]): AgentEvent | null {
  for (const arg of args) {
    let raw: unknown = arg
    if (typeof raw === 'string') {
      try {
        raw = JSON.parse(raw)
      } catch {
        continue
      }
    }
    if (!raw || typeof raw !== 'object') continue
    const o = raw as Record<string, unknown>
    const inner = o.data && typeof o.data === 'object' ? (o.data as Record<string, unknown>) : o
    const type = String(inner.type ?? inner.Type ?? '')
    if (!type) continue
    return {
      type,
      content: eventText(inner.content ?? inner.Content),
      name: inner.name != null ? String(inner.name) : inner.Name != null ? String(inner.Name) : '',
      ok: Boolean(inner.ok ?? inner.OK),
      sessionId: eventText(inner.sessionId ?? inner.SessionID ?? inner.SessionId),
    }
  }
  return null
}

export default function App() {
  const [info, setInfo] = useState({name: 'NotCursor.ai', version: '0.2.1'})
  const [usage, setUsage] = useState<UsageSnapshot>(emptyUsage)
  const [rulesInfo, setRulesInfo] = useState<RulesBundle>({globalDir: '', projectDir: '', global: [], project: []})
  const [projects, setProjects] = useState<Project[]>([])
  const [active, setActive] = useState<Project | null>(null)
  const [files, setFiles] = useState<FileEntry[]>([])
  const [expanded, setExpanded] = useState<Record<string, FileEntry[]>>({})
  const [showTree, setShowTree] = useState(true)
  const [showTerm, setShowTerm] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [layout, setLayout] = useState({
    projectsW: 200,
    treeW: 220,
    settingsW: 230,
    terminalH: 160,
    composerH: 150,
  })
  const layoutRef = useRef(layout)
  const [sessions, setSessions] = useState<ChatSessionMeta[]>([])
  const [activeSessionId, setActiveSessionId] = useState('')
  const [itemsBySession, setItemsBySession] = useState<Record<string, ChatItem[]>>({})
  const [busyBySession, setBusyBySession] = useState<Record<string, boolean>>({})
  const [pendingAtts, setPendingAtts] = useState<PendingAtt[]>([])
  const [dragOver, setDragOver] = useState(false)
  const [retryVisible, setRetryVisible] = useState(false)
  const [editingTabId, setEditingTabId] = useState('')
  const [editingTitle, setEditingTitle] = useState('')
  const [input, setInput] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('deepseek-v4-flash')
  const [maxSteps, setMaxSteps] = useState(40)
  const [keySet, setKeySet] = useState(false)
  const [theme, setTheme] = useState<'dark' | 'light'>('dark')
  const [termCmd, setTermCmd] = useState('')
  const chatRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const xtermRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const assistantBuf = useRef<Record<string, string>>({})
  const activeSessionRef = useRef('')
  const lastRequestRef = useRef<{text: string; atts: PendingAtt[]} | null>(null)

  const items = asList(activeSessionId ? itemsBySession[activeSessionId] : undefined)
  const busy = Boolean(activeSessionId && busyBySession[activeSessionId])

  useEffect(() => {
    activeSessionRef.current = activeSessionId
  }, [activeSessionId])

  const setSessionItems = useCallback((sessionId: string, updater: (prev: ChatItem[]) => ChatItem[]) => {
    if (!sessionId) return
    setItemsBySession((prev) => ({
      ...prev,
      [sessionId]: updater(asList(prev[sessionId])),
    }))
  }, [])

  const refreshFiles = useCallback(async (root = '.') => {
    try {
      setFiles(asList(await ListDir(root)))
    } catch {
      setFiles([])
    }
  }, [])

  const applyUsageStats = useCallback(async () => {
    try {
      const next = usageFromUnknown(await GetUsageStats())
      if (next) setUsage(next)
    } catch {
      /* ignore */
    }
  }, [])

  async function refreshRules() {
    try {
      const b = await GetCursorRules()
      setRulesInfo(normalizeRules(b))
    } catch {
      setRulesInfo(normalizeRules(null))
    }
  }

  useEffect(() => {
    layoutRef.current = layout
  }, [layout])

  const persistLayout = useCallback((next = layoutRef.current) => {
    SaveLayoutSizes(next.projectsW, next.treeW, next.settingsW, next.terminalH).catch(() => undefined)
    SaveComposerHeight(next.composerH).catch(() => undefined)
  }, [])

  const beginResize = useCallback((kind: 'projects' | 'tree' | 'settings' | 'terminal' | 'composer', e: ReactMouseEvent) => {
    e.preventDefault()
    const startX = e.clientX
    const startY = e.clientY
    const start = layoutRef.current
    const clamp = (v: number, min: number, max: number) => Math.max(min, Math.min(max, Math.round(v)))
    const onMove = (ev: MouseEvent) => {
      let next = layoutRef.current
      if (kind === 'projects') {
        next = {...start, projectsW: clamp(start.projectsW + (ev.clientX - startX), 140, 420)}
      } else if (kind === 'tree') {
        next = {...start, treeW: clamp(start.treeW + (ev.clientX - startX), 140, 480)}
      } else if (kind === 'settings') {
        next = {...start, settingsW: clamp(start.settingsW - (ev.clientX - startX), 180, 420)}
      } else if (kind === 'composer') {
        next = {...start, composerH: clamp(start.composerH - (ev.clientY - startY), 110, 480)}
      } else {
        next = {...start, terminalH: clamp(start.terminalH - (ev.clientY - startY), 90, 480)}
      }
      layoutRef.current = next
      setLayout(next)
    }
    const onUp = () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      persistLayout(layoutRef.current)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }, [persistLayout])

  const setSettingsVisible = useCallback((next: boolean | ((v: boolean) => boolean)) => {
    setShowSettings((prev) => {
      const value = typeof next === 'function' ? next(prev) : next
      SaveShowSettings(value).catch(() => undefined)
      return value
    })
  }, [])

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
  }, [theme])

  const applyTheme = useCallback(async (next: 'dark' | 'light') => {
    setTheme(next)
    document.documentElement.setAttribute('data-theme', next)
    try {
      await SaveTheme(next)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    try {
      AppInfo().then((v) => setInfo(v as typeof info)).catch(() => undefined)
      void applyUsageStats()
      GetSettings().then((s) => {
        if (!s) return
        setKeySet(Boolean(s.deepseekKeySet))
        if (typeof s.deepseekModel === 'string' && s.deepseekModel) setModel(s.deepseekModel)
        if (typeof s.agentMaxSteps === 'number' && s.agentMaxSteps > 0) setMaxSteps(s.agentMaxSteps)
        setShowTerm(Boolean(s.showTerminal))
        if (typeof s.showFiles === 'boolean') setShowTree(s.showFiles)
        if (typeof s.showSettings === 'boolean') setShowSettings(s.showSettings)
        if (s.theme === 'light' || s.theme === 'dark') {
          setTheme(s.theme)
          document.documentElement.setAttribute('data-theme', s.theme)
        }
        setLayout({
          projectsW: Number(s.layoutProjectsW) || 200,
          treeW: Number(s.layoutTreeW) || 220,
          settingsW: Number(s.layoutSettingsW) || 230,
          terminalH: Number(s.layoutTerminalH) || 160,
          composerH: Number(s.layoutComposerH) || 150,
        })
      }).catch(() => undefined)
      ListProjects().then((v) => setProjects(asList(v))).catch(() => undefined)
      GetCursorRules().then((b) => setRulesInfo(normalizeRules(b))).catch(() => undefined)
    } catch {
      // window.go / runtime may be missing until Wails injects bindings
    }

    let offAgent: (() => void) | undefined
    let offTerm: (() => void) | undefined
    try {
      offAgent = EventsOn('agent:event', (...args: unknown[]) => {
        try {
          const ev = parseAgentEvent(...args)
          if (!ev) return
          if (ev.type === 'usage') {
            const next = usageFromUnknown(ev.content)
            if (next) setUsage(next)
            else void applyUsageStats()
            return
          }
          const sid = ev.sessionId || activeSessionRef.current
          if (!sid) return
          if (ev.type === 'delta') {
            assistantBuf.current[sid] = (assistantBuf.current[sid] || '') + (ev.content || '')
            const text = assistantBuf.current[sid]
            setSessionItems(sid, (prev) => {
              const copy = [...prev]
              const last = copy[copy.length - 1]
              if (last && last.kind === 'assistant') {
                copy[copy.length - 1] = {kind: 'assistant', content: text}
                return copy
              }
              return [...copy, {kind: 'assistant', content: text}]
            })
          } else if (ev.type === 'reasoning') {
            setSessionItems(sid, (prev) => {
              const copy = [...prev]
              const last = copy[copy.length - 1]
              if (last && last.kind === 'reasoning') {
                copy[copy.length - 1] = {kind: 'reasoning', content: last.content + (ev.content || '')}
                return copy
              }
              return [...copy, {kind: 'reasoning', content: ev.content || ''}]
            })
          } else if (ev.type === 'tool_start') {
            assistantBuf.current[sid] = ''
            setSessionItems(sid, (prev) => [...prev, {
              kind: 'tool',
              name: ev.name || 'tool',
              args: ev.content || '',
              phase: 'running',
            }])
          } else if (ev.type === 'tool_end') {
            setSessionItems(sid, (prev) => {
              const copy = [...prev]
              for (let i = copy.length - 1; i >= 0; i--) {
                const it = copy[i]
                if (it.kind === 'tool' && it.phase === 'running' && it.name === (ev.name || it.name)) {
                  copy[i] = {
                    kind: 'tool',
                    name: it.name,
                    args: it.args,
                    result: ev.content || '',
                    phase: 'done',
                    ok: ev.ok,
                  }
                  return copy
                }
              }
              return [...copy, {
                kind: 'tool',
                name: ev.name || 'tool',
                args: '',
                result: ev.content || '',
                phase: 'done',
                ok: ev.ok,
              }]
            })
          } else if (ev.type === 'reconnect') {
            setSessionItems(sid, (prev) => [...prev, {kind: 'system', content: ev.content || 'reconnecting…'}])
          } else if (ev.type === 'done' || ev.type === 'persist') {
            assistantBuf.current[sid] = ''
            setBusyBySession((b) => ({...b, [sid]: false}))
            if (sid === activeSessionRef.current) setRetryVisible(false)
            void applyUsageStats()
          } else if (ev.type === 'error') {
            assistantBuf.current[sid] = ''
            setBusyBySession((b) => ({...b, [sid]: false}))
            setSessionItems(sid, (prev) => [...prev, {kind: 'system', content: `Error: ${ev.content || 'unknown'}`}])
            if (sid === activeSessionRef.current) setRetryVisible(true)
          }
        } catch (err) {
          console.error('agent event', err)
        }
      })

      offTerm = EventsOn('terminal:data', (data: string) => {
        try {
          if (typeof data === 'string') xtermRef.current?.write(data)
        } catch {
          /* ignore */
        }
      })
    } catch {
      // runtime bindings not ready
    }

    return () => {
      try {
        EventsOff('agent:event')
        EventsOff('terminal:data')
        offAgent?.()
        offTerm?.()
      } catch {
        /* ignore */
      }
    }
  }, [setSessionItems, applyUsageStats])

  useEffect(() => {
    const el = chatRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [items, busy, activeSessionId])

  // Persist active chat after quiet period
  useEffect(() => {
    if (!active || !activeSessionId) return
    const t = window.setTimeout(() => {
      SaveChatSession(activeSessionId, JSON.stringify(items)).catch(() => undefined)
    }, 600)
    return () => window.clearTimeout(t)
  }, [items, active, activeSessionId])

  async function refreshSessions() {
    try {
      const bundle = await ListChatSessions()
      const list = asList(bundle?.sessions).map((s) => ({
        id: String(s.id),
        title: String(s.title || 'Chat'),
      }))
      setSessions(list)
      const aid = String(bundle?.activeId || list[0]?.id || '')
      if (aid) setActiveSessionId(aid)
      return {list, activeId: aid, bundle}
    } catch {
      return {list: [] as ChatSessionMeta[], activeId: '', bundle: null}
    }
  }

  async function restoreChat(projectPath: string) {
    try {
      const {list, activeId} = await refreshSessions()
      const raw = await LoadChat(projectPath)
      const parsed = JSON.parse(raw || '[]')
      const chatItems: ChatItem[] = Array.isArray(parsed) && parsed.length > 0
        ? parsed as ChatItem[]
        : [{kind: 'system', content: 'Чат с агентом. История пуста — задайте задачу. Можно вставить скриншот (Ctrl+V) или перетащить файл.'}]
      if (activeId) {
        setItemsBySession((prev) => ({...prev, [activeId]: chatItems}))
      } else if (list[0]) {
        setItemsBySession((prev) => ({...prev, [list[0].id]: chatItems}))
      }
      await applyUsageStats()
    } catch {
      setItemsBySession({})
      setSessions([])
      setActiveSessionId('')
    }
  }

  async function switchSession(id: string) {
    if (!id || id === activeSessionId) return
    if (activeSessionId) {
      try {
        await SaveChatSession(activeSessionId, JSON.stringify(items))
      } catch { /* ignore */ }
    }
    try {
      const raw = await SwitchChatSession(id)
      const parsed = JSON.parse(raw || '[]')
      const chatItems: ChatItem[] = Array.isArray(parsed) && parsed.length > 0
        ? parsed as ChatItem[]
        : [{kind: 'system', content: 'Новый чат. Задайте задачу или вставьте файл/скриншот.'}]
      setActiveSessionId(id)
      setRetryVisible(false)
      setItemsBySession((prev) => ({...prev, [id]: chatItems}))
      setSessions((prev) => prev.map((s) => s.id === id ? s : s))
      await refreshSessions()
      setActiveSessionId(id)
      await applyUsageStats()
    } catch (e) {
      setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function createSession() {
    if (activeSessionId) {
      try { await SaveChatSession(activeSessionId, JSON.stringify(items)) } catch { /* ignore */ }
    }
    const sess = await NewChatSession('')
    const id = String(sess.id)
    const empty: ChatItem[] = [{kind: 'system', content: 'Новый чат. Можно вести параллельные задачи в разных вкладках.'}]
    setSessions((prev) => [...prev, {id, title: String(sess.title || 'Chat')}])
    setItemsBySession((prev) => ({...prev, [id]: empty}))
    setActiveSessionId(id)
    await refreshSessions()
    setActiveSessionId(id)
    await applyUsageStats()
  }

  function startRename(id: string, title: string) {
    setEditingTabId(id)
    setEditingTitle(title || 'Chat')
  }

  async function commitRename(id: string) {
    const title = editingTitle.trim()
    setEditingTabId('')
    if (!title) return
    setSessions((prev) => prev.map((s) => s.id === id ? {...s, title} : s))
    try {
      await RenameChatSession(id, title)
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function removeSession(id: string) {
    if (sessions.length <= 1) {
      await ClearChat()
      const empty: ChatItem[] = [{kind: 'system', content: 'Чат очищен'}]
      setSessionItems(id, () => empty)
      return
    }
    const raw = await DeleteChatSession(id)
    const parsed = JSON.parse(raw || '[]')
    const {activeId} = await refreshSessions()
    const chatItems: ChatItem[] = Array.isArray(parsed) && parsed.length > 0
      ? parsed as ChatItem[]
      : [{kind: 'system', content: 'Чат с агентом.'}]
    if (activeId) {
      setItemsBySession((prev) => {
        const next = {...prev}
        delete next[id]
        next[activeId] = chatItems
        return next
      })
      setActiveSessionId(activeId)
    }
  }

  function readFileAsPending(file: File): Promise<PendingAtt> {
    return new Promise((resolve, reject) => {
      const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      const isImage = file.type.startsWith('image/') || /\.(png|jpe?g|gif|webp|bmp)$/i.test(file.name)
      if (isImage) {
        const reader = new FileReader()
        reader.onload = () => resolve({
          id,
          name: file.name || 'image.png',
          mime: file.type || 'image/png',
          isImage: true,
          dataUrl: String(reader.result || ''),
        })
        reader.onerror = () => reject(reader.error)
        reader.readAsDataURL(file)
        return
      }
      // text-ish files inline; binary kept as name-only hint
      const textLike = file.type.startsWith('text/')
        || /\.(txt|md|json|ya?ml|toml|csv|go|ts|tsx|js|jsx|py|rs|java|c|cpp|h|cs|html|css|xml|sql|sh|ps1|log)$/i.test(file.name)
        || file.type === 'application/json'
        || file.type === 'application/xml'
      if (textLike && file.size <= 512_000) {
        const reader = new FileReader()
        reader.onload = () => resolve({
          id,
          name: file.name || 'file.txt',
          mime: file.type || 'text/plain',
          isImage: false,
          text: String(reader.result || ''),
        })
        reader.onerror = () => reject(reader.error)
        reader.readAsText(file)
        return
      }
      resolve({
        id,
        name: file.name || 'file.bin',
        mime: file.type || 'application/octet-stream',
        isImage: false,
        text: `(binary file ${file.name}, ${file.size} bytes — save into the workspace and use tools)`,
      })
    })
  }

  async function addFiles(fileList: FileList | File[]) {
    const arr = Array.from(fileList || [])
    if (!arr.length) return
    const next: PendingAtt[] = []
    for (const f of arr) {
      try {
        next.push(await readFileAsPending(f))
      } catch {
        /* skip bad file */
      }
    }
    if (next.length) setPendingAtts((p) => [...p, ...next])
  }

  useEffect(() => {
    void restoreChat('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!showTerm) {
      StopTerminal().catch(() => undefined)
      if (xtermRef.current) {
        xtermRef.current.dispose()
        xtermRef.current = null
        fitRef.current = null
      }
      return
    }
    if (!termRef.current || xtermRef.current) {
      requestAnimationFrame(() => {
        try {
          fitRef.current?.fit()
        } catch {
          /* ignore */
        }
      })
      return
    }
    try {
      const term = new Terminal({
        convertEol: true,
        fontSize: 13,
        fontFamily: 'Menlo, Consolas, "Courier New", monospace',
        theme: {background: '#0d0d0d', foreground: '#c8f7c5'},
      })
      const fit = new FitAddon()
      term.loadAddon(fit)
      term.open(termRef.current)
      try {
        fit.fit()
      } catch {
        /* container may still be 0-sized */
      }
      term.onData((data) => {
        TerminalWrite(data).catch(() => undefined)
      })
      xtermRef.current = term
      fitRef.current = fit
      StartTerminal().catch(() => undefined)
      const onResize = () => {
        try {
          fit.fit()
        } catch {
          /* ignore */
        }
      }
      window.addEventListener('resize', onResize)
      return () => window.removeEventListener('resize', onResize)
    } catch (err) {
      console.error('terminal init failed', err)
    }
  }, [showTerm])

  async function openPicked() {
    try {
      const dir = await PickProjectDir()
      if (!dir) return
      await openProject(dir)
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function openProject(path: string) {
    if (active && activeSessionId) {
      try { await SaveChatSession(activeSessionId, JSON.stringify(items)) } catch { /* ignore */ }
    }
    const p = await OpenProject(path)
    setActive(p)
    setProjects(asList(await ListProjects()))
    await refreshFiles('.')
    await restoreChat(p.path)
    void refreshRules()
    if (showTerm) {
      xtermRef.current?.writeln(`\r\n$ cd ${p.path}`)
      await StartTerminal()
    }
  }

  async function toggleDir(path: string) {
    if (expanded[path]) {
      setExpanded((e) => {
        const n = {...e}
        delete n[path]
        return n
      })
      return
    }
    const kids = asList(await ListDir(path))
    setExpanded((e) => ({...e, [path]: kids}))
  }

  async function openFile(path: string) {
    try {
      const content = await ReadFile(path)
      const preview = content.length > 4000 ? content.slice(0, 4000) + '\n…' : content
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'file', path, content: preview}])
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function saveSettings() {
    if (apiKey.trim()) {
      await SaveDeepSeekKey(apiKey.trim())
      setApiKey('')
      setKeySet(true)
    }
    await SaveDeepSeekModel(model.trim() || 'deepseek-v4-flash')
    await SaveAgentMaxSteps(Number(maxSteps) || 40)
    await SaveShowTerminal(showTerm)
    await SaveTheme(theme)
    if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: 'Settings saved'}])
  }

  async function toggleTerminal(next: boolean) {
    setShowTerm(next)
    try {
      await SaveShowTerminal(next)
    } catch {
      /* ignore */
    }
  }

  async function testConnect() {
    try {
      const r = await ChatOnce('ping')
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: `DeepSeek connect OK: ${r || '(empty content)'}`}])
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: `Connect failed: ${String(e)}`}])
    }
  }

  async function runAgent(text: string, atts: PendingAtt[], skipUserMessage = false) {
    if ((!text && atts.length === 0) || busy || !activeSessionId) return
    lastRequestRef.current = {text, atts}
    setRetryVisible(false)
    assistantBuf.current[activeSessionId] = ''
    const previewAtts: ChatAttPreview[] = atts.map((a) => ({
      name: a.name,
      mime: a.mime,
      isImage: a.isImage,
      dataUrl: a.isImage ? a.dataUrl : undefined,
    }))
    if (!skipUserMessage) {
      setSessionItems(activeSessionId, (m) => [...m, {
        kind: 'user',
        content: text || (atts.some((a) => a.isImage) ? '(изображение)' : '(файл)'),
        attachments: previewAtts.length ? previewAtts : undefined,
      }])
    }
    setBusyBySession((b) => ({...b, [activeSessionId]: true}))
    try {
      await RunAgentWithAttachments(text, atts.map((a) => ({
        name: a.name,
        mime: a.mime || '',
        dataUrl: a.dataUrl || '',
        text: a.text || '',
        isImage: a.isImage,
      })))
    } catch (err) {
      setBusyBySession((b) => ({...b, [activeSessionId]: false}))
      setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(err)}])
      setRetryVisible(true)
    }
  }

  async function sendChat(e?: FormEvent) {
    e?.preventDefault()
    const text = input.trim()
    const atts = pendingAtts
    setInput('')
    setPendingAtts([])
    await runAgent(text, atts)
  }

  async function retryLast() {
    const req = lastRequestRef.current
    if (!req) return
    await runAgent(req.text, req.atts, true)
  }

  async function runOneShot() {
    if (!termCmd.trim()) return
    const res = await RunShell(termCmd.trim())
    xtermRef.current?.writeln(`\r\n> ${termCmd}`)
    if (res.stdout) xtermRef.current?.writeln(String(res.stdout))
    if (res.stderr) xtermRef.current?.writeln(String(res.stderr))
    xtermRef.current?.writeln(`[exit ${res.exitCode}]`)
    setTermCmd('')
  }

  function renderTree(entries: FileEntry[], depth = 0) {
    return asList(entries).map((f) => (
      <div key={f.path} className="tree-row" style={{paddingLeft: 8 + depth * 12}}>
        {f.isDir ? (
          <button type="button" className="tree-btn" onClick={() => toggleDir(f.path)}>
            {expanded[f.path] ? '▾' : '▸'} {f.name}
          </button>
        ) : (
          <button type="button" className="tree-btn file" onClick={() => openFile(f.path)}>
            {f.name}
          </button>
        )}
        {f.isDir && expanded[f.path] && renderTree(expanded[f.path], depth + 1)}
      </div>
    ))
  }

  return (
    <div className="nc-app" data-theme={theme}>
      <header className="nc-topbar">
        <BrandMark size={34} />
        <strong>{info.name}</strong>
        <span className="nc-sub">v{info.version}</span>
        <span className="nc-top-sep" />
        <label className="nc-top-field">
          API key
          <input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={keySet ? '•••• set' : 'sk-...'} />
        </label>
        <label className="nc-top-field">
          Model
          <select value={model} onChange={(e) => setModel(e.target.value)}>
            <option value="deepseek-v4-flash">deepseek-v4-flash</option>
            <option value="deepseek-v4-pro">deepseek-v4-pro</option>
            <option value="deepseek-v4-flash-vision-exp">deepseek-v4-flash-vision-exp</option>
          </select>
        </label>
        <button type="button" onClick={saveSettings}>Save</button>
        <span className={`nc-pill ${keySet ? 'ok' : ''}`}>{keySet ? 'key OK' : 'no key'}</span>
        <span
          className="nc-cost"
          title={`Этот чат: ${usage.chatInputTokens} in / ${usage.chatOutputTokens} out (${fmtUsd(usage.chatCostUsd)}) · Всего на этом компьютере: ${usage.inputTokens} in / ${usage.outputTokens} out (${fmtUsd(usage.costUsd)})`}
        >
          <span className="nc-cost-chat">chat {fmtUsd(usage.chatCostUsd)}</span>
          <span className="nc-cost-sep">·</span>
          <span className="nc-cost-total">total {fmtUsd(usage.costUsd)}</span>
        </span>
        <button type="button" className="nc-ghost" onClick={() => setSettingsVisible((v) => !v)}>
          {showSettings ? 'Hide settings' : 'Settings'}
        </button>
      </header>
      <div
        className={`nc-root ${showTree ? '' : 'no-tree'} ${showSettings ? '' : 'no-settings'}`}
        style={{
          ['--layout-projects' as string]: `${layout.projectsW}px`,
          ['--layout-tree' as string]: `${layout.treeW}px`,
          ['--layout-settings' as string]: `${layout.settingsW}px`,
        }}
      >
        <aside className="nc-projects">
          <button type="button" onClick={openPicked}>Open Project…</button>
          <div className="nc-section-label">Projects</div>
          <ul className="nc-list">
            {asList(projects).length === 0 && (
              <li className="nc-empty">Нет проектов — нажмите Open Project…</li>
            )}
            {asList(projects).map((p) => (
              <li key={p.path} className={active?.path === p.path ? 'active' : ''}>
                <button type="button" onClick={() => openProject(p.path)}>{p.name}</button>
              </li>
            ))}
          </ul>
          <button type="button" className="nc-ghost" onClick={() => {
            setShowTree((v) => {
              const next = !v
              SaveShowFiles(next).catch(() => undefined)
              return next
            })
          }}>
            {showTree ? 'Hide files' : 'Show files'}
          </button>
          <div className="nc-vsplit" onMouseDown={(e) => beginResize('projects', e)} />
        </aside>

        {showTree && (
          <aside className="nc-tree">
            <div className="nc-section-label">{active?.name || 'Files'}</div>
            <div className="tree-scroll">
              {!active && <div className="nc-empty">Сначала откройте проект</div>}
              {active && asList(files).length === 0 && <div className="nc-empty">Пустая папка</div>}
              {renderTree(files)}
            </div>
            <div className="nc-vsplit" onMouseDown={(e) => beginResize('tree', e)} />
          </aside>
        )}

        <main
          className={`nc-main ${showTerm ? '' : 'no-term'}`}
          style={{['--layout-terminal' as string]: `${layout.terminalH}px`}}
        >
          <section className="nc-chat" style={{['--layout-composer' as string]: `${layout.composerH}px`}}>
            <header className="nc-chat-head">
              <div className="nc-tabs">
                {sessions.map((s) => (
                  editingTabId === s.id ? (
                    <input
                      key={s.id}
                      className="nc-tab-rename"
                      autoFocus
                      value={editingTitle}
                      onChange={(e) => setEditingTitle(e.target.value)}
                      onBlur={() => void commitRename(s.id)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') void commitRename(s.id)
                        else if (e.key === 'Escape') setEditingTabId('')
                      }}
                    />
                  ) : (
                    <button
                      key={s.id}
                      type="button"
                      className={`nc-tab ${s.id === activeSessionId ? 'active' : ''} ${busyBySession[s.id] ? 'busy' : ''}`}
                      onClick={() => void switchSession(s.id)}
                      onDoubleClick={() => startRename(s.id, s.title)}
                      title={`${s.title || 'Chat'} (двойной клик — переименовать)`}
                    >
                      <span className="nc-tab-title">{s.title || 'Chat'}</span>
                      {sessions.length > 1 && (
                        <span
                          className="nc-tab-x"
                          onClick={(e) => {
                            e.stopPropagation()
                            void removeSession(s.id)
                          }}
                        >×</span>
                      )}
                    </button>
                  )
                ))}
                <button type="button" className="nc-tab add" onClick={() => void createSession()} title="Новый чат">+</button>
              </div>
              <div className="nc-actions">
                <span className="nc-pill">{busy ? 'думает…' : keySet ? 'key OK' : 'no key'}</span>
                <button type="button" className="nc-ghost" onClick={async () => {
                  await ClearChat()
                  const empty: ChatItem[] = [{kind: 'system', content: 'Чат очищен'}]
                  if (activeSessionId) setSessionItems(activeSessionId, () => empty)
                  await SaveChat(JSON.stringify(empty)).catch(() => undefined)
                  await applyUsageStats()
                }}>Clear</button>
                {retryVisible && !busy && (
                  <button type="button" className="nc-ghost" onClick={() => void retryLast()}>Reconnect</button>
                )}
                <button type="button" className="nc-ghost" disabled={!busy} onClick={() => StopAgent()}>Stop</button>
              </div>
            </header>
            <div className="nc-thread" ref={chatRef}>
              <div className="nc-thread-inner">
                {buildDisplayRows(items, !busy).map((row) => {
                  if (row.kind === 'process') {
                    return <ProcessGroup key={row.key} rows={row.rows} />
                  }
                  if (row.kind === 'tool_group') {
                    return <ToolGroup key={row.key} summary={row.summary} tools={row.tools} />
                  }
                  const m = row.item
                  if (m.kind === 'tool') {
                    return <ToolCard key={row.key} item={m} />
                  }
                  if (m.kind === 'file') {
                    return (
                      <div key={row.key} className="nc-msg file">
                        <div className="nc-role">файл · {m.path}</div>
                        <details>
                          <summary>{previewLen(m.content, 80)}</summary>
                          <pre>{m.content}</pre>
                        </details>
                      </div>
                    )
                  }
                  if (m.kind === 'reasoning') {
                    return <ThinkingBlock key={row.key} content={m.content} collapsed={!busy} />
                  }
                  return (
                    <div key={row.key} className={`nc-msg ${m.kind}`}>
                      <div className="nc-role">
                        {m.kind === 'user' ? 'Вы' : m.kind === 'assistant' ? 'Агент' : 'Система'}
                      </div>
                      {m.kind === 'user' && m.attachments && m.attachments.length > 0 && (
                        <div className="nc-att-row">
                          {m.attachments.map((a, i) => (
                            a.isImage && a.dataUrl
                              ? <img key={i} className="nc-att-thumb" src={a.dataUrl} alt={a.name} />
                              : <span key={i} className="nc-att-chip">{a.name}</span>
                          ))}
                        </div>
                      )}
                      {m.kind === 'assistant'
                        ? <Markdown content={m.content} />
                        : <pre>{m.content}</pre>}
                    </div>
                  )
                })}
                {busy && (
                  <div className="nc-msg status">
                    <div className="nc-role">статус</div>
                    <pre>думает…</pre>
                  </div>
                )}
                <div aria-hidden className="nc-thread-end" />
              </div>
            </div>
            <div className="nc-chat-hsplit" onMouseDown={(e) => beginResize('composer', e)} />
            <div className="nc-composer-wrap">
              <form
                className={`nc-composer ${dragOver ? 'drag' : ''}`}
                onSubmit={sendChat}
                onDragOver={(e) => { e.preventDefault(); setDragOver(true) }}
                onDragLeave={() => setDragOver(false)}
                onDrop={(e) => {
                  e.preventDefault()
                  setDragOver(false)
                  void addFiles(e.dataTransfer.files)
                }}
              >
                {pendingAtts.length > 0 && (
                  <div className="nc-att-pending">
                    {pendingAtts.map((a) => (
                      <div key={a.id} className="nc-att-chip pending">
                        {a.isImage && a.dataUrl
                          ? <img src={a.dataUrl} alt={a.name} />
                          : <span>{a.name}</span>}
                        <button type="button" className="nc-att-rm" onClick={() => setPendingAtts((p) => p.filter((x) => x.id !== a.id))}>×</button>
                      </div>
                    ))}
                  </div>
                )}
                <textarea
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  placeholder="Спросите агента… Ctrl+V / drag-drop — скриншот или файл"
                  rows={3}
                  onPaste={(e) => {
                    const files = e.clipboardData?.files
                    if (files && files.length > 0) {
                      e.preventDefault()
                      void addFiles(files)
                      return
                    }
                    const items = e.clipboardData?.items
                    if (!items) return
                    const collected: File[] = []
                    for (let i = 0; i < items.length; i++) {
                      const it = items[i]
                      if (it.kind === 'file') {
                        const f = it.getAsFile()
                        if (f) collected.push(f)
                      }
                    }
                    if (collected.length) {
                      e.preventDefault()
                      void addFiles(collected)
                    }
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.shiftKey) {
                      e.preventDefault()
                      void sendChat()
                    }
                  }}
                />
                <div className="nc-composer-bar">
                  <div className="nc-composer-left">
                    <button type="button" className="nc-ghost" onClick={() => fileInputRef.current?.click()}>Attach</button>
                    <input
                      ref={fileInputRef}
                      type="file"
                      multiple
                      hidden
                      onChange={(e) => {
                        if (e.target.files) void addFiles(e.target.files)
                        e.target.value = ''
                      }}
                    />
                    <span className="nc-hint">Enter — отправить · картинки → vision</span>
                  </div>
                  <button type="submit" disabled={busy || (!input.trim() && pendingAtts.length === 0)}>{busy ? '…' : 'Send'}</button>
                </div>
              </form>
            </div>
          </section>

          {showTerm && (
            <section className="nc-terminal">
              <div className="nc-hsplit" onMouseDown={(e) => beginResize('terminal', e)} />
              <div className="nc-term-head">
                <span>Terminal</span>
                <div className="nc-term-run">
                  <input value={termCmd} onChange={(e) => setTermCmd(e.target.value)} placeholder="oneshot: date" onKeyDown={(e) => e.key === 'Enter' && void runOneShot()} />
                  <button type="button" onClick={runOneShot}>Run</button>
                </div>
              </div>
              <div className="nc-xterm" ref={termRef} />
            </section>
          )}
        </main>

        {showSettings && (
          <aside className="nc-settings">
            <div className="nc-vsplit leading" onMouseDown={(e) => beginResize('settings', e)} />
            <div className="nc-settings-head">
              <div className="nc-section-label">Settings</div>
              <button type="button" className="nc-ghost" onClick={() => setSettingsVisible(false)}>Close</button>
            </div>
            <div className="nc-section-label">DeepSeek</div>
            <label>
              Model
              <select value={model} onChange={(e) => setModel(e.target.value)}>
                <option value="deepseek-v4-flash">deepseek-v4-flash</option>
                <option value="deepseek-v4-pro">deepseek-v4-pro</option>
                <option value="deepseek-v4-flash-vision-exp">deepseek-v4-flash-vision-exp</option>
              </select>
            </label>
            <label>
              API key
              <input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={keySet ? '•••• set' : 'sk-...'} />
            </label>
            <button type="button" onClick={saveSettings}>Save settings</button>
            <button type="button" className="nc-ghost" onClick={testConnect}>Test connect</button>

            <div className="nc-section-label">Agent</div>
            <label>
              Max tool steps
              <input
                type="number"
                min={1}
                max={500}
                value={maxSteps}
                onChange={(e) => setMaxSteps(Number(e.target.value))}
              />
            </label>
            <p className="nc-help">Лимит шагов агента (tool calls) за один запрос. По умолчанию 40.</p>

            <div className="nc-section-label">Interface</div>
            <label>
              Theme
              <select
                value={theme}
                onChange={(e) => void applyTheme(e.target.value === 'light' ? 'light' : 'dark')}
              >
                <option value="dark">Dark</option>
                <option value="light">Light</option>
              </select>
            </label>
            <label className="nc-check">
              <input
                type="checkbox"
                checked={showTerm}
                onChange={(e) => void toggleTerminal(e.target.checked)}
              />
              Показывать терминал
            </label>
            <p className="nc-help">По умолчанию скрыт — как в Cursor: задачи делает агент через tools.</p>

            <div className="nc-section-label">Git &amp; SSH</div>
            <p className="nc-help">
              Как в Cursor: логины не хранятся в приложении. Push/pull идут через системный{' '}
              <code>git</code> (credential helper / Git Credential Manager). SSH — ключи из{' '}
              <code>~/.ssh</code> или ssh-agent. Настройте доступ один раз в системе — агент подхватит его сам.
            </p>

            <div className="nc-section-label">Cursor Rules</div>
            <p className="nc-help">
              Правила подгружаются из <code>~/.cursor/rules</code> и{' '}
              <code>&lt;project&gt;/.cursor/rules</code> (а также legacy <code>~/.cursorrules</code> и{' '}
              <code>&lt;project&gt;/.cursorrules</code>) и добавляются в системный промпт агента.
            </p>
            <ul className="nc-rules-list">
              {asList(rulesInfo.global).map((r) => (
                <li key={`g:${r.path}`} className="nc-rules-item" title={r.path}>
                  <span className="nc-rules-source global">global</span>
                  <span className="nc-rules-path">{r.name}</span>
                  {r.alwaysApply && <span className="nc-rules-always">always</span>}
                </li>
              ))}
              {asList(rulesInfo.project).map((r) => (
                <li key={`p:${r.path}`} className="nc-rules-item" title={r.path}>
                  <span className="nc-rules-source project">project</span>
                  <span className="nc-rules-path">{r.name}</span>
                  {r.alwaysApply && <span className="nc-rules-always">always</span>}
                </li>
              ))}
              {asList(rulesInfo.global).length + asList(rulesInfo.project).length === 0 && (
                <li className="nc-empty">Правила не найдены.</li>
              )}
            </ul>
            <button type="button" className="nc-ghost" onClick={() => void refreshRules()}>Reload rules</button>
          </aside>
        )}
      </div>
    </div>
  )
}

