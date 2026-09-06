import {FormEvent, useCallback, useEffect, useRef, useState} from 'react'
import {Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import './App.css'
import {
  AppInfo,
  ChatOnce,
  ClearChat,
  GetSettings,
  ListDir,
  OpenProject,
  PickProjectDir,
  ReadFile,
  RunAgent,
  RunShell,
  SaveDeepSeekKey,
  SaveDeepSeekModel,
  SaveGitAuth,
  SaveShowTerminal,
  SSHKeygen,
  SSHListKeys,
  StartTerminal,
  StopAgent,
  StopTerminal,
  TerminalWrite,
  ListProjects,
} from '../wailsjs/go/main/App'
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime'

type Project = { name: string; path: string; opened?: string }
type FileEntry = { name: string; path: string; isDir: boolean }
type ChatItem =
  | { kind: 'user' | 'assistant' | 'system' | 'reasoning'; content: string }
  | { kind: 'tool'; name: string; args: string; result?: string; phase: 'running' | 'done'; ok?: boolean }
  | { kind: 'file'; path: string; content: string }

type AgentEvent = {
  type: string
  content?: string
  name?: string
  ok?: boolean
}

function asList<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
}

function previewLen(text: string, max = 160): string {
  const t = text.replace(/\s+/g, ' ').trim()
  if (t.length <= max) return t
  return t.slice(0, max) + '…'
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

function ThinkingBlock({content}: {content: string}) {
  return (
    <details className="nc-msg reasoning" open>
      <summary className="nc-think-sum">Thinking</summary>
      <pre className="nc-think-body">{content}</pre>
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
      content: inner.content != null ? String(inner.content) : inner.Content != null ? String(inner.Content) : '',
      name: inner.name != null ? String(inner.name) : inner.Name != null ? String(inner.Name) : '',
      ok: Boolean(inner.ok ?? inner.OK),
    }
  }
  return null
}

