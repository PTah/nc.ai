import {FormEvent, useCallback, useEffect, useRef, useState} from 'react'
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
  GetUsageStats,
  ListChatSessions,
  ListDir,
  NewChatSession,
  OpenProject,
  PickProjectDir,
  ReadFile,
  RunAgentWithAttachments,
  RunShell,
  SaveDeepSeekKey,
  SaveDeepSeekModel,
  SaveGitAuth,
  SaveShowTerminal,
  SaveShowFiles,
  SaveChat,
  SaveChatSession,
  LoadChat,
  SSHKeygen,
  SSHListKeys,
  StartTerminal,
  StopAgent,
  StopTerminal,
  SwitchChatSession,
  TerminalWrite,
  ListProjects,
} from '../wailsjs/go/main/App'
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime'
import BrandMark from './BrandMark'

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

function asList<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
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
  return (
    <details className="nc-msg reasoning" open={!collapsed}>
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
  return rows
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
      content: inner.content != null ? String(inner.content) : inner.Content != null ? String(inner.Content) : '',
      name: inner.name != null ? String(inner.name) : inner.Name != null ? String(inner.Name) : '',
      ok: Boolean(inner.ok ?? inner.OK),
      sessionId: inner.sessionId != null ? String(inner.sessionId) : inner.SessionID != null ? String(inner.SessionID) : '',
    }
  }
  return null
}

