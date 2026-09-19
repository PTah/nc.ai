import {FormEvent, MouseEvent as ReactMouseEvent, useCallback, useEffect, useRef, useState} from 'react'
import {Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import './App.css'
import {
  AppInfo,
  ArchiveChatSession,
  AckWelcome,
  ChatOnce,
  ClearAllProviderKeys,
  ClearChat,
  ClearDeepSeekKey,
  ClearLocalKey,
  ClearOpenRouterKey,
  ClearZaiKey,
  CloseProject,
  DeleteChatSession,
  DetectDefaultShell,
  GetDeepSeekPeakInfo,
  GetSettings,
  GetUsageStats,
  GetWelcome,
  ListArchivedChats,
  ListChatSessions,
  ListDir,
  ListModelPrices,
  NewChatSession,
  OpenProject,
  PickProjectDir,
  ReadCursorRule,
  ReadFile,
  ReloadCursorRules,
  RenameChatSession,
  RunAgentWithAttachments,
  RunShell,
  SaveDeepSeekKey,
  SaveDeepSeekModel,
  SaveLocalBaseURL,
  SaveLocalKey,
  SaveLocalModel,
  SaveZaiKey,
  SaveZaiModel,
  SaveZaiEndpoint,
  SaveOpenRouterKey,
  SaveOpenRouterModel,
  ListZaiModels,
  PreferZaiModel,
  ListDeepSeekModels,
  PreferDeepSeekModel,
  GetZaiBalance,
  ListLocalModels,
  PreferLocalModel,
  ProbeLocalHealth,
  UpsertLocalEndpoint,
  RemoveLocalEndpoint,
  DuplicateLocalEndpoint,
  ListOpenRouterModels,
  PreferOpenRouterModel,
  GetOpenRouterBalance,
  SaveQwenKey,
  ClearQwenKey,
  SaveQwenModel,
  SaveQwenEndpoint,
  ListQwenModels,
  PreferQwenModel,
  ProjectHasChats,
  SaveActiveProvider,
  SaveAutoModels,
  SaveLocalLite,
  SaveToolConfirm,
  SavePlanMode,
  SetIDEContext,
  ResolveUserAsk,
  ResolveToolApproval,
  SaveAgentMaxSteps,
  SaveAgentWarnSteps,
  SaveComposerHeight,
  SaveShowTerminal,
  SaveShowFiles,
  SaveShowSettings,
  SaveLayoutSizes,
  SaveShell,
  SaveTheme,
  SaveChat,
  SaveChatSession,
  LoadChat,
  StartTerminal,
  StartupNotice,
  StopAgent,
  StopTerminal,
  SwitchChatSession,
  TerminalResize,
  TerminalWrite,
  WriteCursorRule,
  WriteFile,
  ListProjects,
} from '../wailsjs/go/main/App'
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime'
import BrandMark from './BrandMark'
import Markdown from './Markdown'
import ProjectIcon from './ProjectIcon'
import {applyChatFindMarks, clearChatFindMarks, focusChatFindHit, type ChatFindHit} from './chatFind'

type Project = { name: string; path: string; opened?: string; iconUrl?: string }
type FileEntry = { name: string; path: string; isDir: boolean }
type ChatItem =
  | { kind: 'user' | 'assistant' | 'system' | 'reasoning'; content: string; attachments?: ChatAttPreview[] }
  | { kind: 'progress'; content: string }
  | { kind: 'tool'; name: string; args: string; result?: string; phase: 'running' | 'done'; ok?: boolean; interrupted?: boolean }
  | { kind: 'file'; path: string; content: string }

/** Snapshot of a running agent turn (agent:event "progress"). */
type RunProgressInfo = {
  phase: string
  text: string
  elapsed: string
  seconds: number
  step: number
  total: number
  tools: number
  done: string
  current: string
  task: string
}

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

type ArchiveChat = {
  id: string
  projectName: string
  title: string
  archivedAt: string
  messageCount: number
  items: ChatItem[]
}

type TodoItem = {id: string; content: string; status: string}

type EditorState = {path: string; content: string; orig: string}

type QueuedMsg = {
  id: string
  text: string
  atts: PendingAtt[]
}

function formatArchiveAt(raw: unknown): string {
  if (!raw) return ''
  const d = raw instanceof Date ? raw : new Date(String(raw))
  if (Number.isNaN(d.getTime())) return String(raw)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function mapArchivedFromGo(raw: unknown): ArchiveChat | null {
  if (!raw || typeof raw !== 'object') return null
  const r = raw as Record<string, unknown>
  const id = String(r.id || r.fileName || '')
  if (!id) return null
  let items: ChatItem[] = []
  const itemsJson = String(r.itemsJson || '[]')
  try {
    const parsed = JSON.parse(itemsJson || '[]')
    if (Array.isArray(parsed)) items = parsed as ChatItem[]
  } catch {
    items = []
  }
  const messageCount = Number(r.messageCount) || items.length
  return {
    id,
    projectName: String(r.projectName || '(unknown)'),
    title: String(r.title || 'Chat'),
    archivedAt: formatArchiveAt(r.archivedAt),
    messageCount,
    items,
  }
}

type AgentEvent = {
  type: string
  content?: string
  name?: string
  ok?: boolean
  sessionId?: string
  callId?: string
}

type UsageSnapshot = {
  provider: string
  costUsd: number
  inputTokens: number
  outputTokens: number
  cacheHitTokens: number
  cacheMissTokens: number
  chatCostUsd: number
  chatInputTokens: number
  chatOutputTokens: number
  chatCacheHitTokens: number
  chatCacheMissTokens: number
  balanceOk: boolean
  balanceUsd: number
  balanceDetail: string
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
  provider: 'deepseek',
  costUsd: 0,
  inputTokens: 0,
  outputTokens: 0,
  cacheHitTokens: 0,
  cacheMissTokens: 0,
  chatCostUsd: 0,
  chatInputTokens: 0,
  chatOutputTokens: 0,
  chatCacheHitTokens: 0,
  chatCacheMissTokens: 0,
  balanceOk: false,
  balanceUsd: 0,
  balanceDetail: '',
}

type ProviderId = string

type LocalEndpointRow = {
  id: string
  name: string
  baseUrl: string
  model: string
  keySet: boolean
}

const LOCAL_BASE_DEFAULT = 'http://127.0.0.1:11434/v1'

const LOCAL_PRESETS: {id: string; label: string; url: string}[] = [
  {id: 'ollama', label: 'Ollama', url: 'http://127.0.0.1:11434/v1'},
  {id: 'lmstudio', label: 'LM Studio', url: 'http://127.0.0.1:1234/v1'},
  {id: 'custom', label: 'Custom', url: ''},
]

const DEEPSEEK_MODELS = [
  'deepseek-flash',
] as const

const ZAI_MODELS_FALLBACK = [
  'glm-4.7-flash',
  'glm-4.5-flash',
  'glm-5.3',
  'glm-5.3-flash',
  'glm-5.2',
  'glm-5.1',
  'glm-5',
  'glm-4.7',
  'glm-4.6',
  'glm-4.5',
] as const

const OPENROUTER_MODELS_FALLBACK = [
  'qwen/qwen3-coder-flash:floor',
  'qwen/qwen3-coder-flash',
  'qwen/qwen3-coder-30b-a3b-instruct:floor',
  'qwen/qwen3-coder:floor',
  'qwen/qwen3-coder',
  'qwen/qwen3-coder-plus:floor',
  'qwen/qwen3-coder-next:floor',
  'qwen/qwen3-vl-8b-instruct',
] as const

const QWEN_MODELS_FALLBACK = [
  'qwen-plus',
  'qwen-turbo',
  'qwen-max',
  'qwen3-coder-plus',
  'qwen3-coder-flash',
  'qwen-vl-max',
  'qwen-vl-plus',
] as const

type ZaiEndpointId = 'coding' | 'paas'

function isLocalProvider(id: string): boolean {
  return id === 'local' || id.startsWith('local:')
}

function providerLabelOf(id: ProviderId): string {
  if (id === 'zai') return 'Z.ai'
  if (id === 'openrouter') return 'OpenRouter'
  if (id === 'qwen') return 'Qwen'
  if (isLocalProvider(id)) return 'Custom'
  return 'DeepSeek'
}

function parseProvider(v: unknown): ProviderId {
  const s = String(v ?? '').trim()
  if (s === 'zai' || s === 'openrouter' || s === 'qwen') return s
  if (isLocalProvider(s)) return s === 'local' ? 'local:default' : s
  return 'deepseek'
}

/** Человеческий текст о причине (пере)запуска: и для баннера, и для строки в ленте. */
function startupNoticeText(n: Record<string, any>): string {
  if (!n || typeof n.reason !== 'string') return ''
  if (n.reason === 'deploy') {
    return n.fromVersion
      ? `Приложение перезапущено деплоем (обновление ${String(n.fromVersion)} → ${String(n.toVersion || '')}).`
      : `Приложение перезапущено деплоем (версия ${String(n.toVersion || '')}).`
  }
  if (n.reason === 'update') {
    return `Приложение перезапущено после обновления ${String(n.fromVersion || '?')} → ${String(n.toVersion || '')}.`
  }
  return 'Приложение было перезапущено: предыдущий сеанс завершился аварийно (принудительная остановка или сбой).'
}

function localPresetId(url: string): string {
  const u = url.trim().replace(/\/+$/, '')
  for (const p of LOCAL_PRESETS) {
    if (p.id !== 'custom' && p.url.replace(/\/+$/, '') === u) return p.id
  }
  return 'custom'
}

// stripUrlContext removes the "context" query parameter from every http(s) link
// in a chunk of text. Share links pasted into the chat often carry it; it is not
// needed to open the page, so the input stays short before it reaches the agent.
function stripUrlContext(text: string): string {
  return text.replace(/https?:\/\/[^\s<>"'`]+/gi, (raw) => {
    try {
      const u = new URL(raw)
      if (!u.searchParams.has('context')) return raw
      u.searchParams.delete('context')
      return u.toString()
    } catch {
      return raw
    }
  })
}

function modelOptions(list: readonly string[], current: string): string[] {
  if (!current || list.includes(current)) return [...list]
  return [current, ...list]
}

/** Unwrap Wails/Error prefixes for chat system lines. */
function formatConnectError(e: unknown): string {
  let msg = String(e ?? 'unknown error')
  msg = msg.replace(/^Error:\s*/i, '').trim()
  if (/перегружена/i.test(msg) || /1305/.test(msg) || /temporarily overloaded/i.test(msg)) {
    return 'Модель перегружена, попробуйте позднее…'
  }
  if (/лимит запросов/i.test(msg) || /1302/.test(msg) || /rate limit/i.test(msg)) {
    return 'Превышен лимит запросов, попробуйте позднее…'
  }
  return msg.startsWith('Connect failed') ? msg : `Connect failed: ${msg}`
}

/**
 * Stage cards and the live status line are opt-in: they show up only when the
 * request asks for interim results (see PROGRESS_ASK_RE) or after the agent has
 * been silent for SILENT_PROGRESS_MS.
 */
const SILENT_PROGRESS_MS = 120000

// Requests that explicitly want stage-by-stage reporting.
const PROGRESS_ASK_RE = new RegExp([
  'промежуточн',
  'по\\s+этапам',
  'этапами',
  'показывай\\s+(?:мне\\s+)?(?:этап|прогресс|статус|что\\s+делаешь)',
  'что\\s+(?:делаешь|делаете|продвигается)',
  'о\\s+ходе\\s+работ',
  'отчёт\\s+о\\s+работе',
  'interim\\s+results?',
  'progress\\s+updates?',
  'status\\s+updates?',
  'step\\s+by\\s+step',
].join('|'), 'i')

function wantsInterimProgress(text: string): boolean {
  return PROGRESS_ASK_RE.test(text || '')
}

function asList<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
}

function formatDuration(totalSec: number): string {
  const s = Math.max(0, Math.floor(totalSec))
  const h = Math.floor(s / 3600)
  const m = Math.floor(s / 60) % 60
  const sec = s % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`
}

function parseRunProgress(raw: unknown): RunProgressInfo | null {
  if (!raw || typeof raw !== 'object') return null
  const o = raw as Record<string, unknown>
  const text = String(o.text || '').trim()
  if (!text) return null
  return {
    phase: String(o.phase || 'step'),
    text,
    elapsed: String(o.elapsed || ''),
    seconds: Number(o.seconds) || 0,
    step: Number(o.step) || 0,
    total: Number(o.total) || 0,
    tools: Number(o.tools) || 0,
    done: String(o.done || ''),
    current: String(o.current || ''),
    task: String(o.task || ''),
  }
}

function normalizeRules(b: Partial<RulesBundle> | null | undefined): RulesBundle {
  return {
    globalDir: String(b?.globalDir || ''),
    projectDir: String(b?.projectDir || ''),
    global: asList(b?.global),
    project: asList(b?.project),
  }
}

/** Mirrors Go shouldApplyFull with empty path hints (always-on for this project). */
function isAppliedRule(r: RuleInfo): boolean {
  if (r.alwaysApply) return true
  const name = String(r.name || '').toLowerCase()
  if (name === 'agents.md' || name === '.cursorrules') return true
  const hasDesc = Boolean(String(r.description || '').trim())
  const hasGlobs = Boolean(String(r.globs || '').trim())
  return !hasDesc && !hasGlobs
}

function rulesCounts(b: RulesBundle): {total: number; applied: number} {
  const all = [...asList(b.global), ...asList(b.project)]
  return {total: all.length, applied: all.filter(isAppliedRule).length}
}

function makeUserItem(text: string, atts: PendingAtt[]): ChatItem {
  const previewAtts: ChatAttPreview[] = atts.map((a) => ({
    name: a.name,
    mime: a.mime,
    isImage: a.isImage,
    dataUrl: a.isImage ? a.dataUrl : undefined,
  }))
  return {
    kind: 'user',
    content: text || (atts.some((a) => a.isImage) ? '(изображение)' : '(файл)'),
    attachments: previewAtts.length ? previewAtts : undefined,
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
    provider: String(u.provider || u.Provider || ''),
    costUsd: pickNum(u, 'costUsd', 'CostUsd', 'CostUSD'),
    inputTokens: pickNum(u, 'inputTokens', 'InputTokens'),
    outputTokens: pickNum(u, 'outputTokens', 'OutputTokens'),
    cacheHitTokens: pickNum(u, 'cacheHitTokens', 'CacheHitTokens'),
    cacheMissTokens: pickNum(u, 'cacheMissTokens', 'CacheMissTokens'),
    chatCostUsd: pickNum(u, 'chatCostUsd', 'ChatCostUsd', 'ChatCostUSD'),
    chatInputTokens: pickNum(u, 'chatInputTokens', 'ChatInputTokens'),
    chatOutputTokens: pickNum(u, 'chatOutputTokens', 'ChatOutputTokens'),
    chatCacheHitTokens: pickNum(u, 'chatCacheHitTokens', 'ChatCacheHitTokens'),
    chatCacheMissTokens: pickNum(u, 'chatCacheMissTokens', 'ChatCacheMissTokens'),
    balanceOk: Boolean(u.balanceOk ?? u.BalanceOk),
    balanceUsd: pickNum(u, 'balanceUsd', 'BalanceUsd'),
    balanceDetail: String(u.balanceDetail || u.BalanceDetail || ''),
  }
}

function fmtUsd(c: number): string {
  return `$${Number(c || 0).toFixed(4)}`
}

function cacheHitPct(hit: number, miss: number): number | null {
  const h = Math.max(0, hit || 0)
  const m = Math.max(0, miss || 0)
  const total = h + m
  if (total <= 0) return null
  return Math.round((100 * h) / total)
}

function fmtCachePct(hit: number, miss: number): string {
  const pct = cacheHitPct(hit, miss)
  if (pct == null) return 'cache —'
  return `cache ${pct}%`
}

function fmtPrice1M(c: number, free?: boolean): string {
  if (free || c === 0) return '$0'
  return `$${Number(c).toFixed(3)}`
}

type WelcomeState = {
  show: boolean
  name: string
  version: string
  eyebrow: string
  versionLabel: string
  whatsNew: string
  continueLabel: string
  highlights: string[]
}

type ModelPriceRow = {
  provider: string
  model: string
  inputUsd: number
  outputUsd: number
  cacheHitUsd?: number
  strength: number
  note?: string
  free?: boolean
}

function toolTitle(name: string, phase: 'running' | 'done', ok?: boolean): string {
  const labels: Record<string, string> = {
    read_file: 'Reading file',
    write_file: 'Writing file',
    apply_patch: 'Patching file',
    list_dir: 'Listing directory',
    grep: 'Searching',
    find_files: 'Finding files',
    glob: 'Matching glob',
    delete_file: 'Deleting',
    move_file: 'Moving file',
    todo_write: 'Updating todos',
    ask_user: 'Asking you',
    command_status: 'Checking job',
    read_lints: 'Reading lints',
    web_search: 'Searching the web',
    fetch_url: 'Fetching URL',
    get_env_info: 'Env info',
    run_terminal: 'Running terminal',
    git_status: 'Git status',
    git_diff: 'Git diff',
    git_log: 'Git log',
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

function AskUserDialog({
  sessionId, callId, args, answer, setAnswer, onDone,
}: {
  sessionId: string
  callId: string
  args: string
  answer: string
  setAnswer: (s: string) => void
  onDone: () => void
}) {
  let question = 'Вопрос агента'
  let options: string[] = []
  try {
    const p = JSON.parse(args || '{}') as {question?: string; options?: string[]}
    if (p.question) question = p.question
    if (Array.isArray(p.options)) options = p.options.map(String).filter(Boolean)
  } catch { /* raw */ }
  function reply(text: string) {
    const t = text.trim()
    if (!t) return
    onDone()
    ResolveUserAsk(sessionId, callId, t)
  }
  return (
    <>
      <h2 id="nc-tool-ask-title">{question}</h2>
      <p className="nc-help">Агенту нужно ваше решение, прежде чем продолжить.</p>
      {options.length > 0 && (
        <div className="nc-ask-opts">
          {options.map((o) => (
            <button key={o} type="button" onClick={() => reply(o)}>{o}</button>
          ))}
        </div>
      )}
      <textarea
        className="nc-ask-text"
        value={answer}
        onChange={(e) => setAnswer(e.target.value)}
        placeholder="Свой ответ"
        rows={3}
      />
      <div className="nc-close-project-actions">
        <button type="button" onClick={() => reply(answer)} disabled={!answer.trim()}>Ответить</button>
        <button type="button" className="nc-ghost" onClick={() => { onDone(); ResolveUserAsk(sessionId, callId, '') }}>Отмена</button>
      </div>
    </>
  )
}

/**
 * Steps persisted with phase "running" can only be leftovers of a process that
 * was killed (crash or a deploy restart) — nothing is running in a fresh app.
 * Mark them so the chat does not look like it is still working.
 */
function markInterruptedSteps(items: ChatItem[]): {items: ChatItem[]; changed: boolean} {
  let changed = false
  const out = items.map((m) => {
    if (m.kind === 'tool' && m.phase === 'running') {
      changed = true
      return {...m, phase: 'done' as const, ok: false, interrupted: true}
    }
    return m
  })
  return {items: changed ? out : items, changed}
}

function ToolCard({item}: {item: Extract<ChatItem, {kind: 'tool'}>}) {
  const title = item.interrupted ? 'прервано: приложение было перезапущено' : toolTitle(item.name, item.phase, item.ok)
  const hasBody = Boolean(item.args || item.result)
  return (
    <div className={`nc-msg tool ${item.phase}${item.ok === false ? ' fail' : ''}${item.interrupted ? ' interrupted' : ''}`}>
      <div className="nc-tool-line">
        <span className="nc-tool-icon">{item.phase === 'running' ? '◉' : item.interrupted ? '⚠' : item.ok === false ? '✗' : '✓'}</span>
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
    apply_patch: ['file patched', 'files patched'],
    glob: ['glob', 'globs'],
    grep: ['search', 'searches'],
    find_files: ['file found', 'files found'],
    move_file: ['file moved', 'files moved'],
    run_terminal: ['command', 'commands'],
    git_status: ['git status', 'git status'],
    git_diff: ['git diff', 'git diffs'],
    git_log: ['git log', 'git logs'],
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
      <summary className="nc-think-sum" title="Показать рассуждения модели">
        Thinking…
      </summary>
      <pre className="nc-think-body">{content}</pre>
    </details>
  )
}

/**
 * Agent todo plan. Lives inside the answer stream (not pinned above it) and
 * collapses behind a triangle once every item is done or cancelled — like
 * "Thinking…" and tool groups.
 */
function TodoPanel({todos}: {todos: TodoItem[]}) {
  const hasOpen = todos.some((t) => t.status !== 'completed' && t.status !== 'cancelled')
  const [open, setOpen] = useState(hasOpen)
  const prevOpen = useRef(hasOpen)
  useEffect(() => {
    if (hasOpen !== prevOpen.current) {
      prevOpen.current = hasOpen
      setOpen(hasOpen)
    }
  }, [hasOpen])
  const done = todos.filter((t) => t.status === 'completed').length
  return (
    <details
      className="nc-msg todos"
      open={open}
      onToggle={(e) => setOpen((e.currentTarget as HTMLDetailsElement).open)}
    >
      <summary className="nc-think-sum" title="План задач агента (todo_write)">
        Todo · {done}/{todos.length}
      </summary>
      <ul className="nc-todos">
        {todos.map((t) => (
          <li key={t.id} className={`nc-todo ${t.status}`}>
            <span className="nc-todo-st">{t.status === 'completed' ? '✓' : t.status === 'in_progress' ? '▶' : t.status === 'cancelled' ? '×' : '○'}</span>
            {t.content}
          </li>
        ))}
      </ul>
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
      callId: eventText(inner.callId ?? inner.CallID ?? inner.CallId),
    }
  }
  return null
}

export default function App() {
  const [info, setInfo] = useState({name: 'NotCursor.ai', version: '0.6.3'})
  const [usage, setUsage] = useState<UsageSnapshot>(emptyUsage)
  const [welcome, setWelcome] = useState<WelcomeState | null>(null)
  const [toolAsk, setToolAsk] = useState<{sessionId: string; callId: string; name: string; args: string} | null>(null)
  const [showPrices, setShowPrices] = useState(false)
  const [showArchives, setShowArchives] = useState(false)
  const [archives, setArchives] = useState<ArchiveChat[]>([])
  const [archiveProjectFilter, setArchiveProjectFilter] = useState('')
  const [archiveView, setArchiveView] = useState<ArchiveChat | null>(null)
  const [appDataDir, setAppDataDir] = useState('')
  const [secretsBackend, setSecretsBackend] = useState('')
  const [modelPrices, setModelPrices] = useState<ModelPriceRow[]>([])
  const [pricesLoading, setPricesLoading] = useState(false)
  const [rulesInfo, setRulesInfo] = useState<RulesBundle>({globalDir: '', projectDir: '', global: [], project: []})
  const [shellPath, setShellPath] = useState('')
  const [shellDetected, setShellDetected] = useState('')
  const [shellSaving, setShellSaving] = useState(false)
  const [ruleEdit, setRuleEdit] = useState<{
    path: string
    name: string
    source: string
    text: string
    saving: boolean
    error: string
  } | null>(null)
  const [projectCtx, setProjectCtx] = useState<{x: number; y: number; path: string; name: string} | null>(null)
  const [closeProjectDlg, setCloseProjectDlg] = useState<{path: string; name: string} | null>(null)
  const [pendingCloseChatId, setPendingCloseChatId] = useState<string | null>(null)
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
    chatMaxW: 0, // 0 = на всю ширину панели чата
  })
  const layoutRef = useRef(layout)
  const [sessions, setSessions] = useState<ChatSessionMeta[]>([])
  const [activeSessionId, setActiveSessionId] = useState('')
  const [itemsBySession, setItemsBySession] = useState<Record<string, ChatItem[]>>({})
  const [busyBySession, setBusyBySession] = useState<Record<string, boolean>>({})
  // Drafts, pending attachments and queued messages are per chat session, so a
  // question typed in project A can never be sent from project B.
  const [drafts, setDrafts] = useState<Record<string, {text: string; atts: PendingAtt[]}>>({})
  const [queues, setQueues] = useState<Record<string, QueuedMsg[]>>({})
  const [progressBySession, setProgressBySession] = useState<Record<string, RunProgressInfo>>({})
  const [progressAskedBySession, setProgressAskedBySession] = useState<Record<string, boolean>>({})
  const [runStartedAt, setRunStartedAt] = useState<Record<string, number>>({})
  // Restart banner: why the app (re)started, and whether stale steps were marked.
  const [startupNotice, setStartupNotice] = useState<Record<string, any>>({})
  const [noticeDismissed, setNoticeDismissed] = useState(false)
  const [interruptedSeen, setInterruptedSeen] = useState(false)
  const [dragOver, setDragOver] = useState(false)
  const [retryVisible, setRetryVisible] = useState(false)
  const [editingTabId, setEditingTabId] = useState('')
  const [editingTitle, setEditingTitle] = useState('')
  const [activeProvider, setActiveProvider] = useState<ProviderId>('deepseek')
  const activeProviderRef = useRef<ProviderId>('deepseek')
  activeProviderRef.current = activeProvider
  const [deepseekKey, setDeepseekKey] = useState('')
  const [zaiKey, setZaiKey] = useState('')
  const [openrouterKey, setOpenrouterKey] = useState('')
  const [qwenKey, setQwenKey] = useState('')
  const [localKey, setLocalKey] = useState('')
  const [deepseekModel, setDeepseekModel] = useState('deepseek-flash')
  const [deepseekModels, setDeepseekModels] = useState<string[]>([...DEEPSEEK_MODELS])
  const [zaiModel, setZaiModel] = useState('glm-4.7-flash')
  const [openrouterModel, setOpenrouterModel] = useState('qwen/qwen3-coder-flash:floor')
  const [qwenModel, setQwenModel] = useState('qwen-plus')
  const [qwenEndpoint, setQwenEndpoint] = useState<'intl' | 'cn'>('intl')
  const [localModel, setLocalModel] = useState('')
  const [localBaseUrl, setLocalBaseUrl] = useState(LOCAL_BASE_DEFAULT)
  const [localModels, setLocalModels] = useState<string[]>([])
  const [localEndpoints, setLocalEndpoints] = useState<LocalEndpointRow[]>([])
  const [localEpName, setLocalEpName] = useState('Custom')
  const [localHealth, setLocalHealth] = useState<{
    level: string
    label: string
    detail: string
  } | null>(null)
  const localHealthTimer = useRef<number | null>(null)
  const [zaiEndpoint, setZaiEndpoint] = useState<ZaiEndpointId>('paas')
  const [zaiModels, setZaiModels] = useState<string[]>([...ZAI_MODELS_FALLBACK])
  const [openrouterModels, setOpenrouterModels] = useState<string[]>([...OPENROUTER_MODELS_FALLBACK])
  const [qwenModels, setQwenModels] = useState<string[]>([...QWEN_MODELS_FALLBACK])
  const [zaiBalance, setZaiBalance] = useState<{ok: boolean; availableUsd: number; detail: string; source: string} | null>(null)
  const [orBalance, setOrBalance] = useState<{ok: boolean; availableUsd: number; detail: string; source: string} | null>(null)
  const [autoModels, setAutoModels] = useState(true)
  const [localLite, setLocalLite] = useState(false)
  const [toolConfirm, setToolConfirm] = useState(true)
  const [planMode, setPlanMode] = useState(false)
  const [todosBySession, setTodosBySession] = useState<Record<string, TodoItem[]>>({})
  const [editor, setEditor] = useState<EditorState | null>(null)
  const [askAnswer, setAskAnswer] = useState('')
  const [deepseekPeak, setDeepseekPeak] = useState<{peak: boolean; tooltip: string}>({peak: false, tooltip: ''})
  const [maxSteps, setMaxSteps] = useState(120)
  const [warnSteps, setWarnSteps] = useState(120)
  const unlimitedSteps = maxSteps < 0
  const [deepseekKeySet, setDeepseekKeySet] = useState(false)
  const [zaiKeySet, setZaiKeySet] = useState(false)
  const [openrouterKeySet, setOpenrouterKeySet] = useState(false)
  const [qwenKeySet, setQwenKeySet] = useState(false)
  const [localKeySet, setLocalKeySet] = useState(false)
  const [theme, setTheme] = useState<'dark' | 'light'>('dark')
  const [termCmd, setTermCmd] = useState('')
  const [chatFindOpen, setChatFindOpen] = useState(false)
  const [chatFindQuery, setChatFindQuery] = useState('')
  const [chatFindIndex, setChatFindIndex] = useState(0)
  const [chatFindCount, setChatFindCount] = useState(0)
  const [modelsRefreshing, setModelsRefreshing] = useState(false)
  const [modelsRefreshHint, setModelsRefreshHint] = useState('')
  const chatRef = useRef<HTMLDivElement>(null)
  const chatFindInputRef = useRef<HTMLInputElement>(null)
  const chatFindOpenRef = useRef(false)
  const chatFindRunAt = useRef(0)
  const chatFindHitsRef = useRef<ChatFindHit[]>([])
  const chatFindIndexRef = useRef(0)
  const chatFindQueryDebounced = useRef('')
  const termRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const xtermRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const assistantBuf = useRef<Record<string, string>>({})
  const activeSessionRef = useRef('')
  const noticeQueueRef = useRef<string[]>([])
  const lastRequestRef = useRef<{text: string; atts: PendingAtt[]} | null>(null)
  const lastModelRef = useRef<Record<string, string>>({})
  const busyBySessionRef = useRef<Record<string, boolean>>({})
  const queuesRef = useRef<Record<string, QueuedMsg[]>>({})
  const itemsBySessionRef = useRef<Record<string, ChatItem[]>>({})
  const progressAskedRef = useRef<Record<string, boolean>>({})
  // lastOutputAt: when this session last produced a visible answer (delta/done).
  // silentNoticeRef: one "still working" card per silent stretch.
  const lastOutputAtRef = useRef<Record<string, number>>({})
  const silentNoticeRef = useRef<Record<string, boolean>>({})
  const runAgentRef = useRef<(
    sid: string,
    text: string,
    atts: PendingAtt[],
    skipUserMessage?: boolean,
    force?: boolean,
  ) => Promise<boolean>>(async () => false)
  const [statusTick, setStatusTick] = useState(0)

  const model =
    activeProvider === 'zai'
      ? zaiModel
      : activeProvider === 'openrouter'
        ? openrouterModel
        : activeProvider === 'qwen'
          ? qwenModel
          : isLocalProvider(activeProvider)
            ? localModel
            : deepseekModel
  const keySet =
    activeProvider === 'zai'
      ? zaiKeySet
      : activeProvider === 'openrouter'
        ? openrouterKeySet
        : activeProvider === 'qwen'
          ? qwenKeySet
          : isLocalProvider(activeProvider)
            ? Boolean(localBaseUrl.trim() && localModel.trim())
            : deepseekKeySet
  const providerLabel = providerLabelOf(activeProvider)
  const showBalance = activeProvider === 'zai' || activeProvider === 'openrouter'
  const providerReadyLabel =
    isLocalProvider(activeProvider)
      ? keySet
        ? 'Custom ready'
        : localModel.trim()
          ? 'Custom: set Base URL'
          : 'Custom: set model'
      : keySet
        ? `${providerLabel} key OK`
        : `no ${providerLabel} key`

  const items = asList(activeSessionId ? itemsBySession[activeSessionId] : undefined)
  const busy = Boolean(activeSessionId && busyBySession[activeSessionId])
  // Everything the composer shows is bound to the active chat session: question
  // text, attachments, queue and todo list never leak into another project.
  const draft = activeSessionId ? drafts[activeSessionId] : undefined
  const input = draft?.text ?? ''
  const pendingAtts = asList(draft?.atts)
  const queue = asList(activeSessionId ? queues[activeSessionId] : undefined)
  const queueCount = queue.length
  const todos = asList(activeSessionId ? todosBySession[activeSessionId] : undefined)
  const statusText = busy
    ? queueCount > 0 ? `думает… · очередь ${queueCount}` : 'думает…'
    : queueCount > 0 ? `в очереди: ${queueCount}` : ''
  const {total: rulesTotal, applied: rulesApplied} = rulesCounts(rulesInfo)

  const setInput = useCallback((value: string, sid = activeSessionRef.current) => {
    if (!sid) return
    setDrafts((prev) => ({...prev, [sid]: {text: value, atts: prev[sid]?.atts ?? []}}))
  }, [])
  const setPendingAtts = useCallback((
    updater: PendingAtt[] | ((prev: PendingAtt[]) => PendingAtt[]),
    sid = activeSessionRef.current,
  ) => {
    if (!sid) return
    setDrafts((prev) => {
      const cur = prev[sid] || {text: '', atts: []}
      const atts = typeof updater === 'function' ? updater(cur.atts) : updater
      return {...prev, [sid]: {...cur, atts}}
    })
  }, [])
  const setQueue = useCallback((
    updater: QueuedMsg[] | ((prev: QueuedMsg[]) => QueuedMsg[]),
    sid = activeSessionRef.current,
  ) => {
    if (!sid) return
    setQueues((prev) => {
      const cur = prev[sid] || []
      const next = typeof updater === 'function' ? updater(cur) : updater
      return {...prev, [sid]: next}
    })
  }, [])

  // Only the *active* chat can run: the backend agent loop works on the active
  // session, so a question queued in project A waits until A is opened again —
  // it is never fired into whatever project happens to be in front.
  const flushQueue = useCallback(async (sid: string) => {
    if (!sid || sid !== activeSessionRef.current) return false
    if (busyBySessionRef.current[sid]) return false
    const first = asList(queuesRef.current[sid])[0]
    if (!first) return false
    const started = await runAgentRef.current(sid, first.text, first.atts)
    if (started) {
      setQueues((prev) => ({
        ...prev,
        [sid]: asList(prev[sid]).filter((q) => q.id !== first.id),
      }))
    }
    return started
  }, [])

  // Shared "run is over" cleanup. A run may finish for a project that is not on
  // screen any more — then the transcript is saved right away, from that chat's
  // own project (backend resolves the owner), so the answer is never lost.
  const finishRunUI = useCallback((sid: string) => {
    if (!sid) return
    assistantBuf.current[sid] = ''
    lastOutputAtRef.current[sid] = Date.now()
    silentNoticeRef.current[sid] = false
    setBusyBySession((b) => ({...b, [sid]: false}))
    setProgressBySession((prev) => {
      if (!prev[sid]) return prev
      const next = {...prev}
      delete next[sid]
      return next
    })
    setRunStartedAt((prev) => {
      if (!prev[sid]) return prev
      const next = {...prev}
      delete next[sid]
      return next
    })
    if (sid !== activeSessionRef.current) {
      window.setTimeout(() => {
        const list = itemsBySessionRef.current[sid]
        if (list && list.length > 0) SaveChatSession(sid, JSON.stringify(list)).catch(() => undefined)
      }, 700)
    }
  }, [])

  useEffect(() => {
    busyBySessionRef.current = busyBySession
  }, [busyBySession])
  useEffect(() => {
    queuesRef.current = queues
  }, [queues])
  useEffect(() => {
    itemsBySessionRef.current = itemsBySession
  }, [itemsBySession])
  useEffect(() => {
    progressAskedRef.current = progressAskedBySession
  }, [progressAskedBySession])

  // Live "what is the agent doing now" line and the "Этап" cards are opt-in:
  // they appear only when the request asked for interim results, or when the
  // agent has produced no visible output for two minutes.
  const runInfo = activeSessionId ? progressBySession[activeSessionId] : undefined
  const runStarted = activeSessionId ? runStartedAt[activeSessionId] : 0
  const progressAsked = Boolean(activeSessionId && progressAskedBySession[activeSessionId])
  const nowMs = statusTick || Date.now()
  const lastOutputMs = activeSessionId ? (lastOutputAtRef.current[activeSessionId] || runStarted || nowMs) : nowMs
  const silentForMs = busy ? Math.max(0, nowMs - lastOutputMs) : 0
  const interimVisible = progressAsked || silentForMs >= SILENT_PROGRESS_MS
  const runElapsedSec = runStarted
    ? Math.max(0, Math.floor((nowMs - runStarted) / 1000))
    : runInfo?.seconds ?? 0
  const showRunStatus = busy && interimVisible
  const openTodos = todos.filter((t) => t.status !== 'completed' && t.status !== 'cancelled')
  const activeTodo = todos.find((t) => t.status === 'in_progress') || openTodos[0]
  const runStatusElapsed = runElapsedSec > 0 ? formatDuration(runElapsedSec) : runInfo?.elapsed || '0:00'
  const runStatusStep = runInfo
    ? (runInfo.total > 0 ? `шаг ${runInfo.step}/${runInfo.total}` : `шаг ${runInfo.step}`)
    : ''

  // Tick once a second while a run is in flight so the two-minute silence
  // threshold (and the elapsed timer) trip without waiting for an event.
  useEffect(() => {
    if (!busy) return
    setStatusTick(Date.now())
    const id = window.setInterval(() => setStatusTick(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [busy])

  // When the agent goes quiet for two minutes, post a single "still working"
  // stage card — even if the user did not ask for interim results.
  useEffect(() => {
    if (!busy || !activeSessionId) return
    if (progressAsked || silentForMs < SILENT_PROGRESS_MS) return
    if (silentNoticeRef.current[activeSessionId]) return
    silentNoticeRef.current[activeSessionId] = true
    const text = runInfo?.text
      || `Работа идёт: ${formatDuration(Math.floor(silentForMs / 1000))} без вывода результата`
    setSessionItems(activeSessionId, (m) => [...m, {kind: 'progress', content: text}])
  }, [busy, activeSessionId, progressAsked, silentForMs, runInfo])

  const itemsRef = useRef(items)
  const prevBusyRef = useRef(busy)
  useEffect(() => {
    itemsRef.current = items
  }, [items])
  useEffect(() => {
    if (prevBusyRef.current && !busy) {
      if (activeSessionId) SaveChatSession(activeSessionId, JSON.stringify(items)).catch(() => undefined)
    }
    prevBusyRef.current = busy
  }, [busy, items, activeSessionId])
  useEffect(() => {
    const flush = () => {
      if (activeSessionRef.current) {
        SaveChatSession(activeSessionRef.current, JSON.stringify(itemsRef.current)).catch(() => undefined)
      }
    }
    window.addEventListener('beforeunload', flush)
    return () => window.removeEventListener('beforeunload', flush)
  }, [])

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

  useEffect(() => {
    if (!activeSessionId || noticeQueueRef.current.length === 0) return
    const q = noticeQueueRef.current
    noticeQueueRef.current = []
    setSessionItems(activeSessionId, (m) => [...m, ...q.map((t) => ({kind: 'system' as const, content: t}))])
  }, [activeSessionId, setSessionItems])

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

  function lastUserRequest(items: ChatItem[]): {text: string; atts: PendingAtt[]} | null {
    for (let i = items.length - 1; i >= 0; i--) {
      const it = items[i]
      if (it.kind === 'user') {
        const atts: PendingAtt[] = asList(it.attachments).map((a, idx) => ({
          id: `retry-${i}-${idx}`,
          name: a.name,
          mime: a.mime || '',
          isImage: a.isImage,
          dataUrl: a.isImage ? a.dataUrl : undefined,
          text: '',
        }))
        return {text: it.content, atts}
      }
    }
    return null
  }

  function applyRetryState(items: ChatItem[]) {
    const list = asList(items)
    const last = list[list.length - 1]
    if (last && last.kind === 'system' && last.content.startsWith('Error:')) {
      setRetryVisible(true)
      const req = lastUserRequest(list)
      if (req) lastRequestRef.current = req
    } else {
      setRetryVisible(false)
    }
  }

  async function refreshRules(notify = false) {
    try {
      const b = await ReloadCursorRules()
      setRulesInfo(normalizeRules(b))
      if (notify && activeSessionId) {
        const g = Array.isArray(b?.global) ? b.global.length : 0
        const p = Array.isArray(b?.project) ? b.project.length : 0
        setSessionItems(activeSessionId, (m) => [...m, {
          kind: 'system',
          content: `📋 Правила перечитаны и применены: ${g} глобальных, ${p} проектных. Действуют со следующего запроса агента.`,
        }])
      }
    } catch {
      setRulesInfo(normalizeRules(null))
    }
  }

  useEffect(() => {
    layoutRef.current = layout
  }, [layout])

  const persistLayout = useCallback((next = layoutRef.current) => {
    SaveLayoutSizes(next.projectsW, next.treeW, next.settingsW, next.terminalH, next.chatMaxW).catch(() => undefined)
    SaveComposerHeight(next.composerH).catch(() => undefined)
  }, [])

  const beginResize = useCallback((
    kind: 'projects' | 'tree' | 'settings' | 'terminal' | 'composer' | 'chat',
    e: ReactMouseEvent,
    chatSide: 'left' | 'right' = 'right',
  ) => {
    e.preventDefault()
    e.stopPropagation()
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
      } else if (kind === 'chat') {
        const delta = ev.clientX - startX
        const signed = chatSide === 'left' ? -delta : delta
        const maxW = Math.max(640, Math.floor(window.innerWidth - 80))
        let base = start.chatMaxW
        if (base <= 0) {
          const el = document.querySelector('.nc-thread-inner') as HTMLElement | null
          base = el ? Math.round(el.getBoundingClientRect().width) : Math.min(1200, maxW)
        }
        next = {...start, chatMaxW: clamp(base + signed, 420, maxW)}
      } else {
        const maxH = Math.max(160, Math.floor(window.innerHeight * 0.75))
        next = {...start, terminalH: clamp(start.terminalH - (ev.clientY - startY), 90, maxH)}
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
    const lang = navigator.language || navigator.languages?.[0] || 'en'
    const refreshPeak = () => {
      GetDeepSeekPeakInfo(lang)
        .then((p) => {
          if (!p || typeof p !== 'object') return
          setDeepseekPeak({
            peak: Boolean((p as {peak?: boolean}).peak),
            tooltip: String((p as {tooltip?: string}).tooltip || ''),
          })
        })
        .catch(() => undefined)
    }
    refreshPeak()
    const peakTimer = window.setInterval(refreshPeak, 60_000)
    try {
      AppInfo().then((v) => setInfo(v as typeof info)).catch(() => undefined)
      void applyUsageStats()
      GetWelcome(lang).then((w) => {
        if (!w || typeof w !== 'object') return
        const show = Boolean((w as {show?: boolean}).show)
        if (!show) return
        setWelcome({
          show: true,
          name: String((w as {name?: string}).name || 'NotCursor.ai'),
          version: String((w as {version?: string}).version || ''),
          eyebrow: String((w as {eyebrow?: string}).eyebrow || 'Welcome'),
          versionLabel: String((w as {versionLabel?: string}).versionLabel || 'version'),
          whatsNew: String((w as {whatsNew?: string}).whatsNew || "What's new"),
          continueLabel: String((w as {continueLabel?: string}).continueLabel || 'Continue'),
          highlights: asList(
            Array.isArray((w as {highlights?: unknown}).highlights)
              ? ((w as {highlights: unknown[]}).highlights)
              : undefined,
          ).map(String).filter(Boolean).slice(0, 5),
        })
      }).catch(() => undefined)
      StartupNotice().then((n) => {
        if (n && typeof n === 'object' && typeof (n as Record<string, any>).reason === 'string') {
          const note = n as Record<string, any>
          setStartupNotice(note)
          // Дублируем причину перезапуска в ленту чата: баннер сверху можно
          // закрыть/пропустить, а системная строка остаётся в истории переписки.
          const text = startupNoticeText(note)
          if (text) {
            const line = `⟳ ${text}`
            const sid = activeSessionRef.current
            if (sid) setSessionItems(sid, (m) => [...m, {kind: 'system' as const, content: line}])
            else noticeQueueRef.current = [...noticeQueueRef.current, line]
          }
        }
      }).catch(() => undefined)
      GetSettings().then((s) => {
        if (!s) return
        const provider = parseProvider(s.activeProvider)
        setActiveProvider(provider)
        setDeepseekKeySet(Boolean(s.deepseekKeySet))
        setZaiKeySet(Boolean(s.zaiKeySet))
        setOpenrouterKeySet(Boolean(s.openrouterKeySet))
        setQwenKeySet(Boolean(s.qwenKeySet))
        setLocalKeySet(Boolean(s.localKeySet))
        if (typeof s.secretsBackend === 'string' && s.secretsBackend) setSecretsBackend(s.secretsBackend)
        if (typeof s.deepseekModel === 'string' && s.deepseekModel) setDeepseekModel(s.deepseekModel)
        if (typeof s.zaiModel === 'string' && s.zaiModel) setZaiModel(s.zaiModel)
        if (typeof s.openrouterModel === 'string' && s.openrouterModel) setOpenrouterModel(s.openrouterModel)
        if (typeof s.qwenModel === 'string' && s.qwenModel) setQwenModel(s.qwenModel)
        if (s.qwenEndpoint === 'cn' || s.qwenEndpoint === 'intl') setQwenEndpoint(s.qwenEndpoint)
        if (typeof s.localModel === 'string') setLocalModel(s.localModel)
        if (typeof s.localBaseUrl === 'string' && s.localBaseUrl) setLocalBaseUrl(s.localBaseUrl)
        {
          const eps = Array.isArray(s.localEndpoints) ? s.localEndpoints : []
          const rows: LocalEndpointRow[] = eps.map((raw: any) => ({
            id: String(raw?.id || 'default'),
            name: String(raw?.name || raw?.id || 'Custom'),
            baseUrl: String(raw?.baseUrl || LOCAL_BASE_DEFAULT),
            model: String(raw?.model || ''),
            keySet: Boolean(raw?.keySet),
          }))
          setLocalEndpoints(rows.length ? rows : [{
            id: 'default', name: 'Custom', baseUrl: LOCAL_BASE_DEFAULT, model: '', keySet: Boolean(s.localKeySet),
          }])
          const activeId = isLocalProvider(provider) ? provider.slice('local:'.length) : 'default'
          const cur = rows.find((r) => r.id === activeId) || rows[0]
          if (cur) setLocalEpName(cur.name)
        }
        setZaiEndpoint(s.zaiEndpoint === 'coding' ? 'coding' : 'paas')
        if (s.deepseekKeySet || provider === 'deepseek') void refreshDeepSeekModels()
        if (s.zaiKeySet || provider === 'zai') void refreshZaiModels()
        if (provider === 'zai' && s.zaiKeySet) void refreshZaiBalance()
        if (s.openrouterKeySet || provider === 'openrouter') void refreshOpenRouterModels()
        if (s.qwenKeySet || provider === 'qwen') void refreshQwenModels()
        if (provider === 'openrouter' && s.openrouterKeySet) void refreshOpenRouterBalance()
        if (isLocalProvider(provider)) void refreshLocalModels({applyPreferred: true})
        setAutoModels(Boolean(s.autoModels))
        setLocalLite(Boolean(s.localLite))
        setToolConfirm(s.toolConfirm !== false)
        setPlanMode(Boolean(s.planMode))
        if (typeof s.appDataDir === 'string' && s.appDataDir) setAppDataDir(s.appDataDir)
        if (typeof s.agentMaxSteps === 'number' && s.agentMaxSteps !== 0) setMaxSteps(s.agentMaxSteps)
        if (typeof s.agentWarnSteps === 'number' && s.agentWarnSteps !== 0) {
          // negative in settings = warnings off; the field shows 0 for that
          setWarnSteps(s.agentWarnSteps < 0 ? 0 : s.agentWarnSteps)
        }
        setShowTerm(Boolean(s.showTerminal))
        if (typeof s.showFiles === 'boolean') setShowTree(s.showFiles)
        if (typeof s.showSettings === 'boolean') setShowSettings(s.showSettings)
        if (typeof s.shell === 'string') setShellPath(s.shell)
        else setShellPath('')
        if (typeof s.shellDetected === 'string' && s.shellDetected) setShellDetected(s.shellDetected)
        else if (typeof s.shellResolved === 'string' && s.shellResolved) setShellDetected(s.shellResolved)
        else void DetectDefaultShell().then((p) => setShellDetected(String(p || ''))).catch(() => undefined)
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
          chatMaxW: (() => {
            const raw = Number(s.layoutChatMaxW)
            // 0 / missing = fill pane; migrate old narrow defaults (760/960).
            if (!Number.isFinite(raw) || raw <= 0 || raw === 760 || raw === 960) return 0
            return raw
          })(),
        })
      }).catch(() => undefined)
      ListProjects().then(async (v) => {
        const list = asList(v)
        setProjects(list)
        if (list.length === 0) return
        // The backend restores the last active project at startup. Pick it from
        // the recent list instead of always activating the first entry.
        let target: (typeof list)[number] | undefined
        try {
          const b = await ListChatSessions()
          const projPath = String((b as any)?.project || '')
          if (projPath) target = list.find((p: any) => p.path === projPath)
        } catch {
          target = undefined
        }
        if (!target) target = list[0]
        setActive(target)
        void refreshFiles('.')
      }).catch(() => undefined)
      ReloadCursorRules().then((b) => setRulesInfo(normalizeRules(b))).catch(() => undefined)
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
          if (ev.type === 'notice') {
            const text = String(ev.content || '').trim()
            if (!text) return
            const sid = ev.sessionId || activeSessionRef.current
            if (sid) setSessionItems(sid, (m) => [...m, {kind: 'system' as const, content: text}])
            else noticeQueueRef.current = [...noticeQueueRef.current, text]
            return
          }
          const sid = ev.sessionId || activeSessionRef.current
          if (ev.type === 'model') {
            const nextModel = String(ev.content || '').trim()
            const reason = String(ev.name || '').trim()
            if (nextModel) {
              const prov = activeProviderRef.current
              if (isLocalProvider(prov)) setLocalModel(nextModel)
              else if (prov === 'zai' || nextModel.startsWith('glm')) setZaiModel(nextModel)
              else if (prov === 'qwen' || nextModel.startsWith('qwen-')) setQwenModel(nextModel)
              else if (prov === 'openrouter' || nextModel.includes('/')) setOpenrouterModel(nextModel)
              else setDeepseekModel(nextModel)
              const prev = lastModelRef.current[sid]
              lastModelRef.current[sid] = nextModel
              if (prev && prev !== nextModel) {
                setSessionItems(sid, (m) => [...m, {
                  kind: 'system',
                  content: reason
                    ? `⚙ модель: ${prev} → ${nextModel} (${reason})`
                    : `⚙ модель: ${prev} → ${nextModel}`,
                }])
              }
            }
            return
          }
          if (!sid) return
          if (ev.type === 'tool_ask') {
            setToolAsk({
              sessionId: sid,
              callId: String(ev.callId || ''),
              name: ev.name || 'tool',
              args: ev.content || '',
            })
            return
          }
          if (ev.type === 'delta_clear') {
            assistantBuf.current[sid] = ''
            setSessionItems(sid, (prev) => {
              const copy = [...prev]
              const last = copy[copy.length - 1]
              if (last && last.kind === 'assistant') {
                copy.pop()
                return copy
              }
              return prev
            })
            return
          }
          if (ev.type === 'delta') {
            lastOutputAtRef.current[sid] = Date.now()
            silentNoticeRef.current[sid] = false
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
          } else if (ev.type === 'todos') {
            try {
              const parsed = JSON.parse(ev.content || '[]')
              if (Array.isArray(parsed)) {
                const list = parsed.map((t: {id?: string; content?: string; status?: string}) => ({
                  id: String(t.id || ''),
                  content: String(t.content || ''),
                  status: String(t.status || 'pending'),
                }))
                setTodosBySession((prev) => ({...prev, [sid]: list}))
              }
            } catch { /* ignore */ }
          } else if (ev.type === 'progress') {
            let parsed: RunProgressInfo | null = null
            try {
              parsed = parseRunProgress(JSON.parse(ev.content || '{}'))
            } catch {
              parsed = null
            }
            if (!parsed && ev.content) {
              parsed = {phase: 'step', text: ev.content, elapsed: '', seconds: 0, step: 0, total: 0, tools: 0, done: '', current: '', task: ''}
            }
            if (!parsed) return
            setProgressBySession((prev) => ({...prev, [sid]: parsed as RunProgressInfo}))
            // Heartbeats only refresh the live status line; milestones are the
            // ones worth keeping in the transcript — and only when the request
            // asked for stages, or the agent has been silent for two minutes.
            const asked = Boolean(progressAskedRef.current[sid])
            const silent = Date.now() - (lastOutputAtRef.current[sid] || Date.now()) >= SILENT_PROGRESS_MS
            if (parsed.phase !== 'heartbeat' && parsed.phase !== 'start' && (asked || silent)) {
              setSessionItems(sid, (prev) => [...prev, {kind: 'progress', content: parsed.text}])
            }
          } else if (ev.type === 'tool_start') {
            assistantBuf.current[sid] = ''
            setSessionItems(sid, (prev) => [...prev, {
              kind: 'tool',
              name: ev.name || 'tool',
              args: ev.content || '',
              phase: 'running',
            }])
          } else if (ev.type === 'tool_end') {
            setToolAsk((prev) => {
              if (prev && prev.sessionId === sid && ev.callId && prev.callId === ev.callId) return null
              if (prev && prev.sessionId === sid && !ev.callId && prev.name === (ev.name || '')) return null
              return prev
            })
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
            finishRunUI(sid)
            if (sid === activeSessionRef.current) setRetryVisible(false)
            void applyUsageStats()
          } else if (ev.type === 'error') {
            finishRunUI(sid)
            const errContent = ev.content || 'unknown'
            const canceled = /context canceled|context cancelled/i.test(errContent)
            if (canceled) {
              setSessionItems(sid, (prev) => [...prev, {kind: 'system', content: 'Остановлено'}])
              if (sid === activeSessionRef.current) setRetryVisible(false)
              return
            }
            setSessionItems(sid, (prev) => {
              const next: ChatItem[] = [...prev, {kind: 'system', content: `Error: ${errContent}`}]
              if (/402|Insufficient Balance/i.test(errContent)) {
                next.push({kind: 'system', content: 'Пополните баланс провайдера и нажмите Reconnect.'})
              }
              return next
            })
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
      window.clearInterval(peakTimer)
      try {
        EventsOff('agent:event')
        EventsOff('terminal:data')
        offAgent?.()
        offTerm?.()
      } catch {
        /* ignore */
      }
    }
  }, [setSessionItems, applyUsageStats, finishRunUI])

  useEffect(() => {
    if (chatFindOpen) return
    const el = chatRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [items, busy, activeSessionId, chatFindOpen])

  useEffect(() => {
    chatFindOpenRef.current = chatFindOpen
  }, [chatFindOpen])

  useEffect(() => {
    chatFindIndexRef.current = chatFindIndex
  }, [chatFindIndex])

  useEffect(() => {
    if (!chatFindOpen) {
      const el = chatRef.current
      if (el) clearChatFindMarks(el)
      chatFindHitsRef.current = []
      chatFindQueryDebounced.current = ''
      setChatFindCount(0)
      return
    }
    const id = window.requestAnimationFrame(() => {
      chatFindInputRef.current?.focus()
      chatFindInputRef.current?.select()
    })
    return () => window.cancelAnimationFrame(id)
  }, [chatFindOpen])

  // Debounced find: typing must not re-scan on every key.
  // CSS Highlight API keeps the scan cheap; debounce still avoids storms.
  useEffect(() => {
    if (!chatFindOpen) return
    const root = chatRef.current
    if (!root) return
    const q = chatFindQuery
    const queryChanged = q !== chatFindQueryDebounced.current
    // Pause after typing; slower while the agent streams new DOM.
    const delay = queryChanged ? 180 : busy ? 400 : 100
    const id = window.setTimeout(() => {
      chatFindRunAt.current = performance.now()
      chatFindQueryDebounced.current = q
      const res = applyChatFindMarks(root, q)
      chatFindHitsRef.current = res.hits
      setChatFindCount(res.count)
      focusChatFindHit(res.hits, chatFindIndexRef.current, {
        scroll: queryChanged,
        smooth: false,
      })
    }, delay)
    return () => window.clearTimeout(id)
  }, [chatFindOpen, chatFindQuery, items, busy, activeSessionId])

  // Navigation (↑/↓, F3) only moves the current highlight — no re-scan.
  useEffect(() => {
    if (!chatFindOpen) return
    focusChatFindHit(chatFindHitsRef.current, chatFindIndex, {scroll: true, smooth: false})
  }, [chatFindOpen, chatFindIndex])

  // Drain this session's queue when its own agent is idle. The message is only
  // removed after the run actually started, so a race (project switch, session
  // still finishing) can never swallow a question.
  useEffect(() => {
    if (!activeSessionId || busy) return
    if (queue.length === 0) return
    const t = window.setTimeout(() => { void flushQueue(activeSessionId) }, 150)
    return () => window.clearTimeout(t)
  }, [activeSessionId, busy, queue, flushQueue])

  async function refreshArchives() {
    try {
      const rows = await ListArchivedChats()
      const mapped = asList(rows).map(mapArchivedFromGo).filter((c): c is ArchiveChat => c != null)
      setArchives(mapped)
      return mapped
    } catch {
      setArchives([])
      return [] as ArchiveChat[]
    }
  }

  // Persist active chat after quiet period.
  // Guards: never save an empty transcript, and never save a session that is
  // not part of the currently open project (stale id from a project switch
  // would otherwise overwrite the other project's chat).
  useEffect(() => {
    if (!active || !activeSessionId || items.length === 0) return
    if (!sessions.some((s) => s.id === activeSessionId)) return
    const t = window.setTimeout(() => {
      SaveChatSession(activeSessionId, JSON.stringify(items)).catch(() => undefined)
    }, 300)
    return () => window.clearTimeout(t)
  }, [items, active, activeSessionId, sessions])

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
      // Load sessions and transcript first, then commit all state in one batch.
      // (refreshSessions() would set activeSessionId mid-way and the debounced
      // persist effect could then save an empty transcript over real history.)
      const bundle = await ListChatSessions()
      const list = asList((bundle as any)?.sessions).map((s: any) => ({
        id: String(s.id || ''),
        title: String(s.title || ''),
      }))
      const activeId = String((bundle as any)?.activeId || list[0]?.id || '')
      const raw = await LoadChat(projectPath)
      const parsed = JSON.parse(raw || '[]')
      const loaded: ChatItem[] = Array.isArray(parsed) && parsed.length > 0
        ? parsed as ChatItem[]
        : [{kind: 'system', content: 'Чат с агентом. История пуста — задайте задачу. Можно вставить скриншот (Ctrl+V) или перетащить файл.'}]
      const staged = activeId && busyBySessionRef.current[activeId]
        ? {items: loaded, changed: false}
        : markInterruptedSteps(loaded)
      if (staged.changed) setInterruptedSeen(true)
      const chatItems = staged.items
      setSessions(list)
      if (activeId) {
        setItemsBySession((prev) => ({...prev, [activeId]: chatItems}))
        setActiveSessionId(activeId)
        applyRetryState(chatItems)
      } else if (list[0]) {
        setItemsBySession((prev) => ({...prev, [list[0].id]: chatItems}))
        setActiveSessionId(list[0].id)
        applyRetryState(chatItems)
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
      const loadedItems: ChatItem[] = Array.isArray(parsed) && parsed.length > 0
        ? parsed as ChatItem[]
        : [{kind: 'system', content: 'Новый чат. Задайте задачу или вставьте файл/скриншот.'}]
      const staged = busyBySessionRef.current[id]
        ? {items: loadedItems, changed: false}
        : markInterruptedSteps(loadedItems)
      if (staged.changed) setInterruptedSeen(true)
      const chatItems = staged.items
      setActiveSessionId(id)
      setItemsBySession((prev) => ({...prev, [id]: chatItems}))
      applyRetryState(chatItems)
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
      // Do not silently wipe the last chat: this branch is unreachable from
      // the tab UI (x is hidden for a single tab). Keep the data safe.
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

  async function confirmCloseChat() {
    const id = pendingCloseChatId
    setPendingCloseChatId(null)
    if (!id) return
    try {
      await removeSession(id)
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function openRuleEditor(r: RuleInfo) {
    try {
      const text = await ReadCursorRule(r.path)
      setRuleEdit({path: r.path, name: r.name, source: r.source, text, saving: false, error: ''})
    } catch (e) {
      setRuleEdit({
        path: r.path,
        name: r.name,
        source: r.source,
        text: r.content || '',
        saving: false,
        error: String(e),
      })
    }
  }

  async function saveRuleEditor() {
    if (!ruleEdit) return
    setRuleEdit((prev) => prev ? {...prev, saving: true, error: ''} : prev)
    try {
      await WriteCursorRule(ruleEdit.path, ruleEdit.text)
      setRuleEdit(null)
      await refreshRules(true)
    } catch (e) {
      setRuleEdit((prev) => prev ? {...prev, saving: false, error: String(e)} : prev)
    }
  }

  async function archiveChat() {
    if (!activeSessionId) return
    const sess = sessions.find((s) => s.id === activeSessionId)
    const title = sess?.title || 'Chat'
    try {
      const raw = await ArchiveChatSession(activeSessionId, title)
      const parsed = JSON.parse(raw || '[]')
      const chatItems: ChatItem[] = Array.isArray(parsed) && parsed.length > 0
        ? parsed as ChatItem[]
        : [{kind: 'system', content: 'Новый чат. Задайте задачу или вставьте файл/скриншот.'}]
      const {activeId} = await refreshSessions()
      if (activeId) {
        setItemsBySession((prev) => {
          const next = {...prev}
          delete next[activeSessionId]
          next[activeId] = chatItems
          return next
        })
        setActiveSessionId(activeId)
      }
      void refreshArchives()
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
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

  // Global in-app hotkeys: Ctrl-F chat find, Esc close find, Ctrl-Shift-T terminal, Ctrl-Shift-S settings.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey
      if (mod && !e.altKey && !e.shiftKey && e.code === 'KeyF') {
        e.preventDefault()
        setChatFindOpen((open) => {
          if (open) {
            setChatFindQuery('')
            setChatFindIndex(0)
            return false
          }
          return true
        })
        return
      }
      if (chatFindOpenRef.current && e.key === 'Escape') {
        e.preventDefault()
        setChatFindOpen(false)
        setChatFindQuery('')
        setChatFindIndex(0)
        return
      }
      if (chatFindOpenRef.current && e.code === 'F3') {
        e.preventDefault()
        setChatFindIndex((i) => (e.shiftKey ? i - 1 : i + 1))
        return
      }
      if (e.ctrlKey && e.shiftKey && e.code === 'KeyT') {
        e.preventDefault()
        void toggleTerminal(!showTerm)
        return
      }
      if (e.ctrlKey && e.shiftKey && e.code === 'KeyS') {
        e.preventDefault()
        setSettingsVisible((v) => !v)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showTerm])

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
    const syncPtySize = () => {
      const t = xtermRef.current
      if (!t) return
      TerminalResize(t.cols, t.rows).catch(() => undefined)
    }
    if (!termRef.current || xtermRef.current) {
      requestAnimationFrame(() => {
        try {
          fitRef.current?.fit()
          syncPtySize()
        } catch {
          /* ignore */
        }
      })
      return
    }
    try {
      const term = new Terminal({
        convertEol: true,
        cursorBlink: true,
        fontSize: 13,
        fontFamily: 'Menlo, Consolas, "Courier New", monospace',
        theme: {background: '#0d0d0d', foreground: '#c8f7c5'},
      })
      const fit = new FitAddon()
      term.loadAddon(fit)
      term.open(termRef.current)
      try {
        fit.fit()
        TerminalResize(term.cols, term.rows).catch(() => undefined)
      } catch {
        /* container may still be 0-sized */
      }
      term.onData((data) => {
        TerminalWrite(data).catch(() => undefined)
      })
      xtermRef.current = term
      fitRef.current = fit
      StartTerminal()
        .then(() => syncPtySize())
        .catch(() => undefined)
      const onResize = () => {
        try {
          fit.fit()
          syncPtySize()
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

  useEffect(() => {
    if (!showTerm || !termRef.current) return
    const fitNow = () => {
      try {
        fitRef.current?.fit()
        const t = xtermRef.current
        if (t) TerminalResize(t.cols, t.rows).catch(() => undefined)
      } catch {
        /* ignore */
      }
    }
    const id = requestAnimationFrame(fitNow)
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(() => fitNow()) : null
    ro?.observe(termRef.current)
    return () => {
      cancelAnimationFrame(id)
      ro?.disconnect()
    }
  }, [showTerm, layout.terminalH])

  useEffect(() => {
    if (!projectCtx) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node | null
      const menu = document.querySelector('.nc-ctx-menu')
      if (menu && t && menu.contains(t)) return
      setProjectCtx(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setProjectCtx(null)
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [projectCtx])

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

  async function finishCloseProject(path: string, chatAction: '' | 'delete' | 'archive') {
    const wasActive = active?.path === path
    if (wasActive && activeSessionId) {
      try { await SaveChatSession(activeSessionId, JSON.stringify(items)) } catch { /* ignore */ }
    }
    const next = await CloseProject(path, chatAction)
    setProjects(asList(await ListProjects()))
    setProjectCtx(null)
    setCloseProjectDlg(null)
    if (chatAction === 'archive') void refreshArchives()
    if (!wasActive) return
    if (next && next.path) {
      setActive(next)
      setExpanded({})
      await refreshFiles('.')
      await restoreChat(next.path)
      void refreshRules()
      if (showTerm) {
        xtermRef.current?.writeln(`\r\n$ cd ${next.path}`)
        await StartTerminal()
      }
      return
    }
    setActive(null)
    setFiles([])
    setExpanded({})
    setSessions([])
    setActiveSessionId('')
    setItemsBySession({})
    setBusyBySession({})
  }

  async function requestCloseProject(path: string, name: string) {
    setProjectCtx(null)
    try {
      const has = await ProjectHasChats(path)
      if (has) {
        setCloseProjectDlg({path, name})
        return
      }
      await finishCloseProject(path, '')
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
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
      setEditor({path, content, orig: content})
      void SetIDEContext(path, 1)
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function saveEditor() {
    if (!editor) return
    try {
      await WriteFile(editor.path, editor.content)
      setEditor({...editor, orig: editor.content})
      void SetIDEContext(editor.path, 1)
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function refreshZaiBalance() {
    try {
      const b = await GetZaiBalance()
      if (!b || typeof b !== 'object') {
        setZaiBalance(null)
        return
      }
      setZaiBalance({
        ok: Boolean((b as {ok?: boolean}).ok),
        availableUsd: Number((b as {availableUsd?: number}).availableUsd) || 0,
        detail: String((b as {detail?: string}).detail || ''),
        source: String((b as {source?: string}).source || ''),
      })
      void applyUsageStats()
    } catch {
      setZaiBalance({ok: false, availableUsd: 0, detail: 'Не удалось запросить баланс', source: 'none'})
    }
  }

  async function refreshOpenRouterBalance() {
    try {
      const b = await GetOpenRouterBalance()
      if (!b || typeof b !== 'object') {
        setOrBalance(null)
        return
      }
      setOrBalance({
        ok: Boolean((b as {ok?: boolean}).ok),
        availableUsd: Number((b as {availableUsd?: number}).availableUsd) || 0,
        detail: String((b as {detail?: string}).detail || ''),
        source: String((b as {source?: string}).source || ''),
      })
      void applyUsageStats()
    } catch {
      setOrBalance({ok: false, availableUsd: 0, detail: 'Не удалось запросить баланс', source: 'none'})
    }
  }

  async function refreshDeepSeekModels(opts?: {applyPreferred?: boolean}) {
    try {
      const list = asList(await ListDeepSeekModels()).map(String).filter(Boolean)
      if (list.length === 0) return list
      setDeepseekModels(list)
      if (opts?.applyPreferred) {
        let next = ''
        try {
          next = String(await PreferDeepSeekModel(list) || '').trim()
        } catch {
          next = list[0] || ''
        }
        if (next && next !== deepseekModel) {
          setDeepseekModel(next)
          await SaveDeepSeekModel(next)
        }
      }
      return list
    } catch {
      return [] as string[]
    }
  }

  async function refreshZaiModels(opts?: {applyPreferred?: boolean}) {
    try {
      const list = asList(await ListZaiModels()).map(String).filter(Boolean)
      if (list.length === 0) return list
      setZaiModels(list)
      if (opts?.applyPreferred) {
        let next = ''
        try {
          next = String(await PreferZaiModel(list) || '').trim()
        } catch {
          next = list[0] || ''
        }
        if (next && next !== zaiModel) {
          setZaiModel(next)
          await SaveZaiModel(next)
        }
      }
      return list
    } catch {
      return [] as string[]
    }
  }

  async function refreshOpenRouterModels(opts?: {applyPreferred?: boolean}) {
    try {
      const list = asList(await ListOpenRouterModels()).map(String).filter(Boolean)
      if (list.length === 0) return list
      setOpenrouterModels(list)
      if (opts?.applyPreferred) {
        let next = ''
        try {
          next = String(await PreferOpenRouterModel(list) || '').trim()
        } catch {
          next = list[0] || ''
        }
        if (next && next !== openrouterModel) {
          setOpenrouterModel(next)
          await SaveOpenRouterModel(next)
        }
      }
      return list
    } catch {
      return [] as string[]
    }
  }

  async function refreshQwenModels(opts?: {applyPreferred?: boolean}) {
    try {
      const list = asList(await ListQwenModels()).map(String).filter(Boolean)
      if (list.length === 0) return list
      setQwenModels(list)
      if (opts?.applyPreferred) {
        let next = ''
        try {
          next = String(await PreferQwenModel(list) || '').trim()
        } catch {
          next = list[0] || ''
        }
        if (next && next !== qwenModel) {
          setQwenModel(next)
          await SaveQwenModel(next)
        }
      }
      return list
    } catch {
      return [] as string[]
    }
  }

  async function refreshLocalModels(opts?: {applyPreferred?: boolean}) {
    try {
      const list = asList(await ListLocalModels()).map(String).filter(Boolean)
      setLocalModels(list)
      if (opts?.applyPreferred && list.length > 0) {
        let next = ''
        try {
          next = String(await PreferLocalModel(list) || '').trim()
        } catch {
          next = list[0] || ''
        }
        if (next && next !== localModel) {
          setLocalModel(next)
          await SaveLocalModel(next)
        }
      }
      void refreshLocalHealth()
      return list
    } catch (e) {
      if (activeSessionId) {
        setSessionItems(activeSessionId, (m) => [
          ...m,
          {kind: 'system', content: `Custom models: ${formatConnectError(e)}`},
        ])
      }
      void refreshLocalHealth()
      return [] as string[]
    }
  }

  async function refreshLocalHealth() {
    if (!isLocalProvider(activeProvider)) {
      setLocalHealth(null)
      return
    }
    try {
      const h = await ProbeLocalHealth()
      if (!h) return
      setLocalHealth({
        level: String(h.level || 'critical'),
        label: String(h.label || 'Local ?'),
        detail: String(h.detail || ''),
      })
    } catch (e) {
      setLocalHealth({
        level: 'critical',
        label: 'Local down',
        detail: formatConnectError(e),
      })
    }
  }

  useEffect(() => {
    if (!isLocalProvider(activeProvider)) {
      setLocalHealth(null)
      if (localHealthTimer.current != null) {
        window.clearInterval(localHealthTimer.current)
        localHealthTimer.current = null
      }
      return
    }
    void refreshLocalHealth()
    if (localHealthTimer.current != null) {
      window.clearInterval(localHealthTimer.current)
    }
    localHealthTimer.current = window.setInterval(() => {
      void refreshLocalHealth()
    }, 120000)
    return () => {
      if (localHealthTimer.current != null) {
        window.clearInterval(localHealthTimer.current)
        localHealthTimer.current = null
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeProvider, localBaseUrl, localModel])

  async function refreshActiveProviderModels() {
    if (modelsRefreshing) return
    setModelsRefreshing(true)
    setModelsRefreshHint('')
    try {
      let list: string[] = []
      if (activeProvider === 'deepseek') {
        list = await refreshDeepSeekModels({applyPreferred: true})
      } else if (activeProvider === 'zai') {
        list = await refreshZaiModels({applyPreferred: true})
      } else if (activeProvider === 'openrouter') {
        list = await refreshOpenRouterModels({applyPreferred: true})
      } else if (activeProvider === 'qwen') {
        list = await refreshQwenModels({applyPreferred: true})
      } else {
        list = await refreshLocalModels({applyPreferred: true})
      }
      setModelsRefreshHint(
        list.length > 0
          ? `${providerLabel}: ${list.length} моделей`
          : `${providerLabel}: список пуст / ошибка`,
      )
    } finally {
      setModelsRefreshing(false)
      window.setTimeout(() => setModelsRefreshHint(''), 3500)
    }
  }

  async function applyProvider(next: ProviderId) {
    setActiveProvider(next)
    try {
      await SaveActiveProvider(next)
      if (isLocalProvider(next)) {
        // keep AutoModels setting — local uses weak↔strong catalog when on
        const id = next.startsWith('local:') ? next.slice(6) : 'default'
        const ep = localEndpoints.find((x) => x.id === id)
        if (ep) {
          setLocalEpName(ep.name)
          setLocalBaseUrl(ep.baseUrl || LOCAL_BASE_DEFAULT)
          setLocalModel(ep.model || '')
          setLocalKeySet(ep.keySet)
        }
        void refreshLocalModels({applyPreferred: true})
        void applyUsageStats()
      } else if (next === 'zai') {
        setLocalHealth(null)
        void refreshZaiModels({applyPreferred: true})
        void refreshZaiBalance().then(() => applyUsageStats())
      } else if (next === 'openrouter') {
        setLocalHealth(null)
        void refreshOpenRouterModels({applyPreferred: true})
        void refreshOpenRouterBalance().then(() => applyUsageStats())
      } else if (next === 'qwen') {
        setLocalHealth(null)
        void refreshQwenModels({applyPreferred: true})
        void applyUsageStats()
      } else {
        setLocalHealth(null)
        void refreshDeepSeekModels({applyPreferred: true})
        void applyUsageStats()
      }
    } catch {
      /* ignore */
    }
  }

  async function applyModel(next: string) {
    if (activeProvider === 'zai') {
      setZaiModel(next)
      if (autoModels) {
        setAutoModels(false)
        void SaveAutoModels(false)
      }
      void SaveZaiModel(next)
      return
    }
    if (activeProvider === 'openrouter') {
      setOpenrouterModel(next)
      if (autoModels) {
        setAutoModels(false)
        void SaveAutoModels(false)
      }
      void SaveOpenRouterModel(next)
      return
    }
    if (isLocalProvider(activeProvider)) {
      setLocalModel(next)
      if (autoModels) {
        setAutoModels(false)
        void SaveAutoModels(false)
      }
      void SaveLocalModel(next)
      return
    }
    if (activeProvider === 'qwen') {
      setQwenModel(next)
      if (autoModels) {
        setAutoModels(false)
        void SaveAutoModels(false)
      }
      void SaveQwenModel(next)
      return
    }
    setDeepseekModel(next)
    if (autoModels) {
      setAutoModels(false)
      void SaveAutoModels(false)
    }
    void SaveDeepSeekModel(next)
  }

  async function saveSettings() {
    if (deepseekKey.trim()) {
      await SaveDeepSeekKey(deepseekKey.trim())
      setDeepseekKey('')
      setDeepseekKeySet(true)
    }
    if (zaiKey.trim()) {
      await SaveZaiKey(zaiKey.trim())
      setZaiKey('')
      setZaiKeySet(true)
    }
    if (openrouterKey.trim()) {
      await SaveOpenRouterKey(openrouterKey.trim())
      setOpenrouterKey('')
      setOpenrouterKeySet(true)
    }
    if (qwenKey.trim()) {
      await SaveQwenKey(qwenKey.trim())
      setQwenKey('')
      setQwenKeySet(true)
    }
    if (localKey.trim()) {
      await SaveLocalKey(localKey.trim())
      setLocalKey('')
      setLocalKeySet(true)
    }
    await SaveActiveProvider(activeProvider)
    await SaveDeepSeekModel(deepseekModel.trim() || 'deepseek-flash')
    await SaveZaiModel(zaiModel.trim() || 'glm-4.7-flash')
    await SaveZaiEndpoint(zaiEndpoint)
    await SaveOpenRouterModel(openrouterModel.trim() || 'qwen/qwen3-coder-flash:floor')
    await SaveQwenModel(qwenModel.trim() || 'qwen-plus')
    await SaveQwenEndpoint(qwenEndpoint)
    await SaveLocalBaseURL(localBaseUrl.trim() || LOCAL_BASE_DEFAULT)
    await SaveLocalModel(localModel.trim())
    if (activeProvider === 'zai' || zaiKeySet) {
      await refreshZaiModels()
      await refreshZaiBalance()
    }
    if (activeProvider === 'openrouter' || openrouterKeySet) {
      await refreshOpenRouterModels()
      await refreshOpenRouterBalance()
    }
    if (activeProvider === 'qwen' || qwenKeySet) {
      await refreshQwenModels()
    }
    if (isLocalProvider(activeProvider)) {
      await refreshLocalModels()
    }
    if (activeProvider === 'deepseek' || deepseekKeySet) {
      await refreshDeepSeekModels()
    }
    await SaveAutoModels(autoModels)
    await SaveAgentMaxSteps(unlimitedSteps ? -1 : (Number(maxSteps) || 120))
    await SaveAgentWarnSteps(warnSteps > 0 ? Number(warnSteps) : -1)
    await SaveShowTerminal(showTerm)
    await SaveTheme(theme)
    if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: 'Settings saved'}])
  }

  async function clearProviderKey(which: ProviderId) {
    try {
      if (which === 'zai') {
        await ClearZaiKey()
        setZaiKey('')
        setZaiKeySet(false)
      } else if (which === 'openrouter') {
        await ClearOpenRouterKey()
        setOpenrouterKey('')
        setOpenrouterKeySet(false)
      } else if (which === 'qwen') {
        await ClearQwenKey()
        setQwenKey('')
        setQwenKeySet(false)
      } else if (isLocalProvider(which)) {
        await ClearLocalKey()
        setLocalKey('')
        setLocalKeySet(false)
      } else {
        await ClearDeepSeekKey()
        setDeepseekKey('')
        setDeepseekKeySet(false)
      }
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: 'API key cleared'}])
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
  }

  async function clearAllKeys() {
    if (!window.confirm('Удалить все сохранённые API-ключи с этого компьютера? Смените ключи на сайте провайдера, если они могли утечь.')) {
      return
    }
    try {
      await ClearAllProviderKeys()
      setDeepseekKey('')
      setZaiKey('')
      setOpenrouterKey('')
      setLocalKey('')
      setDeepseekKeySet(false)
      setZaiKeySet(false)
      setOpenrouterKeySet(false)
      setLocalKeySet(false)
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: 'Все API-ключи удалены. При необходимости смените ключ у провайдера.'}])
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
    }
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
      if (activeProvider === 'deepseek') {
        await refreshDeepSeekModels({applyPreferred: true})
      }
      if (activeProvider === 'zai') {
        await refreshZaiModels({applyPreferred: true})
        await refreshZaiBalance()
      }
      if (activeProvider === 'openrouter') {
        await refreshOpenRouterModels({applyPreferred: true})
        await refreshOpenRouterBalance()
      }
      if (isLocalProvider(activeProvider)) {
        await refreshLocalModels({applyPreferred: true})
        await refreshLocalHealth()
      }
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: `${providerLabel} connect OK: ${r || '(empty content)'}`}])
    } catch (e) {
      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: formatConnectError(e)}])
    }
  }

  // Starts an agent run for one chat session. Returns true only when the run
  // actually started, so callers can keep queued messages they could not send.
  async function runAgent(
    sid: string,
    text: string,
    atts: PendingAtt[],
    skipUserMessage = false,
    force = false,
  ): Promise<boolean> {
    if (!sid || sid !== activeSessionRef.current) return false
    if (!text && atts.length === 0) return false
    if (busyBySessionRef.current[sid] && !force) return false
    lastRequestRef.current = {text, atts}
    setRetryVisible(false)
    assistantBuf.current[sid] = ''
    // Stage cards/status are opt-in per request; silence is measured from now.
    lastOutputAtRef.current[sid] = Date.now()
    silentNoticeRef.current[sid] = false
    setProgressAskedBySession((prev) => ({...prev, [sid]: wantsInterimProgress(text)}))
    if (!skipUserMessage) {
      setSessionItems(sid, (m) => [...m, makeUserItem(text, atts)])
    }
    setRunStartedAt((prev) => ({...prev, [sid]: Date.now()}))
    setProgressBySession((prev) => {
      if (!prev[sid]) return prev
      const next = {...prev}
      delete next[sid]
      return next
    })
    setBusyBySession((b) => ({...b, [sid]: true}))
    try {
      await RunAgentWithAttachments(text, atts.map((a) => ({
        name: a.name,
        mime: a.mime || '',
        dataUrl: a.dataUrl || '',
        text: a.text || '',
        isImage: a.isImage,
      })))
    } catch (err) {
      setBusyBySession((b) => ({...b, [sid]: false}))
      setSessionItems(sid, (m) => [...m, {kind: 'system', content: String(err)}])
      setRetryVisible(true)
      return false
    }
    return true
  }
  runAgentRef.current = runAgent

  async function sendChat(e?: FormEvent) {
    e?.preventDefault()
    const sid = activeSessionId
    if (!sid) return
    const text = stripUrlContext(input.trim())
    const atts = pendingAtts
    if (!text && atts.length === 0) return
    setInput('', sid)
    setPendingAtts([], sid)
    if (busyBySessionRef.current[sid]) {
      const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      setQueue((q) => [...q, {id, text, atts}], sid)
      return
    }
    const started = await runAgent(sid, text, atts)
    if (!started) {
      // Never lose the question: keep it queued for this chat instead.
      const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      setQueue((q) => [...q, {id, text, atts}], sid)
    }
  }

  async function sendQueuedNow(id: string) {
    const sid = activeSessionId
    if (!sid) return
    const msg = queue.find((q) => q.id === id)
    if (!msg) return
    setQueue((q) => q.filter((x) => x.id !== id), sid)
    if (busyBySessionRef.current[sid]) {
      StopAgent()
      // Give the backend a moment to release the session before restarting it.
      await new Promise((resolve) => window.setTimeout(resolve, 150))
    }
    const started = await runAgent(sid, msg.text, msg.atts, false, true)
    if (!started) setQueue((q) => [...q, msg], sid)
  }

  function dismissQueued(id: string) {
    setQueue((q) => q.filter((x) => x.id !== id))
  }

  async function retryLast() {
    const req = lastRequestRef.current
    const sid = activeSessionId
    if (!req || !sid) return
    await runAgent(sid, req.text, req.atts, true)
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
          <button type="button" className={`tree-btn file ${editor?.path === f.path ? 'active' : ''}`} onClick={() => openFile(f.path)}>
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
        <div className="nc-top-brand">
          <BrandMark size={34} />
          <strong>{info.name}</strong>
          <span className="nc-sub">v{info.version}</span>
        </div>
        <div className="nc-top-controls">
          <label className="nc-top-field" title={`Модель активного провайдера: ${providerLabel}`}>
            <span className="nc-top-field-label">Model ({providerLabel})</span>
            <select
              value={model}
              onChange={(e) => void applyModel(e.target.value)}
              title={
                autoModels
                  ? 'Auto-models выберет модель сама; ручной выбор отключает Auto'
                  : `Модель ${providerLabel}`
              }
            >
              {activeProvider === 'zai'
                ? modelOptions(zaiModels, zaiModel).map((m) => (
                    <option key={m} value={m}>{m}</option>
                  ))
                : activeProvider === 'openrouter'
                  ? modelOptions(openrouterModels, openrouterModel).map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))
                : activeProvider === 'qwen'
                  ? modelOptions(qwenModels, qwenModel).map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))
                : isLocalProvider(activeProvider)
                  ? modelOptions(localModels, localModel).map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))
                : modelOptions(deepseekModels, deepseekModel).map((m) => (
                    <option key={m} value={m}>{m}</option>
                  ))}
            </select>
          </label>
          <button
            type="button"
            className="nc-ghost"
            disabled={modelsRefreshing}
            onClick={() => void refreshActiveProviderModels()}
            title={`Перечитать модели у ${providerLabel}`}
          >
            {modelsRefreshing ? '…' : '↻ Models'}
          </button>
          {modelsRefreshHint ? (
            <span className="nc-pill" title={modelsRefreshHint}>{modelsRefreshHint}</span>
          ) : null}
          <label
            className="nc-top-check"
            title={
              isLocalProvider(activeProvider)
                ? 'Custom: меньшая coder-модель с tools для простых задач; крупнее — для сложных (модели без tools пропускаются)'
                : activeProvider === 'zai'
                ? 'Автовыбор: glm-4.7-flash (free) → glm-5.3 на сложных задачах; картинки → glm-5.3-flash'
                : activeProvider === 'openrouter'
                  ? 'Автовыбор: qwen3-coder-flash:floor → qwen3-coder:floor; картинки → qwen3-vl'
                  : activeProvider === 'qwen'
                  ? 'Автовыбор: qwen-plus → qwen-max на сложных задачах; картинки → qwen-vl-max'
                : 'Автовыбор flash / pro / vision по задаче и длине прогона'
            }
          >
            <input
              type="checkbox"
              checked={autoModels}
              onChange={(e) => {
                const on = e.target.checked
                setAutoModels(on)
                void SaveAutoModels(on)
              }}
            />
            <span>Auto-models</span>
          </label>
          {isLocalProvider(activeProvider) ? (
            <label
              className="nc-top-check"
              title="Custom lite: короткий промпт и ~12 tools (быстрее, но слабее на tool-calling). Выкл = полный агент как у облака — лучше для qwen-coder 7B/14B."
            >
              <input
                type="checkbox"
                checked={localLite}
                onChange={(e) => {
                  const on = e.target.checked
                  setLocalLite(on)
                  void SaveLocalLite(on)
                }}
              />
              <span>Custom lite</span>
            </label>
          ) : null}
          {activeProvider === 'deepseek' && deepseekPeak.peak && (
            <span
              className="nc-peak-warn"
              title={deepseekPeak.tooltip || 'Вы работаете в высокозагруженные часы, цена запросов удвоена'}
              aria-label={deepseekPeak.tooltip || 'Пиковые часы DeepSeek'}
            >
              !
            </span>
          )}
          <span className={`nc-pill ${keySet ? 'ok' : ''}`}>{providerReadyLabel}</span>
          <span
            className="nc-cost"
            title={
              (() => {
                const chatPct = cacheHitPct(usage.chatCacheHitTokens, usage.chatCacheMissTokens)
                const totPct = cacheHitPct(usage.cacheHitTokens, usage.cacheMissTokens)
                const cacheLine =
                  ` · chat cache ${usage.chatCacheHitTokens} hit / ${usage.chatCacheMissTokens} miss` +
                  (chatPct == null ? '' : ` (${chatPct}%)`) +
                  ` · total cache ${usage.cacheHitTokens} hit / ${usage.cacheMissTokens} miss` +
                  (totPct == null ? '' : ` (${totPct}%)`)
                if (activeProvider === 'zai') {
                  return `Провайдер Z.ai · чат: ${usage.chatInputTokens} in / ${usage.chatOutputTokens} out (${fmtUsd(usage.chatCostUsd)}) · total Z.ai: ${usage.inputTokens} in / ${usage.outputTokens} out (${fmtUsd(usage.costUsd)})` +
                    cacheLine +
                    (usage.balanceDetail ? ` · ${usage.balanceDetail}` : '')
                }
                if (activeProvider === 'openrouter') {
                  return `Провайдер OpenRouter · чат: ${usage.chatInputTokens} in / ${usage.chatOutputTokens} out (${fmtUsd(usage.chatCostUsd)}) · total OR: ${usage.inputTokens} in / ${usage.outputTokens} out (${fmtUsd(usage.costUsd)})` +
                    cacheLine +
                    (usage.balanceDetail ? ` · ${usage.balanceDetail}` : '')
                }
                if (activeProvider === 'qwen') {
                  return `Провайдер Qwen · чат: ${usage.chatInputTokens} in / ${usage.chatOutputTokens} out (${fmtUsd(usage.chatCostUsd)}) · total Qwen: ${usage.inputTokens} in / ${usage.outputTokens} out (${fmtUsd(usage.costUsd)})` +
                    cacheLine
                }
                if (isLocalProvider(activeProvider)) {
                  return `Провайдер Custom · чат: ${usage.chatInputTokens} in / ${usage.chatOutputTokens} out · total Custom: ${usage.inputTokens} in / ${usage.outputTokens} out` +
                    cacheLine +
                    ' · стоимость $0 (свой сервер)'
                }
                return `Провайдер DeepSeek · чат: ${usage.chatInputTokens} in / ${usage.chatOutputTokens} out (${fmtUsd(usage.chatCostUsd)}) · total DeepSeek: ${usage.inputTokens} in / ${usage.outputTokens} out (${fmtUsd(usage.costUsd)})` +
                  cacheLine
              })()
            }
          >
            <span className="nc-cost-chat">{providerLabel} chat {fmtUsd(usage.chatCostUsd)}</span>
            <span className="nc-cost-sep">·</span>
            <span className="nc-cost-total">total {fmtUsd(usage.costUsd)}</span>
            <span className="nc-cost-sep">·</span>
            <span className="nc-cost-cache">{fmtCachePct(usage.chatCacheHitTokens, usage.chatCacheMissTokens)}</span>
            {showBalance && (
              <>
                <span className="nc-cost-sep">·</span>
                <span className="nc-cost-bal" title={usage.balanceDetail || 'Refresh balance в Settings'}>
                  {usage.balanceOk ? `bal ${fmtUsd(usage.balanceUsd)}` : 'bal —'}
                </span>
              </>
            )}
          </span>
          <button type="button" className="nc-ghost" onClick={() => setSettingsVisible((v) => !v)} title="Ctrl+Shift+S">
            {showSettings ? 'Hide settings' : 'Settings'}
          </button>
        </div>
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
          <ul className="nc-list" onClick={() => setProjectCtx(null)}>
            {asList(projects).length === 0 && (
              <li className="nc-empty">Нет проектов — нажмите Open Project…</li>
            )}
            {asList(projects).map((p) => (
              <li key={p.path} className={active?.path === p.path ? 'active' : ''}>
                <button
                  type="button"
                  className="nc-project-btn"
                  onClick={() => openProject(p.path)}
                  onContextMenu={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    setProjectCtx({x: e.clientX, y: e.clientY, path: p.path, name: p.name})
                  }}
                  title={p.path}
                >
                  <ProjectIcon src={p.iconUrl} name={p.name} />
                  <span className="nc-project-name">{p.name}</span>
                </button>
              </li>
            ))}
          </ul>
          <div className="nc-projects-foot">
            <button
              type="button"
              className="nc-ghost"
              onClick={() => {
                setArchiveView(null)
                setArchiveProjectFilter('')
                setShowArchives(true)
                void refreshArchives()
              }}>
              🗄 Archived chats
            </button>
            <button type="button" className="nc-ghost" onClick={() => {
              setShowTree((v) => {
                const next = !v
                SaveShowFiles(next).catch(() => undefined)
                return next
              })
            }}>
              {showTree ? 'Hide files' : 'Show files'}
            </button>
            <button
              type="button"
              className="nc-ghost"
              onClick={() => {
                setShowPrices(true)
                if (modelPrices.length === 0) {
                  setPricesLoading(true)
                  ListModelPrices()
                    .then((rows) => {
                      const list = asList(rows).map((r) => {
                        const o = (r || {}) as unknown as Record<string, unknown>
                        return {
                          provider: String(o.provider || o.Provider || ''),
                          model: String(o.model || o.Model || ''),
                          inputUsd: Number(o.inputUsd ?? o.InputUSD ?? 0) || 0,
                          outputUsd: Number(o.outputUsd ?? o.OutputUSD ?? 0) || 0,
                          cacheHitUsd: Number(o.cacheHitUsd ?? o.CacheHitUSD ?? 0) || 0,
                          strength: Number(o.strength ?? o.Strength ?? 0) || 0,
                          note: String(o.note || o.Note || ''),
                          free: Boolean(o.free ?? o.Free),
                        } as ModelPriceRow
                      })
                      setModelPrices(list)
                    })
                    .catch(() => setModelPrices([]))
                    .finally(() => setPricesLoading(false))
                }
              }}
            >
              Model prices
            </button>
          </div>
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
          className={`nc-main ${showTerm ? '' : 'no-term'} ${editor ? 'has-editor' : ''}`}
          style={{['--layout-terminal' as string]: `${layout.terminalH}px`}}
        >
          <section
            className="nc-chat"
            style={{
              ['--layout-composer' as string]: `${layout.composerH}px`,
              ['--layout-chat-max' as string]: layout.chatMaxW > 0 ? `${layout.chatMaxW}px` : '100%',
            }}
          >
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
                      {(queues[s.id]?.length ?? 0) > 0 && (
                        <span className="nc-tab-queue" title={`Отложенных вопросов: ${queues[s.id].length}`}>
                          {queues[s.id].length}
                        </span>
                      )}
                      {sessions.length > 1 && (
                        <span
                          className="nc-tab-x"
                          onClick={(e) => {
                            e.stopPropagation()
                            setPendingCloseChatId(s.id)
                          }}
                        >×</span>
                      )}
                    </button>
                  )
                ))}
                <button type="button" className="nc-tab add" onClick={() => void createSession()} title="Новый чат">+</button>
              </div>
              <div className="nc-actions">
                {isLocalProvider(activeProvider) && localHealth ? (
                  <span
                    className={`nc-pill ${localHealth.level === 'ok' ? 'ok' : localHealth.level}`}
                    title={localHealth.detail || 'Состояние своего сервера (клик — обновить)'}
                    onClick={() => void refreshLocalHealth()}
                    style={{cursor: 'pointer'}}
                  >
                    {localHealth.label}
                  </span>
                ) : null}
                <span
                  className="nc-pill"
                  title="Всего найденных Cursor/AGENTS правил · сколько всегда применяются к проекту (alwaysApply / AGENTS.md / без globs)"
                >
                  Rules Total/Applied: {rulesTotal}/{rulesApplied}
                </span>
                {statusText ? <span className="nc-pill">{statusText}</span> : null}
                <button
                  type="button"
                  className={`nc-ghost ${planMode ? 'on' : ''}`}
                  onClick={() => {
                    const on = !planMode
                    setPlanMode(on)
                    void SavePlanMode(on)
                  }}
                  title="Plan: только чтение и план. Act — правки и shell."
                >
                  {planMode ? 'Plan' : 'Act'}
                </button>
                <button type="button" className="nc-ghost" onClick={async () => {
                  await ClearChat()
                  const empty: ChatItem[] = [{kind: 'system', content: 'Чат очищен'}]
                  if (activeSessionId) setSessionItems(activeSessionId, () => empty)
                  await SaveChat(JSON.stringify(empty)).catch(() => undefined)
                  await applyUsageStats()
                }} title="Очистить текущий чат">Clear</button>
                <button type="button" className="nc-ghost" onClick={() => void archiveChat()} disabled={!activeSessionId || busy} title="Перенести текущий чат в архив (папка chat_archive)">Archive</button>
                {retryVisible && !busy && (
                  <button type="button" className="nc-ghost" onClick={() => void retryLast()} title="Повторить последний запрос">Reconnect</button>
                )}
                <button type="button" className="nc-ghost" disabled={!busy} onClick={() => StopAgent()} title="Остановить текущий запуск агента">Stop</button>
                <button
                  type="button"
                  className={`nc-ghost ${chatFindOpen ? 'on' : ''}`}
                  onClick={() => {
                    setChatFindOpen((open) => {
                      if (open) {
                        setChatFindQuery('')
                        setChatFindIndex(0)
                        return false
                      }
                      return true
                    })
                  }}
                  title="Поиск по чату (Ctrl+F)"
                >
                  Find
                </button>
              </div>
            </header>
            {startupNotice.reason && !noticeDismissed && (
              <div className="nc-restart-note" role="status">
                <span className="nc-restart-icon" aria-hidden>⟳</span>
                <span className="nc-restart-text">
                  {startupNoticeText(startupNotice)}
                  {interruptedSeen ? ' Незавершённые шаги агента помечены как «прервано» — их нужно повторить.' : ''}
                </span>
                <button
                  type="button"
                  className="nc-restart-x"
                  title="Скрыть"
                  onClick={() => setNoticeDismissed(true)}
                >
                  ✕
                </button>
              </div>
            )}
            {chatFindOpen && (
              <div className="nc-find-bar" role="search">
                <label className="nc-find-label">
                  <span className="nc-find-label-text">Найти</span>
                  <input
                    ref={chatFindInputRef}
                    className="nc-find-input"
                    type="search"
                    value={chatFindQuery}
                    placeholder="слово или фраза…"
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(e) => {
                      setChatFindQuery(e.target.value)
                      setChatFindIndex(0)
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Escape') {
                        e.preventDefault()
                        e.stopPropagation()
                        setChatFindOpen(false)
                        setChatFindQuery('')
                        setChatFindIndex(0)
                        return
                      }
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        if (!chatFindQuery.trim() || chatFindCount === 0) return
                        setChatFindIndex((i) => (e.shiftKey ? i - 1 : i + 1))
                      }
                    }}
                  />
                </label>
                <span className="nc-find-count" title="Совпадения в чате">
                  {chatFindQuery.trim()
                    ? chatFindCount > 0
                      ? `${((chatFindIndex % chatFindCount) + chatFindCount) % chatFindCount + 1}/${chatFindCount}`
                      : '0/0'
                    : '—'}
                </span>
                <button
                  type="button"
                  className="nc-ghost"
                  disabled={!chatFindQuery.trim() || chatFindCount === 0}
                  onClick={() => setChatFindIndex((i) => i - 1)}
                  title="Предыдущее (Shift+Enter / Shift+F3)"
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="nc-ghost"
                  disabled={!chatFindQuery.trim() || chatFindCount === 0}
                  onClick={() => setChatFindIndex((i) => i + 1)}
                  title="Следующее (Enter / F3)"
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="nc-ghost"
                  onClick={() => {
                    setChatFindOpen(false)
                    setChatFindQuery('')
                    setChatFindIndex(0)
                  }}
                  title="Закрыть (Esc / Ctrl+F)"
                >
                  ×
                </button>
              </div>
            )}
            <div className="nc-thread" ref={chatRef}>
              <div className="nc-thread-inner">
                <div
                  className="nc-chat-wsplit left"
                  title="Изменить ширину чата"
                  onMouseDown={(e) => beginResize('chat', e, 'left')}
                />
                <div
                  className="nc-chat-wsplit right"
                  title="Изменить ширину чата"
                  onMouseDown={(e) => beginResize('chat', e, 'right')}
                />
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
                        <pre className="nc-file-body">{m.content}</pre>
                      </div>
                    )
                  }
                  if (m.kind === 'reasoning') {
                    return <ThinkingBlock key={row.key} content={m.content} collapsed />
                  }
                  if (m.kind === 'progress') {
                    return (
                      <div key={row.key} className="nc-msg progress">
                        <div className="nc-role">этап</div>
                        <pre>{m.content}</pre>
                      </div>
                    )
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
                {todos.length > 0 && <TodoPanel todos={todos} />}
                <div aria-hidden className="nc-thread-end" />
              </div>
            </div>
            <div className="nc-chat-hsplit" onMouseDown={(e) => beginResize('composer', e)} />
            {queue.length > 0 && (
              <div className="nc-queue-pending" aria-label="Отложенные запросы">
                <div className="nc-queue-head">
                  <span className="nc-queue-title">
                    В очереди: {queue.length}
                    {active ? ` · ${active.name}` : ''}
                  </span>
                  <span className="nc-queue-hint">
                    {busy ? 'уйдут после текущего ответа' : 'отправляю…'}
                  </span>
                </div>
                <div className="nc-queue-list">
                  {queue.map((q) => (
                    <div key={q.id} className="nc-queue-card">
                      <div className="nc-queue-card-text">
                        {q.text || (q.atts.length ? `(вложения: ${q.atts.length})` : '(пусто)')}
                      </div>
                      <div className="nc-queue-card-actions">
                        <button
                          type="button"
                          className="nc-ghost"
                          title="Остановить текущий ответ и отправить сейчас"
                          onClick={() => void sendQueuedNow(q.id)}
                        >
                          Send now
                        </button>
                        <button
                          type="button"
                          className="nc-ghost"
                          title="Убрать из очереди"
                          onClick={() => dismissQueued(q.id)}
                        >
                          ×
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
            <div className="nc-composer-wrap">
              {showRunStatus && (
                <div className="nc-run-status" title="Текущий этап работы агента">
                  <span className="nc-run-dot" aria-hidden />
                  <span className="nc-run-elapsed">⏱ {runStatusElapsed}</span>
                  {runStatusStep && <span className="nc-run-step">{runStatusStep}</span>}
                  {runInfo?.current && <span className="nc-run-current">сейчас: {runInfo.current}</span>}
                  {activeTodo && <span className="nc-run-todo">осталось: {activeTodo.content}</span>}
                </div>
              )}
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
                <div
                  className="nc-chat-wsplit left"
                  title="Изменить ширину чата"
                  onMouseDown={(e) => beginResize('chat', e, 'left')}
                />
                <div
                  className="nc-chat-wsplit right"
                  title="Изменить ширину чата"
                  onMouseDown={(e) => beginResize('chat', e, 'right')}
                />
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
                  onChange={(e) => setInput(stripUrlContext(e.target.value))}
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
                  <button type="submit" disabled={!input.trim() && pendingAtts.length === 0}>{busy ? 'Send' : 'Send'}</button>
                </div>
              </form>
            </div>
          </section>

          {editor && (
            <section className="nc-editor">
              <div className="nc-hsplit" onMouseDown={(e) => beginResize('terminal', e)} />
              <div className="nc-term-head">
                <span className="nc-editor-path" title={editor.path}>{editor.path}{editor.content !== editor.orig ? ' •' : ''}</span>
                <div className="nc-term-run">
                  <button type="button" onClick={() => void saveEditor()} disabled={editor.content === editor.orig}>Save</button>
                  <button type="button" className="nc-ghost" onClick={() => { setEditor(null); void SetIDEContext('', 0) }}>Close</button>
                </div>
              </div>
              <textarea
                className="nc-editor-body"
                spellCheck={false}
                value={editor.content}
                onChange={(e) => {
                  const content = e.target.value
                  setEditor((prev) => prev ? {...prev, content} : prev)
                }}
                onSelect={(e) => {
                  const el = e.currentTarget
                  const line = el.value.slice(0, el.selectionStart).split('\n').length
                  void SetIDEContext(editor.path, line)
                }}
              />
            </section>
          )}

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
            <div className="nc-section-label">Provider</div>
            <label>
              Active
              <select
                value={activeProvider}
                onChange={(e) => void applyProvider(parseProvider(e.target.value))}
              >
                <option value="deepseek">DeepSeek</option>
                <option value="zai">Z.ai (GLM)</option>
                <option value="openrouter">OpenRouter</option>
                <option value="qwen">Qwen (DashScope)</option>
                {localEndpoints.map((ep) => (
                  <option key={ep.id} value={`local:${ep.id}`}>
                    Custom: {ep.name}
                  </option>
                ))}
              </select>
            </label>
            <p className="nc-help">Ключи хранятся отдельно; ниже — настройки только активного провайдера.</p>
            <label
              className="nc-top-check"
              title={
                isLocalProvider(activeProvider)
                  ? 'Custom: быстрая маленькая coder (tools) ↔ крупнее на сложных задачах; без tools (часто deepseek-coder-v2) не берём'
                  : activeProvider === 'zai'
                  ? 'Z.ai: free flash → glm-5.3 на сложных задачах'
                  : activeProvider === 'openrouter'
                    ? 'OpenRouter: flash:floor → coder:floor'
                    : activeProvider === 'qwen'
                    ? 'Qwen: qwen-plus → qwen-max на сложных задачах'
                  : 'DeepSeek: flash / pro / vision'
              }
            >
              <input
                type="checkbox"
                checked={autoModels}
                onChange={(e) => {
                  const on = e.target.checked
                  setAutoModels(on)
                  void SaveAutoModels(on)
                }}
              />
              Auto-models (
              {isLocalProvider(activeProvider)
                ? '7b ↔ larger'
                : activeProvider === 'zai'
                ? 'free → 5.3'
                : activeProvider === 'openrouter'
                  ? 'flash → coder'
                  : activeProvider === 'qwen'
                  ? 'plus → max'
                  : 'flash / pro / vision'}
              )
            </label>
            <label
              className="nc-top-check"
              title="Спрашивать перед write_file, run_terminal, git_push, ssh_exec"
            >
              <input
                type="checkbox"
                checked={toolConfirm}
                onChange={(e) => {
                  const on = e.target.checked
                  setToolConfirm(on)
                  void SaveToolConfirm(on)
                }}
              />
              Confirm dangerous tools
            </label>
            <label
              className="nc-top-check"
              title="Plan: агент только исследует и предлагает план, без правок и shell"
            >
              <input
                type="checkbox"
                checked={planMode}
                onChange={(e) => {
                  const on = e.target.checked
                  setPlanMode(on)
                  void SavePlanMode(on)
                }}
              />
              Plan mode (без правок)
            </label>

            {activeProvider === 'deepseek' ? (
              <>
                <div className="nc-section-label">DeepSeek</div>
                <label>
                  Model
                  <select
                    value={deepseekModel}
                    onChange={(e) => {
                      const next = e.target.value
                      setDeepseekModel(next)
                      if (autoModels) {
                        setAutoModels(false)
                        void SaveAutoModels(false)
                      }
                      void SaveDeepSeekModel(next)
                    }}
                  >
                    {modelOptions(deepseekModels, deepseekModel).map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))}
                  </select>
                </label>
                <button type="button" className="nc-ghost" onClick={() => void refreshDeepSeekModels({applyPreferred: true})}>
                  Refresh models
                </button>
                <label>
                  API key
                  <input
                    type="password"
                    value={deepseekKey}
                    onChange={(e) => setDeepseekKey(e.target.value)}
                    placeholder={deepseekKeySet ? '•••• set' : 'sk-...'}
                  />
                </label>
                <p className="nc-help">
                  Ключ хранится в {secretsBackend || 'системном хранилище'}, не в settings.json.
                  {deepseekKeySet ? ' Сменить: вставьте новый и Save. ' : ' '}
                  {deepseekKeySet ? <button type="button" className="nc-ghost" onClick={() => void clearProviderKey('deepseek')}>Clear key</button> : null}
                </p>
              </>
            ) : activeProvider === 'openrouter' ? (
              <>
                <div className="nc-section-label">OpenRouter</div>
                <p className="nc-help">
                  Prepaid: ключ на <code>openrouter.ai/keys</code>, баланс на{' '}
                  <code>openrouter.ai/credits</code> (мин. ~$5). Суффикс <code>:floor</code> — самый дешёвый провайдер.
                  Endpoint: <code>https://openrouter.ai/api/v1</code>.
                </p>
                <label>
                  Model
                  <select
                    value={openrouterModels.includes(openrouterModel) ? openrouterModel : openrouterModel}
                    onChange={(e) => {
                      const next = e.target.value
                      setOpenrouterModel(next)
                      if (autoModels) {
                        setAutoModels(false)
                        void SaveAutoModels(false)
                      }
                      void SaveOpenRouterModel(next)
                    }}
                  >
                    {modelOptions(openrouterModels, openrouterModel).map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))}
                  </select>
                </label>
                <label>
                  Custom model id
                  <input
                    type="text"
                    value={openrouterModel}
                    onChange={(e) => setOpenrouterModel(e.target.value)}
                    onBlur={(e) => {
                      const next = e.target.value.trim()
                      if (next) void SaveOpenRouterModel(next)
                    }}
                    placeholder="qwen/qwen3-coder-flash:floor"
                  />
                </label>
                <button type="button" className="nc-ghost" onClick={() => void refreshOpenRouterModels()}>Refresh models</button>
                <button type="button" className="nc-ghost" onClick={() => void refreshOpenRouterBalance()}>Refresh balance</button>
                {orBalance && (
                  <p className="nc-help">
                    {orBalance.ok
                      ? `Кредиты: ${fmtUsd(orBalance.availableUsd)} (${orBalance.detail})`
                      : orBalance.detail || 'Баланс недоступен через API'}
                  </p>
                )}
                <p className="nc-help">
                  Для агента нужны модели с tool calling (qwen3-coder*).{' '}
                  <code>qwen/qwen-2.5-coder-32b-instruct</code> сейчас без tools — не для agent loop.
                  Стоимость берём из <code>usage.cost</code> ответа OpenRouter, когда есть.
                </p>
                <label>
                  API key
                  <input
                    type="password"
                    value={openrouterKey}
                    onChange={(e) => setOpenrouterKey(e.target.value)}
                    placeholder={openrouterKeySet ? '•••• set' : 'sk-or-v1-...'}
                  />
                </label>
                <p className="nc-help">
                  Ключ хранится в {secretsBackend || 'системном хранилище'}.
                  {openrouterKeySet ? <button type="button" className="nc-ghost" onClick={() => void clearProviderKey('openrouter')}>Clear key</button> : null}
                </p>
              </>
            ) : activeProvider === 'qwen' ? (
              <>
                <div className="nc-section-label">Qwen (DashScope)</div>
                <p className="nc-help">
                  Alibaba Cloud Model Studio. Ключ — в Model Studio (DashScope) → API-KEY.
                  OpenAI-совместимый режим <code>/compatible-mode/v1</code>, tool calling из коробки.
                </p>
                <label>
                  Endpoint
                  <select
                    value={qwenEndpoint}
                    onChange={(e) => {
                      const next = e.target.value === 'cn' ? 'cn' : 'intl'
                      setQwenEndpoint(next)
                      void SaveQwenEndpoint(next)
                    }}
                  >
                    <option value="intl">International (dashscope-intl)</option>
                    <option value="cn">China (dashscope)</option>
                  </select>
                </label>
                <label>
                  Model
                  <select
                    value={qwenModels.includes(qwenModel) ? qwenModel : qwenModel}
                    onChange={(e) => void applyModel(e.target.value)}
                  >
                    {modelOptions(qwenModels, qwenModel).map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))}
                  </select>
                </label>
                <label>
                  Custom model id
                  <input
                    type="text"
                    value={qwenModel}
                    onChange={(e) => setQwenModel(e.target.value)}
                    onBlur={(e) => {
                      const next = e.target.value.trim()
                      if (next) void SaveQwenModel(next)
                    }}
                    placeholder="qwen-plus"
                  />
                </label>
                <button type="button" className="nc-ghost" onClick={() => void refreshQwenModels()}>Refresh models</button>
                <p className="nc-help">
                  Для агента нужны модели с tool calling (<code>qwen-max</code>, <code>qwen-plus</code>,{' '}
                  <code>qwen-turbo</code>, <code>qwen3-coder-*</code>). Баланса через API нет — смотрите
                  консоль Model Studio. Стоимость считаем по нашему тарифу (usage.cost DashScope не отдаёт).
                </p>
                <label>
                  API key
                  <input
                    type="password"
                    value={qwenKey}
                    onChange={(e) => setQwenKey(e.target.value)}
                    placeholder={qwenKeySet ? '•••• set' : 'sk-...'}
                  />
                </label>
                <p className="nc-help">
                  Ключ хранится в {secretsBackend || 'системном хранилище'}.
                  {qwenKeySet ? <button type="button" className="nc-ghost" onClick={() => void clearProviderKey('qwen')}>Clear key</button> : null}
                </p>
              </>
            ) : isLocalProvider(activeProvider) ? (
              <>
                <div className="nc-section-label">Custom servers</div>
                <p className="nc-help">
                  Свои серверы (Custom): Ollama / LM Studio / vLLM. Список — в <code>settings.json</code>
                  (<code>localEndpoints</code>). По умолчанию полный агент (как облако). Галочка
                  <b> Custom lite</b> — короткий system + меньше tools (быстрее, но модель чаще гадает).
                  Auto-models: модели с capability <code>tools</code>. Если ответы одинаковые —
                  <b>новый чат</b> (старые «Вероятно…» в истории заражают следующие ответы).
                  Подробно, простыми словами — <code>docs/local-models-ru.md</code>.
                </p>
                <label
                  className="nc-top-check"
                  style={{marginBottom: 8}}
                  title="Custom lite: короткий промпт и ~12 tools (быстрее, но слабее на tool-calling). Выкл = полный агент как у облака — лучше для qwen-coder 7B/14B."
                >
                  <input
                    type="checkbox"
                    checked={localLite}
                    onChange={(e) => {
                      const on = e.target.checked
                      setLocalLite(on)
                      void SaveLocalLite(on)
                    }}
                  />
                  Custom lite <span className="nc-help" style={{display: 'inline'}}>(та же галочка, что в топбаре)</span>
                </label>
                <div style={{display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 8}}>
                  <button
                    type="button"
                    className="nc-ghost"
                    onClick={() => {
                      void (async () => {
                        try {
                          const row = await UpsertLocalEndpoint('', 'New local', LOCAL_BASE_DEFAULT, '')
                          const ep: LocalEndpointRow = {
                            id: String(row.id),
                            name: String(row.name || 'New local'),
                            baseUrl: String(row.baseUrl || LOCAL_BASE_DEFAULT),
                            model: String(row.model || ''),
                            keySet: Boolean(row.keySet),
                          }
                          setLocalEndpoints((prev) => [...prev.filter((x) => x.id !== ep.id), ep])
                          await applyProvider(`local:${ep.id}`)
                          setLocalEpName(ep.name)
                          setLocalBaseUrl(ep.baseUrl)
                          setLocalModel(ep.model)
                          setLocalKeySet(false)
                        } catch (e) {
                          console.error(e)
                        }
                      })()
                    }}
                  >
                    + Add server
                  </button>
                  <button
                    type="button"
                    className="nc-ghost"
                    onClick={() => {
                      const id = activeProvider.startsWith('local:') ? activeProvider.slice(6) : 'default'
                      void (async () => {
                        try {
                          const row = await DuplicateLocalEndpoint(id)
                          const ep: LocalEndpointRow = {
                            id: String(row.id),
                            name: String(row.name || 'copy'),
                            baseUrl: String(row.baseUrl || LOCAL_BASE_DEFAULT),
                            model: String(row.model || ''),
                            keySet: false,
                          }
                          setLocalEndpoints((prev) => [...prev, ep])
                          await applyProvider(`local:${ep.id}`)
                          setLocalEpName(ep.name)
                          setLocalBaseUrl(ep.baseUrl)
                          setLocalModel(ep.model)
                          setLocalKeySet(false)
                        } catch (e) {
                          console.error(e)
                        }
                      })()
                    }}
                  >
                    Duplicate
                  </button>
                  <button
                    type="button"
                    className="nc-ghost"
                    disabled={localEndpoints.length <= 1}
                    onClick={() => {
                      const id = activeProvider.startsWith('local:') ? activeProvider.slice(6) : 'default'
                      if (!window.confirm(`Удалить свой сервер «${localEpName}»?`)) return
                      void (async () => {
                        try {
                          await RemoveLocalEndpoint(id)
                          const next = localEndpoints.filter((x) => x.id !== id)
                          setLocalEndpoints(next)
                          const fallback = next[0]
                          if (fallback) {
                            await applyProvider(`local:${fallback.id}`)
                            setLocalEpName(fallback.name)
                            setLocalBaseUrl(fallback.baseUrl)
                            setLocalModel(fallback.model)
                            setLocalKeySet(fallback.keySet)
                          }
                        } catch (e) {
                          console.error(e)
                        }
                      })()
                    }}
                  >
                    Delete
                  </button>
                </div>
                <label>
                  Display name
                  <input
                    type="text"
                    value={localEpName}
                    onChange={(e) => setLocalEpName(e.target.value)}
                    onBlur={(e) => {
                      const name = e.target.value.trim() || 'Custom'
                      setLocalEpName(name)
                      const id = activeProvider.startsWith('local:') ? activeProvider.slice(6) : 'default'
                      void UpsertLocalEndpoint(id, name, localBaseUrl, localModel).then((row) => {
                        setLocalEndpoints((prev) => prev.map((x) => x.id === id ? {
                          ...x,
                          name: String(row.name || name),
                          baseUrl: String(row.baseUrl || localBaseUrl),
                          model: String(row.model || localModel),
                        } : x))
                      })
                    }}
                  />
                </label>
                <label>
                  Preset
                  <select
                    value={localPresetId(localBaseUrl)}
                    onChange={(e) => {
                      const pid = e.target.value
                      const preset = LOCAL_PRESETS.find((p) => p.id === pid)
                      if (!preset || preset.id === 'custom') return
                      setLocalBaseUrl(preset.url)
                      void SaveLocalBaseURL(preset.url).then(() => refreshLocalModels({applyPreferred: true}))
                    }}
                  >
                    {LOCAL_PRESETS.map((p) => (
                      <option key={p.id} value={p.id}>{p.label}</option>
                    ))}
                  </select>
                </label>
                <label>
                  Base URL
                  <input
                    type="text"
                    value={localBaseUrl}
                    onChange={(e) => setLocalBaseUrl(e.target.value)}
                    onBlur={(e) => {
                      const next = e.target.value.trim() || LOCAL_BASE_DEFAULT
                      setLocalBaseUrl(next)
                      void SaveLocalBaseURL(next)
                      const id = activeProvider.startsWith('local:') ? activeProvider.slice(6) : 'default'
                      setLocalEndpoints((prev) => prev.map((x) => x.id === id ? {...x, baseUrl: next} : x))
                    }}
                    placeholder={LOCAL_BASE_DEFAULT}
                  />
                </label>
                <label>
                  Model
                  <select
                    value={localModel}
                    onChange={(e) => {
                      void applyModel(e.target.value)
                    }}
                  >
                    {localModels.length === 0 && !localModel ? (
                      <option value="">— Refresh models —</option>
                    ) : null}
                    {modelOptions(localModels, localModel).map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))}
                  </select>
                </label>
                <label>
                  Custom model id
                  <input
                    type="text"
                    value={localModel}
                    onChange={(e) => setLocalModel(e.target.value)}
                    onBlur={(e) => {
                      const next = e.target.value.trim()
                      void applyModel(next)
                      const id = activeProvider.startsWith('local:') ? activeProvider.slice(6) : 'default'
                      setLocalEndpoints((prev) => prev.map((x) => x.id === id ? {...x, model: next} : x))
                    }}
                    placeholder="qwen2.5-coder:14b"
                  />
                </label>
                <button type="button" className="nc-ghost" onClick={() => void refreshLocalModels({applyPreferred: true})}>
                  Refresh models
                </button>
                <label>
                  API token (optional)
                  <input
                    type="password"
                    value={localKey}
                    onChange={(e) => setLocalKey(e.target.value)}
                    placeholder={localKeySet ? '•••• set' : 'обычно пусто для localhost'}
                  />
                </label>
                <p className="nc-help">
                  Токен — отдельно на каждый сервер ({secretsBackend || 'системное хранилище'}).
                  {localKeySet ? (
                    <button type="button" className="nc-ghost" onClick={() => void clearProviderKey(activeProvider)}>Clear token</button>
                  ) : null}
                </p>
              </>
            ) : (
              <>
                <div className="nc-section-label">Z.ai</div>
                <label>
                  Endpoint
                  <select
                    value={zaiEndpoint}
                    onChange={(e) => {
                      const next = e.target.value === 'paas' ? 'paas' : 'coding'
                      setZaiEndpoint(next)
                      void SaveZaiEndpoint(next).then(() => refreshZaiModels({applyPreferred: true}))
                    }}
                    title="Coding Plan: api.z.ai/api/coding/paas/v4 · Pay-as-you-go: api.z.ai/api/paas/v4"
                  >
                    <option value="paas">Pay-as-you-go (рекомендуется)</option>
                    <option value="coding">Coding Plan</option>
                  </select>
                </label>
                <p className="nc-help">
                  Обычный баланс: <code>…/paas/v4</code>. DevPack / subscription: <code>…/coding/paas/v4</code>.
                </p>
                <label>
                  Model
                  <select
                    value={zaiModels.includes(zaiModel) ? zaiModel : zaiModel}
                    onChange={(e) => {
                      const next = e.target.value
                      setZaiModel(next)
                      if (autoModels) {
                        setAutoModels(false)
                        void SaveAutoModels(false)
                      }
                      void SaveZaiModel(next)
                    }}
                  >
                    {modelOptions(zaiModels, zaiModel).map((m) => (
                      <option key={m} value={m}>
                        {m.endsWith('-flash') && (m === 'glm-4.7-flash' || m === 'glm-4.5-flash') ? `${m} (free)` : m}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Custom model id
                  <input
                    type="text"
                    value={zaiModel}
                    onChange={(e) => setZaiModel(e.target.value)}
                    onBlur={(e) => {
                      const next = e.target.value.trim()
                      if (next) void SaveZaiModel(next)
                    }}
                    placeholder="glm-4.7-flash"
                  />
                </label>
                <button type="button" className="nc-ghost" onClick={() => void refreshZaiModels()}>Refresh models</button>
                <button type="button" className="nc-ghost" onClick={() => void refreshZaiBalance()}>Refresh balance</button>
                {zaiBalance && (
                  <p className="nc-help">
                    {zaiBalance.ok && zaiBalance.source === 'credit_grants'
                      ? `Баланс / grants: ${fmtUsd(zaiBalance.availableUsd)} (${zaiBalance.detail})`
                      : zaiBalance.detail || 'Баланс недоступен через API'}
                  </p>
                )}
                <p className="nc-help">
                  Бесплатно на Pay-as-you-go (по pricing Z.ai): <code>glm-4.7-flash</code>, <code>glm-4.5-flash</code>.
                  Их часто нет в <code>GET /models</code> — мы всё равно добавляем в список. <code>glm-5.3*</code> платные (нужен баланс).
                  Ошибка 1113 = нет денег на payg; либо free-модель, либо пополни баланс, либо Endpoint → Coding Plan (если есть подписка).
                </p>
                <p className="nc-help">
                  Список с <code>GET /models</code> (+ free flash, если каталог их скрыл). Ручной выбор модели сохраняется;
                  бесплатная подставляется только если сохранённой id нет в списке.
                </p>
                <label>
                  API key
                  <input
                    type="password"
                    value={zaiKey}
                    onChange={(e) => setZaiKey(e.target.value)}
                    placeholder={zaiKeySet ? '•••• set' : 'zai-...'}
                  />
                </label>
                <p className="nc-help">
                  Ключ хранится в {secretsBackend || 'системном хранилище'}.
                  {zaiKeySet ? <button type="button" className="nc-ghost" onClick={() => void clearProviderKey('zai')}>Clear key</button> : null}
                </p>
              </>
            )}
            {appDataDir ? (
              <p className="nc-help">
                Данные приложения: <code>{appDataDir}</code>
                <br />
                Чаты: <code>chats</code>, архивы: <code>chat_archive</code>, настройки: <code>settings.json</code> (без API-ключей)
              </p>
            ) : null}
            <button type="button" onClick={saveSettings}>Save settings</button>
            <button type="button" className="nc-ghost" onClick={testConnect}>Test connect ({providerLabel})</button>
            <button type="button" className="nc-ghost" onClick={() => void clearAllKeys()}>Clear all keys</button>

            <div className="nc-section-label">Agent</div>
            <label>
              Max tool steps
              <input
                type="number"
                min={1}
                max={5000}
                disabled={unlimitedSteps}
                value={unlimitedSteps ? '' : maxSteps}
                placeholder="120"
                onChange={(e) => setMaxSteps(Number(e.target.value))}
              />
            </label>
            <label className="nc-inline-check">
              <input
                type="checkbox"
                checked={unlimitedSteps}
                onChange={(e) => setMaxSteps(e.target.checked ? -1 : 120)}
              />
              Без ограничения шагов (только предупреждение)
            </label>
            <label>
              Предупреждать после
              <input
                type="number"
                min={0}
                max={5000}
                value={warnSteps}
                onChange={(e) => setWarnSteps(Number(e.target.value))}
              />
            </label>
            <p className="nc-help">
              Шагов за один запрос: по умолчанию 120. С галочкой «без ограничения» агент не
              останавливается, а после указанного порога пишет, что работа продолжается. 0 в пороге —
              предупреждений нет.
            </p>

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
              Показывать терминал (Ctrl-Shift-T)
            </label>
            <p className="nc-help">По умолчанию скрыт — как в Cursor: задачи делает агент через tools.</p>
            <div className="nc-section-label">Shell</div>
            <p className="nc-help">
              Интерактивный терминал через системный PTY (Windows ConPTY / macOS pty). Пустое поле — авто:
              PowerShell&nbsp;7 или&nbsp;5 на Windows, <code>$SHELL</code> на macOS/Linux.
            </p>
            <label>
              Путь к shell
              <input
                value={shellPath}
                placeholder={shellDetected || 'auto'}
                onChange={(e) => setShellPath(e.target.value)}
                spellCheck={false}
              />
            </label>
            {shellDetected ? (
              <p className="nc-help">Обнаружен: <code>{shellDetected}</code></p>
            ) : null}
            <div className="nc-shell-actions">
              <button
                type="button"
                className="nc-ghost"
                disabled={shellSaving}
                onClick={() => {
                  void DetectDefaultShell()
                    .then((p) => {
                      const path = String(p || '')
                      setShellDetected(path)
                      setShellPath('')
                    })
                    .catch(() => undefined)
                }}
              >
                Авто (обнаружить)
              </button>
              <button
                type="button"
                disabled={shellSaving}
                onClick={() => {
                  setShellSaving(true)
                  void SaveShell(shellPath.trim())
                    .then(async () => {
                      const d = await DetectDefaultShell().catch(() => '')
                      setShellDetected(String(d || shellDetected))
                      if (showTerm) {
                        await StartTerminal().catch(() => undefined)
                        const t = xtermRef.current
                        if (t) {
                          try { fitRef.current?.fit() } catch { /* ignore */ }
                          TerminalResize(t.cols, t.rows).catch(() => undefined)
                        }
                      }
                    })
                    .catch((e) => {
                      if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
                    })
                    .finally(() => setShellSaving(false))
                }}
              >
                {shellSaving ? 'Сохранение…' : 'Сохранить shell'}
              </button>
            </div>

            <div className="nc-section-label">Git &amp; SSH</div>
            <p className="nc-help">
              Как в Cursor: логины не хранятся в приложении. Push/pull идут через системный{' '}
              <code>git</code> (credential helper / Git Credential Manager). SSH — ключи из{' '}
              <code>~/.ssh</code> или ssh-agent. Настройте доступ один раз в системе — агент подхватит его сам.
            </p>

            <div className="nc-section-label">Cursor Rules</div>
            <p className="nc-help">
              Правила читаются в нативном формате Cursor (<code>.mdc</code>/<code>.md</code> + frontmatter)
              из <code>~/.cursor/rules</code> и <code>&lt;project&gt;/.cursor/rules</code> (и legacy{' '}
              <code>.cursorrules</code> / <code>AGENTS.md</code>). Тело с{' '}
              <code>alwaysApply: true</code> (и без description/globs) попадает в системный промпт целиком;
              остальные — каталогом «name — description», пока пути в запросе не совпадут с globs.
              Двойной клик по имени — просмотр и правка файла.
            </p>
            <ul className="nc-rules-list">
              {asList(rulesInfo.global).map((r) => (
                <li
                  key={`g:${r.path}`}
                  className="nc-rules-item"
                  title={`${r.path}\n(двойной клик — открыть)`}
                  onDoubleClick={() => void openRuleEditor(r)}
                >
                  <span className="nc-rules-source global">global</span>
                  <span className="nc-rules-path">{r.name}</span>
                  {r.alwaysApply && <span className="nc-rules-always">always</span>}
                </li>
              ))}
              {asList(rulesInfo.project).map((r) => (
                <li
                  key={`p:${r.path}`}
                  className="nc-rules-item"
                  title={`${r.path}\n(двойной клик — открыть)`}
                  onDoubleClick={() => void openRuleEditor(r)}
                >
                  <span className="nc-rules-source project">project</span>
                  <span className="nc-rules-path">{r.name}</span>
                  {r.alwaysApply && <span className="nc-rules-always">always</span>}
                </li>
              ))}
              {asList(rulesInfo.global).length + asList(rulesInfo.project).length === 0 && (
                <li className="nc-empty">Правила не найдены.</li>
              )}
            </ul>
            <button type="button" className="nc-ghost" onClick={() => void refreshRules(true)}>Reload rules</button>
          </aside>
        )}
      </div>

      {welcome?.show && (
        <div className="nc-modal-backdrop" role="presentation">
          <div className="nc-modal nc-welcome" role="dialog" aria-labelledby="nc-welcome-title">
            <p className="nc-welcome-eyebrow">{welcome.eyebrow}</p>
            <h1 id="nc-welcome-title" className="nc-welcome-title">{welcome.name}</h1>
            <p className="nc-welcome-ver">{welcome.versionLabel} {welcome.version}</p>
            <p className="nc-section-label">{welcome.whatsNew}</p>
            <ol className="nc-welcome-list">
              {welcome.highlights.map((h, i) => (
                <li key={`${i}-${h.slice(0, 24)}`}>{h}</li>
              ))}
            </ol>
            <button
              type="button"
              className="nc-welcome-go"
              onClick={() => {
                setWelcome(null)
                void AckWelcome()
              }}
            >
              {welcome.continueLabel}
            </button>
          </div>
        </div>
      )}

      {showPrices && (
        <div className="nc-modal-backdrop" role="presentation" onClick={() => setShowPrices(false)}>
          <div
            className="nc-modal nc-prices"
            role="dialog"
            aria-labelledby="nc-prices-title"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="nc-prices-head">
              <h2 id="nc-prices-title">Model prices</h2>
            </div>
            <p className="nc-help">
              USD за 1M токенов · от слабых к сильным. DeepSeek Flash с 10.09.2026: peak miss $0.30 / out $1.20
              (off-peak ≈ ½); пик по пекинскому расписанию, в note — локальные часы. OpenRouter mid-market; факт
              может быть из <code>usage.cost</code>.
            </p>
            {pricesLoading && <p className="nc-muted">Загрузка…</p>}
            {!pricesLoading && (
              <div className="nc-prices-table-wrap">
                <table className="nc-prices-table">
                  <thead>
                    <tr>
                      <th>Provider</th>
                      <th>Model</th>
                      <th>Input</th>
                      <th>Output</th>
                      <th>Note</th>
                    </tr>
                  </thead>
                  <tbody>
                    {modelPrices.map((r) => (
                      <tr key={`${r.provider}-${r.model}`}>
                        <td>{r.provider}</td>
                        <td><code>{r.model}</code></td>
                        <td>{fmtPrice1M(r.inputUsd, r.free)}</td>
                        <td>{fmtPrice1M(r.outputUsd, r.free)}</td>
                        <td className="nc-muted">{r.note || (r.free ? 'free' : '')}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="nc-prices-foot">
              <button type="button" className="nc-prices-close" onClick={() => setShowPrices(false)}>
                Закрыть
              </button>
            </div>
          </div>
        </div>
      )}

      {ruleEdit && (
        <div
          className="nc-modal-backdrop"
          role="presentation"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget && !ruleEdit.saving) setRuleEdit(null)
          }}
        >
          <div
            className="nc-modal nc-rule-edit"
            role="dialog"
            aria-labelledby="nc-rule-edit-title"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="nc-rule-edit-head">
              <div>
                <h2 id="nc-rule-edit-title">{ruleEdit.name}</h2>
                <p className="nc-help nc-rule-edit-path" title={ruleEdit.path}>
                  {ruleEdit.source} · {ruleEdit.path}
                </p>
              </div>
              <button type="button" className="nc-ghost" disabled={ruleEdit.saving} onClick={() => setRuleEdit(null)}>
                Close
              </button>
            </div>
            <textarea
              className="nc-rule-edit-body"
              value={ruleEdit.text}
              spellCheck={false}
              onChange={(e) => setRuleEdit((prev) => prev ? {...prev, text: e.target.value} : prev)}
            />
            {ruleEdit.error ? <p className="nc-rule-edit-err">{ruleEdit.error}</p> : null}
            <div className="nc-rule-edit-actions">
              <button type="button" className="nc-ghost" disabled={ruleEdit.saving} onClick={() => setRuleEdit(null)}>
                Отмена
              </button>
              <button type="button" disabled={ruleEdit.saving} onClick={() => void saveRuleEditor()}>
                {ruleEdit.saving ? 'Сохранение…' : 'Сохранить'}
              </button>
            </div>
          </div>
        </div>
      )}

      {projectCtx && (
        <div
          className="nc-ctx-menu"
          style={{left: projectCtx.x, top: projectCtx.y}}
          role="menu"
          onMouseDown={(e) => e.stopPropagation()}
          onClick={(e) => e.stopPropagation()}
        >
          <button
            type="button"
            role="menuitem"
            onMouseDown={(e) => e.stopPropagation()}
            onClick={() => void requestCloseProject(projectCtx.path, projectCtx.name)}
          >
            Закрыть папку проекта
          </button>
        </div>
      )}

      {pendingCloseChatId && (
        <div className="nc-modal-backdrop" role="presentation" onClick={() => setPendingCloseChatId(null)}>
          <div
            className="nc-modal nc-confirm"
            role="dialog"
            aria-labelledby="nc-close-chat-title"
            onClick={(e) => e.stopPropagation()}
          >
            <p className="nc-confirm-app">{info.name || 'NotCursor.ai'}</p>
            <h2 id="nc-close-chat-title">Хотите закрыть текущий чат?</h2>
            <p className="nc-help">Чат будет удалён с диска без архива.</p>
            <div className="nc-close-project-actions">
              <button type="button" className="nc-danger" onClick={() => void confirmCloseChat()}>
                Закрыть
              </button>
              <button type="button" className="nc-ghost" onClick={() => setPendingCloseChatId(null)}>
                Отмена
              </button>
            </div>
          </div>
        </div>
      )}

      {toolAsk && (
        <div className="nc-modal-backdrop" role="presentation">
          <div
            className="nc-modal nc-confirm"
            role="dialog"
            aria-labelledby="nc-tool-ask-title"
            onClick={(e) => e.stopPropagation()}
          >
            {toolAsk.name === 'ask_user' ? (
              <AskUserDialog
                sessionId={toolAsk.sessionId}
                callId={toolAsk.callId}
                args={toolAsk.args}
                answer={askAnswer}
                setAnswer={setAskAnswer}
                onDone={() => { setToolAsk(null); setAskAnswer('') }}
              />
            ) : (
              <>
                <p className="nc-confirm-app">{info.name || 'NotCursor.ai'}</p>
                <h2 id="nc-tool-ask-title">Разрешить «{toolAsk.name}»?</h2>
                <p className="nc-help">Агент хочет выполнить потенциально опасное действие.</p>
                <pre className="nc-tool-ask-args">{toolAsk.args || '(no args)'}</pre>
                <div className="nc-close-project-actions">
                  <button
                    type="button"
                    onClick={() => {
                      const ask = toolAsk
                      setToolAsk(null)
                      ResolveToolApproval(ask.sessionId, ask.callId, true)
                    }}
                  >
                    Разрешить
                  </button>
                  <button
                    type="button"
                    className="nc-danger"
                    onClick={() => {
                      const ask = toolAsk
                      setToolAsk(null)
                      ResolveToolApproval(ask.sessionId, ask.callId, false)
                    }}
                  >
                    Запретить
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {closeProjectDlg && (
        <div className="nc-modal-backdrop" role="presentation" onClick={() => setCloseProjectDlg(null)}>
          <div
            className="nc-modal nc-close-project"
            role="dialog"
            aria-labelledby="nc-close-project-title"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 id="nc-close-project-title">Закрыть проект «{closeProjectDlg.name}»?</h2>
            <p className="nc-help">
              В проекте есть чаты. Удалить их с диска, архивировать или отменить закрытие?
            </p>
            <div className="nc-close-project-actions">
              <button
                type="button"
                className="nc-danger"
                onClick={() => void finishCloseProject(closeProjectDlg.path, 'delete').catch((e) => {
                  if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
                })}
              >
                Удалить чаты
              </button>
              <button
                type="button"
                onClick={() => void finishCloseProject(closeProjectDlg.path, 'archive').catch((e) => {
                  if (activeSessionId) setSessionItems(activeSessionId, (m) => [...m, {kind: 'system', content: String(e)}])
                })}
              >
                Архивировать чаты
              </button>
              <button type="button" className="nc-ghost" onClick={() => setCloseProjectDlg(null)}>
                Отмена
              </button>
            </div>
          </div>
        </div>
      )}
      {showArchives && !archiveView && (
        <div className="nc-modal-backdrop" role="presentation" onClick={() => setShowArchives(false)}>
          <div
            className="nc-modal nc-archives"
            role="dialog"
            aria-labelledby="nc-archives-title"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="nc-archives-head">
              <h2 id="nc-archives-title">Архивы чатов</h2>
              <button type="button" className="nc-prices-close" onClick={() => setShowArchives(false)}>
                ✕
              </button>
            </div>
            <p className="nc-help">
              Прочитайте архивный чат — он откроется в режиме просмотра и не попадёт в активные чаты. Нужный
              фрагмент можно скопировать и вставить в текущий чат.
              {appDataDir ? (
                <>
                  {' '}Файлы: <code>{appDataDir}\chat_archive</code>
                </>
              ) : null}
            </p>
            <div className="nc-archives-search">
              <input
                type="text"
                placeholder="Поиск по названию проекта или чата…"
                value={archiveProjectFilter}
                onChange={(e) => setArchiveProjectFilter(e.target.value)}
                autoFocus
              />
            </div>
            <div className="nc-archives-scroll">
              {(() => {
                const q = archiveProjectFilter.trim().toLowerCase()
                const projects = [...new Set(archives.map((c) => c.projectName))]
                const filtered = archives.filter((c) => {
                  if (!q) return true
                  return c.title.toLowerCase().includes(q) || c.projectName.toLowerCase().includes(q)
                })
                return projects.map((p) => {
                  const chats = filtered.filter((c) => c.projectName === p)
                  if (chats.length === 0) return null
                  return (
                    <div key={p} className="nc-archives-project">
                      <div className="nc-archives-project-name">{p}</div>
                      {chats.map((c) => (
                        <button
                          key={c.id}
                          type="button"
                          className="nc-archives-chat"
                          onClick={() => setArchiveView(c)}
                        >
                          <span className="nc-archives-chat-title">{c.title}</span>
                          <span className="nc-archives-chat-meta">
                            {c.archivedAt} · {c.messageCount} сообщ.
                          </span>
                        </button>
                      ))}
                    </div>
                  )
                })
              })()}
              {archives.length === 0 && <div className="nc-archives-empty">Архивов пока нет</div>}
            </div>
            <div className="nc-archives-foot">
              <button type="button" className="nc-ghost" onClick={() => setShowArchives(false)}>
                Закрыть
              </button>
            </div>
          </div>
        </div>
      )}
      {showArchives && archiveView && (
        <div className="nc-modal-backdrop" role="presentation" onClick={() => setShowArchives(false)}>
          <div
            className="nc-modal nc-archives nc-archives-readonly"
            role="dialog"
            aria-labelledby="nc-archive-view-title"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="nc-archives-head">
              <div>
                <h2 id="nc-archive-view-title">{archiveView.title}</h2>
                <p className="nc-help">
                  {archiveView.projectName} · архивирован {archiveView.archivedAt} · {archiveView.messageCount}{' '}
                  сообщ.
                </p>
              </div>
              <button type="button" className="nc-prices-close" onClick={() => setShowArchives(false)}>
                ✕
              </button>
            </div>
            <div className="nc-archives-scroll nc-archives-readonly-scroll">
              {archiveView.items.map((m, i) => {
                if (m.kind === 'user' || m.kind === 'system' || m.kind === 'reasoning') {
                  return (
                    <div key={i} className="nc-arc-msg nc-arc-msg-plain">
                      <div className="nc-arc-role">
                        {m.kind === 'user' ? 'Вы' : m.kind === 'system' ? 'Система' : 'Размышления'}
                      </div>
                      <div className="nc-arc-content">{m.content}</div>
                    </div>
                  )
                }
                if (m.kind === 'assistant') {
                  return (
                    <div key={i} className="nc-arc-msg nc-arc-msg-assistant">
                      <div className="nc-arc-role">Агент</div>
                      <div className="nc-arc-content">
                        <Markdown content={m.content} />
                      </div>
                    </div>
                  )
                }
                if (m.kind === 'tool') {
                  return (
                    <div key={i} className="nc-arc-msg nc-arc-msg-tool">
                      <div className="nc-arc-role">Инструмент: {m.name}</div>
                      <div className="nc-arc-content">{String(m.result ?? m.args ?? '')}</div>
                    </div>
                  )
                }
                if (m.kind === 'file') {
                  return (
                    <div key={i} className="nc-arc-msg nc-arc-msg-file">
                      <div className="nc-arc-role">Файл: {m.path}</div>
                      <div className="nc-arc-content">{m.content}</div>
                    </div>
                  )
                }
                return null
              })}
            </div>
            <div className="nc-archives-foot">
              <button type="button" className="nc-ghost" onClick={() => setArchiveView(null)}>
                ← К списку архивов
              </button>
              <button type="button" className="nc-ghost" onClick={() => setShowArchives(false)}>
                Закрыть
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

