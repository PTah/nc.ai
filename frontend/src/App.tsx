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
  SSHKeygen,
  SSHListKeys,
  StartTerminal,
  StopAgent,
  TerminalWrite,
  WriteFile,
  ListProjects,
} from '../wailsjs/go/main/App'
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime'

type Project = { name: string; path: string; opened?: string }
type FileEntry = { name: string; path: string; isDir: boolean }
type ChatItem =
  | { kind: 'user' | 'assistant' | 'system' | 'reasoning'; content: string }
  | { kind: 'tool'; name: string; content: string; phase: 'start' | 'end'; ok?: boolean }

type AgentEvent = {
  type: string
  content?: string
  name?: string
  ok?: boolean
}

function asList<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
}

export default function App() {
  const [info, setInfo] = useState({name: 'NotCursor.ai', version: '0.1.1'})
  const [projects, setProjects] = useState<Project[]>([])
  const [active, setActive] = useState<Project | null>(null)
  const [files, setFiles] = useState<FileEntry[]>([])
  const [expanded, setExpanded] = useState<Record<string, FileEntry[]>>({})
  const [showTree, setShowTree] = useState(true)
  const [editorPath, setEditorPath] = useState('')
  const [editorContent, setEditorContent] = useState('')
  const [items, setItems] = useState<ChatItem[]>([
    {kind: 'system', content: 'NotCursor.ai · DeepSeek agent. Откройте проект слева и вставьте API key справа.'},
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
      }).catch(() => undefined)
      ListProjects().then((v) => setProjects(asList(v))).catch(() => undefined)
      SSHListKeys().then((v) => setSshKeys(asList(v))).catch(() => undefined)
    } catch {
      // window.go / runtime may be missing until Wails injects bindings
    }

    let offAgent: (() => void) | undefined
    let offTerm: (() => void) | undefined
    try {
      offAgent = EventsOn('agent:event', (raw: AgentEvent) => {
        const ev = raw
        if (ev.type === 'delta') {
          assistantBuf.current += ev.content || ''
          const text = assistantBuf.current
          setItems((prev) => {
            const copy = [...prev]
            const last = copy[copy.length - 1]
            if (last && last.kind === 'assistant') {
              copy[copy.length - 1] = {kind: 'assistant', content: text}
              return copy
            }
            return [...copy, {kind: 'assistant', content: text}]
          })
        } else if (ev.type === 'reasoning') {
          setItems((prev) => [...prev, {kind: 'reasoning', content: ev.content || ''}])
        } else if (ev.type === 'tool_start') {
          assistantBuf.current = ''
          setItems((prev) => [...prev, {kind: 'tool', name: ev.name || 'tool', content: ev.content || '', phase: 'start'}])
        } else if (ev.type === 'tool_end') {
          setItems((prev) => [...prev, {kind: 'tool', name: ev.name || 'tool', content: ev.content || '', phase: 'end', ok: ev.ok}])
        } else if (ev.type === 'done') {
          assistantBuf.current = ''
          setBusy(false)
        } else if (ev.type === 'error') {
          assistantBuf.current = ''
          setBusy(false)
          setItems((prev) => [...prev, {kind: 'system', content: `Error: ${ev.content}`}])
        }
      })

      offTerm = EventsOn('terminal:data', (data: string) => {
        xtermRef.current?.write(data)
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
    chatRef.current?.scrollTo({top: chatRef.current.scrollHeight})
  }, [items])

  useEffect(() => {
    if (!termRef.current || xtermRef.current) return
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
  }, [])

  async function openPicked() {
    try {
      const dir = await PickProjectDir()
      if (!dir) return
      await openProject(dir)
    } catch (e) {
      setItems((m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function openProject(path: string) {
    const p = await OpenProject(path)
    setActive(p)
    setProjects(asList(await ListProjects()))
    await refreshFiles('.')
    xtermRef.current?.writeln(`\r\n$ cd ${p.path}`)
    await StartTerminal()
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
    const content = await ReadFile(path)
    setEditorPath(path)
    setEditorContent(content)
  }

  async function saveEditor() {
    if (!editorPath) return
    await WriteFile(editorPath, editorContent)
    setItems((m) => [...m, {kind: 'system', content: `Saved ${editorPath}`}])
  }

  async function saveSettings() {
    if (apiKey.trim()) {
      await SaveDeepSeekKey(apiKey.trim())
      setApiKey('')
      setKeySet(true)
    }
    await SaveDeepSeekModel(model.trim() || 'deepseek-v4-flash')
    if (gitUser || gitPass) {
      await SaveGitAuth(gitUser, gitPass)
      setGitPass('')
    }
    setItems((m) => [...m, {kind: 'system', content: 'Settings saved'}])
  }

  async function testConnect() {
    try {
      const r = await ChatOnce('ping')
      setItems((m) => [...m, {kind: 'system', content: `DeepSeek connect OK: ${r || '(empty content)'}`}])
    } catch (e) {
      setItems((m) => [...m, {kind: 'system', content: `Connect failed: ${String(e)}`}])
    }
  }

  async function sendChat(e: FormEvent) {
    e.preventDefault()
    const text = input.trim()
    if (!text || busy) return
    setInput('')
    assistantBuf.current = ''
    setItems((m) => [...m, {kind: 'user', content: text}])
    setBusy(true)
    try {
      await RunAgent(text)
    } catch (err) {
      setBusy(false)
      setItems((m) => [...m, {kind: 'system', content: String(err)}])
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
    setItems((m) => [...m, {kind: 'system', content: `SSH key created:\n${pub}`}])
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
      </header>
      <div className={`nc-root ${showTree ? '' : 'no-tree'}`}>
      <aside className="nc-projects">
        <div className="nc-brand">
          <span className="nc-logo">NC</span>
          <div>
            <div className="nc-title">{info.name}</div>
            <div className="nc-sub">v{info.version}</div>
          </div>
        </div>
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

      <main className="nc-main">
        <section className="nc-editor">
          <header>
            <span>{editorPath || 'Editor'}</span>
            <button type="button" disabled={!editorPath} onClick={saveEditor}>Save</button>
          </header>
          <textarea
            value={editorContent}
            onChange={(e) => setEditorContent(e.target.value)}
            placeholder="Окно текста: откройте файл в дереве слева…"
            spellCheck={false}
          />
        </section>

        <section className="nc-chat">
          <header className="nc-chat-head">
            <span>Agent · DeepSeek</span>
            <div className="nc-actions">
              <span className="nc-pill">{keySet ? 'key OK' : 'no key'}</span>
              <button type="button" className="nc-ghost" onClick={() => { ClearChat(); setItems([{kind: 'system', content: 'Chat cleared'}]) }}>Clear</button>
              <button type="button" className="nc-ghost" disabled={!busy} onClick={() => StopAgent()}>Stop</button>
            </div>
          </header>
          <div className="nc-messages" ref={chatRef}>
            {items.map((m, i) => {
              if (m.kind === 'tool') {
                return (
                  <div key={i} className={`nc-msg tool ${m.phase}`}>
                    <div className="nc-role">tool · {m.name} · {m.phase}{m.phase === 'end' ? (m.ok ? ' ✓' : ' ✗') : ''}</div>
                    <pre>{m.content}</pre>
                  </div>
                )
              }
              return (
                <div key={i} className={`nc-msg ${m.kind}`}>
                  <div className="nc-role">{m.kind}</div>
                  <pre>{m.content}</pre>
                </div>
              )
            })}
          </div>
          <form className="nc-composer" onSubmit={sendChat}>
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="Чат с агентом: исправь файл, git status, ssh…"
              rows={3}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  void sendChat(e as unknown as FormEvent)
                }
              }}
            />
            <button type="submit" disabled={busy}>{busy ? '…' : 'Send'}</button>
          </form>
        </section>

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
      </main>

      <aside className="nc-settings">
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
      </div>
    </div>
  )
}