export default function App() {
  const [info, setInfo] = useState({name: 'NotCursor.ai', version: '0.1.7'})
  const [usage, setUsage] = useState({
    costUsd: 0,
    inputTokens: 0,
    outputTokens: 0,
    chatCostUsd: 0,
    chatInputTokens: 0,
    chatOutputTokens: 0,
  })
  const [projects, setProjects] = useState<Project[]>([])
  const [active, setActive] = useState<Project | null>(null)
  const [files, setFiles] = useState<FileEntry[]>([])
  const [expanded, setExpanded] = useState<Record<string, FileEntry[]>>({})
  const [showTree, setShowTree] = useState(true)
  const [showTerm, setShowTerm] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [sessions, setSessions] = useState<ChatSessionMeta[]>([])
  const [activeSessionId, setActiveSessionId] = useState('')
  const [itemsBySession, setItemsBySession] = useState<Record<string, ChatItem[]>>({})
  const [busyBySession, setBusyBySession] = useState<Record<string, boolean>>({})
  const [pendingAtts, setPendingAtts] = useState<PendingAtt[]>([])
  const [dragOver, setDragOver] = useState(false)
  const [input, setInput] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('deepseek-v4-flash')
  const [keySet, setKeySet] = useState(false)
  const [gitUser, setGitUser] = useState('')
  const [gitPass, setGitPass] = useState('')
  const [sshKeys, setSshKeys] = useState<string[]>([])
  const [termCmd, setTermCmd] = useState('')
  const chatRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const xtermRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const assistantBuf = useRef<Record<string, string>>({})
  const activeSessionRef = useRef('')

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
      const u = await GetUsageStats()
      if (!u) return
      setUsage({
        costUsd: Number(u.costUsd) || 0,
        inputTokens: Number(u.inputTokens) || 0,
        outputTokens: Number(u.outputTokens) || 0,
        chatCostUsd: Number(u.chatCostUsd) || 0,
        chatInputTokens: Number(u.chatInputTokens) || 0,
        chatOutputTokens: Number(u.chatOutputTokens) || 0,
      })
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
        if (typeof s.gitUsername === 'string') setGitUser(s.gitUsername)
        setShowTerm(Boolean(s.showTerminal))
        if (typeof s.showFiles === 'boolean') setShowTree(s.showFiles)
      }).catch(() => undefined)
      ListProjects().then((v) => setProjects(asList(v))).catch(() => undefined)
      SSHListKeys().then((v) => setSshKeys(asList(v))).catch(() => undefined)
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
          } else if (ev.type === 'usage') {
            try {
              const u = JSON.parse(ev.content || '{}')
              setUsage({
                costUsd: Number(u.costUsd) || 0,
                inputTokens: Number(u.inputTokens) || 0,
                outputTokens: Number(u.outputTokens) || 0,
                chatCostUsd: Number(u.chatCostUsd) || 0,
                chatInputTokens: Number(u.chatInputTokens) || 0,
                chatOutputTokens: Number(u.chatOutputTokens) || 0,
              })
            } catch {
              /* ignore */
            }
          } else if (ev.type === 'done' || ev.type === 'persist') {
            assistantBuf.current[sid] = ''
            setBusyBySession((b) => ({...b, [sid]: false}))
          } else if (ev.type === 'error') {
            assistantBuf.current[sid] = ''
            setBusyBySession((b) => ({...b, [sid]: false}))
            setSessionItems(sid, (prev) => [...prev, {kind: 'system', content: `Error: ${ev.content || 'unknown'}`}])
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
  }, [setSessionItems])

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
    await SaveShowTerminal(showTerm)
    if (gitUser || gitPass) {
      await SaveGitAuth(gitUser, gitPass)
      setGitPass('')
    }
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

  async function sendChat(e?: FormEvent) {
    e?.preventDefault()
    const text = input.trim()
    const atts = pendingAtts
    if ((!text && atts.length === 0) || busy || !activeSessionId) return
    setInput('')
    setPendingAtts([])
    assistantBuf.current[activeSessionId] = ''
    const previewAtts: ChatAttPreview[] = atts.map((a) => ({
      name: a.name,
      mime: a.mime,
      isImage: a.isImage,
      dataUrl: a.isImage ? a.dataUrl : undefined,
    }))
    setSessionItems(activeSessionId, (m) => [...m, {
      kind: 'user',
      content: text || (atts.some((a) => a.isImage) ? '(изображение)' : '(файл)'),
      attachments: previewAtts.length ? previewAtts : undefined,
    }])
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
    }
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

  async function genKey() {
    const name = prompt('SSH key name', 'id_ed25519')
    if (!name) return
    const pub = await SSHKeygen(name)
    setSshKeys(asList(await SSHListKeys()))
    if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: `SSH key created:\n${pub}`}])
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
    <div className="nc-app">
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
          title={`Чат: ${usage.chatInputTokens} in / ${usage.chatOutputTokens} out · Всего: ${usage.inputTokens} in / ${usage.outputTokens} out`}
        >
          <span className="nc-cost-chat">{fmtUsd(usage.chatCostUsd)}</span>
          <span className="nc-cost-sep">·</span>
          <span className="nc-cost-total">{fmtUsd(usage.costUsd)}</span>
        </span>
        <button type="button" className="nc-ghost" onClick={() => setShowSettings((v) => !v)}>
          {showSettings ? 'Hide settings' : 'Settings'}
        </button>
      </header>
      <div className={`nc-root ${showTree ? '' : 'no-tree'} ${showSettings ? '' : 'no-settings'}`}>
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
        </aside>

        {showTree && (
          <aside className="nc-tree">
            <div className="nc-section-label">{active?.name || 'Files'}</div>
            <div className="tree-scroll">
              {!active && <div className="nc-empty">Сначала откройте проект</div>}
              {active && asList(files).length === 0 && <div className="nc-empty">Пустая папка</div>}
              {renderTree(files)}
            </div>
          </aside>
        )}

        <main className={`nc-main ${showTerm ? '' : 'no-term'}`}>
          <section className="nc-chat">
            <header className="nc-chat-head">
              <div className="nc-tabs">
                {sessions.map((s) => (
                  <button
                    key={s.id}
                    type="button"
                    className={`nc-tab ${s.id === activeSessionId ? 'active' : ''} ${busyBySession[s.id] ? 'busy' : ''}`}
                    onClick={() => void switchSession(s.id)}
                    title={s.title}
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
                <button type="button" className="nc-ghost" disabled={!busy} onClick={() => StopAgent()}>Stop</button>
              </div>
            </header>
            <div className="nc-thread" ref={chatRef}>
              <div className="nc-thread-inner">
                {buildDisplayRows(items, !busy).map((row) => {
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
                      <pre className={m.kind === 'assistant' ? 'nc-answer' : undefined}>{m.content}</pre>
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
            <div className="nc-settings-head">
              <div className="nc-section-label">Settings</div>
              <button type="button" className="nc-ghost" onClick={() => setShowSettings(false)}>Close</button>
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

            <div className="nc-section-label">Interface</div>
            <label className="nc-check">
              <input
                type="checkbox"
                checked={showTerm}
                onChange={(e) => void toggleTerminal(e.target.checked)}
              />
              Показывать терминал
            </label>
            <p className="nc-muted">По умолчанию скрыт — как в Cursor: задачи делает агент через tools.</p>

            <div className="nc-section-label">Git / Gitea</div>
            <label>
              Username
              <input value={gitUser} onChange={(e) => setGitUser(e.target.value)} />
            </label>
            <label>
              Password / token
              <input type="password" value={gitPass} onChange={(e) => setGitPass(e.target.value)} />
            </label>

            <div className="nc-section-label">SSH</div>
            <button type="button" className="nc-ghost" onClick={genKey}>Generate key</button>
            <ul className="nc-list compact">
              {asList(sshKeys).length === 0 && <li className="nc-empty">Ключей нет</li>}
              {asList(sshKeys).map((k) => <li key={k}>{k}</li>)}
            </ul>
          </aside>
        )}
      </div>
    </div>
  )
}