export default function App() {
  const [info, setInfo] = useState({name: 'NotCursor.ai', version: '0.1.5'})
  const [projects, setProjects] = useState<Project[]>([])
  const [active, setActive] = useState<Project | null>(null)
  const [files, setFiles] = useState<FileEntry[]>([])
  const [expanded, setExpanded] = useState<Record<string, FileEntry[]>>({})
  const [showTree, setShowTree] = useState(true)
  const [showTerm, setShowTerm] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [items, setItems] = useState<ChatItem[]>([
    {kind: 'system', content: 'Чат с агентом. Откройте проект, сохраните API key и дайте задачу — агент сам работает с файлами, git и shell через tools.'},
  ])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('deepseek-v4-flash')
  const [keySet, setKeySet] = useState(false)
  const [gitUser, setGitUser] = useState('')
  const [gitPass, setGitPass] = useState('')
  const [sshKeys, setSshKeys] = useState<string[]>([])
  const [termCmd, setTermCmd] = useState('')
  const chatRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<HTMLDivElement>(null)
  const xtermRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const assistantBuf = useRef('')

  const refreshFiles = useCallback(async (root = '.') => {
    try {
      setFiles(asList(await ListDir(root)))
    } catch {
      setFiles([])
    }
  }, [])

  useEffect(() => {
    try {
      AppInfo().then((v) => setInfo(v as typeof info)).catch(() => undefined)
      GetSettings().then((s) => {
        if (!s) return
        setKeySet(Boolean(s.deepseekKeySet))
        if (typeof s.deepseekModel === 'string' && s.deepseekModel) setModel(s.deepseekModel)
        if (typeof s.gitUsername === 'string') setGitUser(s.gitUsername)
        setShowTerm(Boolean(s.showTerminal))
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
          if (ev.type === 'delta') {
            assistantBuf.current += ev.content || ''
            const text = assistantBuf.current
            setItems((prev) => {
              const copy = [...asList(prev)]
              const last = copy[copy.length - 1]
              if (last && last.kind === 'assistant') {
                copy[copy.length - 1] = {kind: 'assistant', content: text}
                return copy
              }
              return [...copy, {kind: 'assistant', content: text}]
            })
          } else if (ev.type === 'reasoning') {
            setItems((prev) => {
              const copy = [...asList(prev)]
              const last = copy[copy.length - 1]
              // Append into last thinking block of this sub-turn if present
              if (last && last.kind === 'reasoning') {
                copy[copy.length - 1] = {kind: 'reasoning', content: last.content + (ev.content || '')}
                return copy
              }
              return [...copy, {kind: 'reasoning', content: ev.content || ''}]
            })
          } else if (ev.type === 'tool_start') {
            assistantBuf.current = ''
            setItems((prev) => [...asList(prev), {
              kind: 'tool',
              name: ev.name || 'tool',
              args: ev.content || '',
              phase: 'running',
            }])
          } else if (ev.type === 'tool_end') {
            setItems((prev) => {
              const copy = [...asList(prev)]
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
          } else if (ev.type === 'done') {
            assistantBuf.current = ''
            setBusy(false)
          } else if (ev.type === 'error') {
            assistantBuf.current = ''
            setBusy(false)
            setItems((prev) => [...asList(prev), {kind: 'system', content: `Error: ${ev.content || 'unknown'}`}])
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
  }, [])

  useEffect(() => {
    const el = chatRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [items, busy])

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
      setItems((m) => [...asList(m), {kind: 'system', content: String(e)}])
    }
  }

  async function openProject(path: string) {
    const p = await OpenProject(path)
    setActive(p)
    setProjects(asList(await ListProjects()))
    await refreshFiles('.')
    if (showTerm) {
      xtermRef.current?.writeln(`\r\n$ cd ${p.path}`)
      await StartTerminal()
    }
    setItems((m) => [...asList(m), {kind: 'system', content: `Проект открыт: ${p.name}\n${p.path}`}])
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
      setItems((m) => [...asList(m), {kind: 'file', path, content: preview}])
    } catch (e) {
      setItems((m) => [...asList(m), {kind: 'system', content: String(e)}])
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
    setItems((m) => [...asList(m), {kind: 'system', content: 'Settings saved'}])
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
      setItems((m) => [...asList(m), {kind: 'system', content: `DeepSeek connect OK: ${r || '(empty content)'}`}])
    } catch (e) {
      setItems((m) => [...asList(m), {kind: 'system', content: `Connect failed: ${String(e)}`}])
    }
  }

  async function sendChat(e?: FormEvent) {
    e?.preventDefault()
    const text = input.trim()
    if (!text || busy) return
    setInput('')
    assistantBuf.current = ''
    setItems((m) => [...asList(m), {kind: 'user', content: text}])
    setBusy(true)
    try {
      await RunAgent(text)
    } catch (err) {
      setBusy(false)
      setItems((m) => [...asList(m), {kind: 'system', content: String(err)}])
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
    setItems((m) => [...asList(m), {kind: 'system', content: `SSH key created:\n${pub}`}])
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
        <span className="nc-logo">NC</span>
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
          </select>
        </label>
        <button type="button" onClick={saveSettings}>Save</button>
        <span className={`nc-pill ${keySet ? 'ok' : ''}`}>{keySet ? 'key OK' : 'no key'}</span>
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
          <button type="button" className="nc-ghost" onClick={() => setShowTree((v) => !v)}>
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
              <span>Чат · {active?.name || 'агент'}</span>
              <div className="nc-actions">
                <span className="nc-pill">{busy ? 'думает…' : keySet ? 'key OK' : 'no key'}</span>
                <button type="button" className="nc-ghost" onClick={() => { ClearChat(); setItems([{kind: 'system', content: 'Чат очищен'}]) }}>Clear</button>
                <button type="button" className="nc-ghost" disabled={!busy} onClick={() => StopAgent()}>Stop</button>
              </div>
            </header>
            <div className="nc-thread" ref={chatRef}>
              <div className="nc-thread-inner">
                {asList(items).map((m, i) => {
                  if (m.kind === 'tool') {
                    return <ToolCard key={i} item={m} />
                  }
                  if (m.kind === 'file') {
                    return (
                      <div key={i} className="nc-msg file">
                        <div className="nc-role">файл · {m.path}</div>
                        <details open>
                          <summary>{previewLen(m.content, 80)}</summary>
                          <pre>{m.content}</pre>
                        </details>
                      </div>
                    )
                  }
                  if (m.kind === 'reasoning') {
                    return <ThinkingBlock key={i} content={m.content} />
                  }
                  return (
                    <div key={i} className={`nc-msg ${m.kind}`}>
                      <div className="nc-role">
                        {m.kind === 'user' ? 'Вы' : m.kind === 'assistant' ? 'Агент' : 'Система'}
                      </div>
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
              <form className="nc-composer" onSubmit={sendChat}>
                <textarea
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  placeholder="Спросите агента: прочитай проект и скажи, о чём он…"
                  rows={3}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.shiftKey) {
                      e.preventDefault()
                      void sendChat()
                    }
                  }}
                />
                <div className="nc-composer-bar">
                  <span className="nc-hint">Enter — отправить, Shift+Enter — новая строка</span>
                  <button type="submit" disabled={busy}>{busy ? '…' : 'Send'}</button>
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
