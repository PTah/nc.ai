import {FormEvent, useEffect, useState} from 'react'
import './App.css'

type Project = { name: string; path: string; opened?: string }
type FileEntry = { name: string; path: string; isDir: boolean }
type ChatMsg = { role: 'user' | 'assistant' | 'system'; content: string }

declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          AppInfo: () => Promise<Record<string, string>>
          GetSettings: () => Promise<Record<string, unknown>>
          SaveDeepSeekKey: (key: string) => Promise<void>
          SaveDeepSeekModel: (model: string) => Promise<void>
          ListProjects: () => Promise<Project[]>
          OpenProject: (path: string) => Promise<Project>
          ListDir: (rel: string) => Promise<FileEntry[]>
          ChatOnce: (msg: string) => Promise<string>
        }
      }
    }
  }
}

function api() {
  return window.go?.main?.App
}

export default function App() {
  const [info, setInfo] = useState<Record<string, string>>({name: 'NotCursor.ai', version: '0.1.0'})
  const [projects, setProjects] = useState<Project[]>([])
  const [active, setActive] = useState<Project | null>(null)
  const [files, setFiles] = useState<FileEntry[]>([])
  const [showTree, setShowTree] = useState(true)
  const [messages, setMessages] = useState<ChatMsg[]>([
    {role: 'system', content: 'Этап 1: DeepSeek chat. Укажите API key в настройках справа.'},
  ])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [apiKey, setApiKey] = useState('')
  const [model, setModel] = useState('deepseek-v4-flash')
  const [keySet, setKeySet] = useState(false)
  const [termLines, setTermLines] = useState<string[]>(['NotCursor terminal ready.', 'PowerShell / PTY wiring — next milestone.'])
  const [projectPath, setProjectPath] = useState('')

  useEffect(() => {
    const a = api()
    if (!a) return
    a.AppInfo().then(setInfo).catch(() => undefined)
    a.GetSettings().then((s) => {
      setKeySet(Boolean(s.deepseekKeySet))
      if (typeof s.deepseekModel === 'string' && s.deepseekModel) setModel(s.deepseekModel)
    }).catch(() => undefined)
    a.ListProjects().then(setProjects).catch(() => undefined)
  }, [])

  async function openProject(path?: string) {
    const a = api()
    const target = (path ?? projectPath).trim()
    if (!a || !target) return
    try {
      const p = await a.OpenProject(target)
      setProjectPath(p.path)
      setActive(p)
      setProjects(await a.ListProjects())
      setFiles(await a.ListDir('.'))
      setTermLines((prev) => [...prev, `cd ${p.path}`])
    } catch (e) {
      setMessages((m) => [...m, {role: 'assistant', content: `Ошибка открытия проекта: ${String(e)}`}])
    }
  }

  async function saveKey() {
    const a = api()
    if (!a) return
    await a.SaveDeepSeekKey(apiKey.trim())
    await a.SaveDeepSeekModel(model.trim() || 'deepseek-v4-flash')
    setKeySet(true)
    setApiKey('')
    setMessages((m) => [...m, {role: 'system', content: 'DeepSeek API key сохранён локально.'}])
  }

  async function sendChat(e: FormEvent) {
    e.preventDefault()
    const text = input.trim()
    if (!text || busy) return
    setInput('')
    setMessages((m) => [...m, {role: 'user', content: text}])
    setBusy(true)
    try {
      const a = api()
      if (!a) {
        setMessages((m) => [...m, {role: 'assistant', content: 'Wails bridge недоступен (откройте через wails dev).'}])
        return
      }
      const reply = await a.ChatOnce(text)
      setMessages((m) => [...m, {role: 'assistant', content: reply}])
    } catch (err) {
      setMessages((m) => [...m, {role: 'assistant', content: `Ошибка: ${String(err)}`}])
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="nc-root">
      <aside className="nc-projects">
        <div className="nc-brand">
          <span className="nc-logo">NC</span>
          <div>
            <div className="nc-title">{info.name ?? 'NotCursor.ai'}</div>
            <div className="nc-sub">v{info.version ?? '0.1.0'}</div>
          </div>
        </div>
        <div className="nc-section-label">Projects</div>
        <div className="nc-open-row">
          <input
            value={projectPath}
            onChange={(e) => setProjectPath(e.target.value)}
            placeholder="Путь к папке проекта"
          />
          <button type="button" onClick={() => openProject()}>Open</button>
        </div>
        <ul className="nc-list">
          {projects.map((p) => (
            <li key={p.path} className={active?.path === p.path ? 'active' : ''}>
              <button type="button" onClick={() => openProject(p.path)}>
                {p.name}
              </button>
            </li>
          ))}
          {projects.length === 0 && <li className="nc-muted">Нет проектов</li>}
        </ul>
        <button type="button" className="nc-ghost" onClick={() => setShowTree((v) => !v)}>
          {showTree ? 'Скрыть дерево' : 'Показать дерево'}
        </button>
      </aside>

      {showTree && (
        <aside className="nc-tree">
          <div className="nc-section-label">{active?.name ?? 'Files'}</div>
          <ul className="nc-list compact">
            {files.map((f) => (
              <li key={f.path} className={f.isDir ? 'dir' : 'file'}>{f.isDir ? '▸ ' : ''}{f.name}</li>
            ))}
            {!active && <li className="nc-muted">Откройте проект</li>}
          </ul>
        </aside>
      )}

      <main className="nc-main">
        <section className="nc-chat">
          <header className="nc-chat-head">
            <span>Agent · DeepSeek</span>
            <span className="nc-pill">{keySet ? 'API key OK' : 'API key missing'}</span>
          </header>
          <div className="nc-messages">
            {messages.map((m, i) => (
              <div key={i} className={`nc-msg ${m.role}`}>
                <div className="nc-role">{m.role}</div>
                <pre>{m.content}</pre>
              </div>
            ))}
          </div>
          <form className="nc-composer" onSubmit={sendChat}>
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="Спросите агента… (этап 1: ChatOnce → DeepSeek)"
              rows={3}
            />
            <button type="submit" disabled={busy}>{busy ? '…' : 'Send'}</button>
          </form>
        </section>

        <section className="nc-terminal">
          <div className="nc-section-label">Terminal</div>
          <pre className="nc-term-body">{termLines.join('\n')}</pre>
        </section>
      </main>

      <aside className="nc-settings">
        <div className="nc-section-label">Settings</div>
        <label>
          DeepSeek model
          <input value={model} onChange={(e) => setModel(e.target.value)} placeholder="deepseek-v4-flash" />
        </label>
        <label>
          API key
          <input
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={keySet ? '•••••••• (заменить)' : 'sk-...'}
          />
        </label>
        <button type="button" onClick={saveKey}>Save</button>
        <p className="nc-hint">
          Docs: <code>docs/TZ.md</code>, <code>docs/exchange-protocols/</code>
        </p>
      </aside>
    </div>
  )
}
