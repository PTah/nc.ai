<!-- Created by nc.ai 1.2.11 on Papasha 2026-11-14 12:13:24 -->
# NotCursor.ai

**English** · [Русский](README.md)

An AI coding agent in a dedicated window: chat, project tree, editor and a real terminal.
Built with **Wails v2 (Go) + React/TypeScript** — on top of the system WebView, without Electron and
without a 200 MB runtime sitting next to the exe.

The agent reads and edits files in your project, runs commands, commits and pushes through the system
`git`, and connects over SSH using keys from `~/.ssh`. Provider keys live in the system store (macOS
Keychain / Windows Credential Manager), chat history stays local. The only outbound traffic is the
request to the LLM provider you picked yourself.

**Version:** `1.2.12` · **Output:** `build/bin/NotCursor.exe` (Windows) and `build/bin/NotCursor.app` (macOS)

## Quick start

This section is for those who want to **build the app from source** (development, a custom host, a
custom build). If you just need a working NotCursor.ai, don't compile anything: grab a ready build from
the **releases** (`docs/releases/`, includes launch instructions). Everything below can be skipped.

Install these first:

- **Go** 1.26 (`go.mod` requires `go 1.26.0`) and **Node.js** with npm;
- **Wails CLI** 2.15.0 — `go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0`
  (so `wails` is on `PATH`: `%USERPROFILE%\go\bin` on Windows, `~/go/bin` on macOS);
- Windows: WebView2 Runtime (already present on current Windows builds). macOS: Xcode Command Line
  Tools — needed both for the build and for the ad-hoc `.app` signature.

Then, from the repository root:

```bash
cd frontend && npm install && cd ..
wails dev
```

The NotCursor window opens. Go to **Settings → Provider** and paste a DeepSeek key — or pick the Local
provider if Ollama/LM Studio is already running nearby. Without a key the window opens, but the chat
won't answer.

Check that the code builds and the tests are green:

```bash
go test ./...
```

### Building the binary

Windows:

```powershell
.\build.ps1                    # build and restart NotCursor.exe
.\build.ps1 -NoRestart         # build only
.\build.ps1 -CopyTo D:\Share   # build and copy the exe to a share folder
```

macOS (only built on a macOS host; the script does the ad-hoc codesign itself):

```bash
./build.sh                     # build, replace /Applications/NotCursor.app and restart
./build.sh --no-restart        # build only
./build.sh --universal         # fat binary arm64 + amd64
./build.sh --no-install        # leave the installed copy alone
./build.sh --install-to ~/Apps # install to your own folder instead of /Applications
./build.sh --copy-to ~/Share   # also copy the .app to a share folder
```

The script builds `build/bin/NotCursor.app`, signs it ad-hoc, **replaces**
`/Applications/NotCursor.app` with the fresh bundle (closing the old process) and launches the app from
`/Applications`. So the Dock/Spotlight icon and "Applications" always point to the latest build — no
separate installer, but no manual copying of `.app` either. The target folder can be changed with
`--install-to DIR` or the `NC_INSTALL_DIR` variable. If an agent run is in progress (`running.json` in
the data directory), the script **refuses** to close the client and suggests `--force-restart` —
restarting over a running agent cuts the answer off.

The full cycle — version, installing the build, artifacts, release and re-uploading under the same
number — is in [docs/build-and-release.md](docs/build-and-release.md).

## Providers

Switching — **Settings → Provider**.

| Provider | Models | What to know |
|---|---|---|
| DeepSeek | `deepseek-flash` (default), `deepseek-v4-pro`; legacy `deepseek-v4-flash` / `…-vision-exp` → Flash | OpenAI-compatible API, automatic model choice |
| Z.ai | `glm-4.5…5.3` (flash / pro / vision) | model list and balance are shown right in Settings |
| OpenRouter | any `openrouter/…` | your own key and endpoint |
| Qwen (DashScope) | `qwen-max` / `qwen-plus` / `qwen-turbo`, `qwen3-coder-*`, `qwen-vl-*` | Alibaba Cloud Model Studio, OpenAI-compatible; intl / cn region |
| Yandex AI Studio | `yandexgpt` / `yandexgpt-lite`, `qwen3-235b-a22b-fp8`, `gpt-oss-120b` | API key + folder id; URI `gpt://<folder>/<model>/latest` |
| Local | anything `/models` returns — Ollama, LM Studio, vLLM, llama.cpp | defaults to `http://127.0.0.1:11434/v1`, key optional |

- DeepSeek auto-choice: `deepseek-flash` by default; on complex requests / long runs —
  `deepseek-v4-pro`. Mid-run escalation to Pro on a tools dead-end / empty answer; the `/pro` or `[pro]`
  prefix means Pro right away. Multimodality — the same Flash.
- Provider errors become readable text: `402` → "Top up your balance…", `429` → rate limit with a wait
  derived from the headers: `Retry-After`, `x-ratelimit-reset-*` (duration or unix time),
  `anthropic-ratelimit-*-reset` (RFC3339), plus quota remaining and `limit_source` ("token limit").
  A short limit is waited out and retried automatically; a daily one returns an error with a hint right away.
- **Fallback provider**: if the primary hits a long limit or is overloaded, the run switches to the
  backup once and keeps working (the chat shows a "continuing on the fallback" line). Configured in
  Settings → Provider: fields **Fallback** and **Fallback: model** (empty = off; the `settings.json`
  equivalent is `"fallbackProvider": "deepseek"`, `"fallbackModel": "deepseek-v4-flash"`; Local is not
  used as a fallback when the request contains images).
- The **↻ Models** button re-reads the model list from the active provider. DeepSeek also goes through
  `GET /models` instead of taking the list from the code.
- Keys are never written to `settings.json`: macOS Keychain, Windows Credential Manager, on Linux a file
  with `0600` permissions. Wiped with Clear key / Clear all keys.

## Agent loop

Request → thinking / tool calls per the protocols in `docs/exchange-protocols/` → tool results → next
step, until the agent is done.

- Steps per request limit — **Settings → Agent**: 120 by default; the "no limit" checkbox removes the
  hard cap (the agent doesn't stop, but after the "Warn after N" threshold it says work continues).
  When the hard limit does run out, the agent does a soft wrap-up without tools: it answers with what it
  managed to find out.
- While working the agent sends **stages**: step x/y, elapsed, what's done and what's happening now.
  "Stage" cards in the transcript and the live status above the input appear **on request** ("show
  intermediate results", "by stages", "what are you doing") or automatically when the agent is silent
  with no output for more than two minutes.
- Messages typed while the agent is working **queue up for this chat** (each project has its own
  queue); the queue panel is always visible above the input. On a queue card:
  - **"Send"** — append the request **after the current run**: it goes into the live context between
    steps, the run is not interrupted and nothing already done is lost;
  - **"Parallel"** — start this request on a separate lane (multitask) without touching the current run;
  - `×` — remove from the queue.

  If you press nothing, the message is sent automatically after the current run finishes. Abort the run
  entirely with the **Stop** button in the chat header.
- **The message draft is kept.** Text typed but not sent is written to `composer-drafts.json` (one
  entry per chat) and restored after an app restart — an accidental close no longer eats what you wrote.
- **Closing while working asks for confirmation**: if an agent run is in progress or there's text in the
  input, the app shows a "Close the app?" dialog (default — "Cancel"). An interrupted answer is marked
  with a note in the chat — there's no silent truncation.
- **Run marker**: while the agent works the app holds `running.json` in the data directory, and
  `./build.sh` (or the restart script) won't close the client over a run — otherwise the tail of the
  answer is lost. A deliberate restart is `--force-restart` / `restart-app-when-idle.sh --yes --force`.
- Model reasoning is collapsed under "Thinking…": click to expand, hidden by default.
- Reconnect: network failures and `429` / `1302` / `1305` responses are retried with backoff. If that
  doesn't help either, a **Reconnect** button appears in the header.
- Compaction: results of old tools are folded into `[compacted] …`, the last three stay full — otherwise
  a long chat hits the context limit. Compaction runs between agent turns.
- Prefix-cache: stable system and turn-rules stay at the start of the prompt to hit the provider cache.
  Next to the cost in the header there's a `cache N%` chip — hits for the **current** chat; the full
  breakdown (`chat cache … hit / … miss (N%)` and `total cache … hit / … miss (N%)` lines) appears in the
  tooltip when hovering the chip. Until the provider returns any cache tokens the chip shows `cache —`,
  and the tooltip has no percentages — only zeros.
- **Network and connection drops** (Settings → Network): **HTTP protocol (LLM)** switches the protocol
  to providers — `Auto (HTTP/2)` or `HTTP/1.1` (h2 off: helps when a CDN or a bypass router cuts a long
  stream, "connection reset by peer" mid-answer). **HTTP/2 keepalive** (20 s by default) sends a PING
  after full silence so the connection doesn't fall asleep in NAT/DPI and a dead stream is visible right
  away. Next to it — **Reconnect attempts** (how many times to reconnect after a drop, default 3) and
  **Reconnect budget, sec** (how long to wait for a series of retries in total, default 180), plus a
  drop counter for the launch with the last error text and a reset button.
- **Stall watchdog** (Settings → Agent, "Abort a stuck step after, min"): if a step shows no signs of
  life — no model answer, no tool start/end, no stream deltas — for longer than the given time (10
  minutes by default), the run is **force-aborted** and the transcript gets a breakdown: at which step it
  stopped, how long it waited, in which phase (waiting for the model or a hung tool), which model and how
  many tools already ran, plus what to do about it. The "don't abort a stuck step" checkbox disables the
  control.

## Tools

| Tool | What it does |
|---|---|
| `read_file` / `write_file` / `apply_patch` / `delete_file` / `move_file` | read (with line numbers), write, patch, delete, rename inside the sandbox |
| `list_dir` / `find_files` / `glob` / `grep` | walk directories, search by fuzzy name, by glob (`**/*.go`), by content (regex like ripgrep) |
| `find_symbol` / `project_search` | find declarations and concepts via the project symbol index: `file:line` and a doc comment instead of a series of `grep` + `read_file` |
| `remember` / `recall` / `forget` | long-term project memory: one fact line + paths; the note is flagged if the file changed since it was written |
| `history_search` | search past chats of this project (live and archived) |
| `run_terminal` / `command_status` | shell commands: own `cwd`, background mode (`is_background`) |
| `todo_write` / `ask_user` / `read_lints` | task list, question to the user (one option is marked "recommended"), `go vet` on changed files |
| `switch_project` | switch to another project from the agent: the run finishes with a handoff, the app opens the project and starts a new run there |
| `create_project` | a turnkey new project from "create repo X and publish to Y": folder + `git init` with the first commit + an empty repo on GitHub/Gitea + `push -u origin master` + a dashboard project; the run moves there with a handoff |
| `web_search` / `fetch_url` | the web — with an SSRF guard: LAN, loopback and metadata addresses are blocked. Engine: DuckDuckGo (default) or DeepSeek Search & Fetch — Settings → Agent, with a fallback to DuckDuckGo on an API error |
| `git_*` | status / diff / log / commit / push through the **system `git`** |
| `ssh_*` | exec and key generation, keys taken from **`~/.ssh`** |

**A new project in one sentence.** Say: "Create repo `my-service` and publish it to Home / github /
work" — the agent calls `create_project` and does everything itself:

1. creates the project folder next to the current ones (or in `dir`, if given) and **a dashboard project** with that name;
2. `git init -b master`, `README.md`, `.gitignore` and the first commit `chore: initial commit`;
3. creates the repo on the hosting from the settings and adds `origin`;
4. pushes `master` and switches to work in the new project — the run ends with a handoff summary, the app opens the project and continues there.

**About access:** no separate logins are needed in the app — commits and pushes go through the normal
`git` on the machine (SSH keys, credential helper, `~/.ssh/config` already configured). A token is only
needed to **create** a repository via the API; if the hosting supports **"create on push"** (Gitea can),
no token is needed at all — the repo appears from the first `git push`.

**There can be several hostings.** Settings → Agent and MCP → "Git hostings" block: each entry has its
own **name** (e.g. `Home`, `work`, `github`), type (Gitea/GitHub), `baseUrl` (API and https clone
address), `sshBase` (`git@git.example.com:2222` or `ssh://***@host:2222`), default owner, "create on
push" and "default" flags. Then the command only needs the name: "publish to Home / github / work" — the
agent takes exactly that hosting. If no name is given, the one marked "default" is used.

If the hosting is unavailable or no token is set, the local part still remains: the folder, repository,
first commit and dashboard project are created, and the answer says exactly what failed ("no token",
"repository already exists", "push failed"); no work is lost because of the network.

Paths are counted from the project root and never go above it. If `read_file` gets a non-existent path,
it suggests similar files from the same folder.

### Project memory: map, index, facts, history

The agent remembers nothing between runs — it only sees what was put into the context. That's why four
levels of project knowledge were added to the prompt and the toolset (0.9.0):

| Level | What it gives | Cost |
|---|---|---|
| Project map in `<project_map>` | module, go packages, entry points, biggest files, docs | ~0.9 KB in the prompt |
| Symbol index (`find_symbol`, `project_search`) | "where is it declared" and "where is X handled" in one call | local index, cache by `size+mtime` |
| Fact memory (`remember`, `recall`) | found earlier: where things live, decisions, gotchas | ~1.2 KB in the prompt |
| Chat history (`history_search`) | "we discussed this" — from past chats of the project | 0 in the prompt, 1 call |

Memory is stored **outside the repository** (OS config dir, keyed by project path) and never goes into
git. If a file a note refers to has changed since the note was written, the note arrives with a "file
changed after the note, verify" flag — memory saves searching, but doesn't replace verification.

Details: formats, budgets, what's indexed, cache and memory paths, how to reset —
[docs/project-memory.md](docs/project-memory.md).

### MCP — external tools (code graph and memory)

MCP servers are connected in **Settings → Agent and MCP**: a preset installs the package, writes the
command and enables the recommended tools; then checkboxes choose what to hand to the model. Tool names
for the provider are `mcp__<server>__<tool>`, schemas sit in the prompt only for enabled ones.

- **Local (stdio)** — Codebase Memory, Cognee, Playwright, **Semgrep** (security review) and any
  `npx`/`uvx` servers: the preset installs the package and writes the command.
- **Hosted (Streamable HTTP)** — since 1.1.16: "Add URL server" → `https://host/mcp` → the **"Sign in"**
  button (OAuth: discovery, dynamic client registration, PKCE, local redirect) or a static token in
  `headers`. Tokens live in the system secret store, not in `settings.json`; `refresh_token` is refreshed
  automatically.
- Addresses are checked for SSRF: only `https`, `http` — only for localhost, internal networks — via the
  `allowPrivate` flag, redirects to a foreign host are forbidden.

- Enabled servers are **started at app launch** and **synchronously before a run** (20 s cap), so the
  graph is available from the first step, not the second.
- If the tools still aren't there (the server is starting or crashed), the chat gets a note "MCP server
  is still starting…", and start/stop/errors are written to `agent.log`
  (`mcp started id=… tools=…`, `mcp stopped id=…`, `mcp server id=… state=…`).
- Details and the preset table — [docs/mcp.md](docs/mcp.md), coding scenarios —
  [docs/mcp-coding-servers.md](docs/mcp-coding-servers.md).

## Chat UI

- **Plan / Act** in the header. Plan — the agent only reads and plans, without edits or shell commands.
- **Projects** — the **Project ▾** button in the left panel: **Open project…** opens an existing folder,
  **Create project…** creates a new project folder in the chosen place and opens it right away (the
  "Where to create" field is pre-filled from last time, "Browse…" picks a folder). The folder name must
  not contain path separators or the characters `< > : " | ? *`, and an existing folder can't be created
  again — only opened.
- **Clone repository…** clones a repository into the chosen folder and opens it as a project right away.
  The project list in the panel is always **alphabetical** (case-insensitive), not in the order added —
  a new project takes its place rather than jumping to the top.
  The access method is chosen automatically: if SSH is set up (key in `~/.ssh`, port and `IdentityFile`
  from `~/.ssh/config`) and the host responds — clone over SSH, otherwise over HTTPS. Links
  `ssh://***@host:port/path`, `git@host:path`, `https://host/path` and local paths/`file://` are
  supported. The folder gets the repository name; its path is visible in the dialog before pressing
  "Clone". If there's access neither via SSH nor HTTPS, the dialog shows an error with both attempts and
  a hint on what to check (key in the account / token for HTTPS and repo permissions). Cloning waits up
  to 5 minutes.
- **The `Menu ▾` menu** (left panel) gathered everything that used to hang as buttons: the **Project**
  section — `Open project…`, `Create project…`, `Clone repository…`, `New workspace…` and
  `Workspace from this project…` / `Edit workspace folders…`; the **File** section — `Archived chats`,
  `Hide files` / `Show files`, `Exit`; the **Settings** section — `Settings` / `Hide Settings` (toggles
  the settings panel, `Ctrl+Shift+S` still works) and `Model prices`; the **Help** section — `About`.
  There's no separate Settings button in the header.
- **Workspace — several projects in one chat (like Cursor).** A workspace is a separate entity: a name +
  a list of folders, without a "main" folder. In the left panel it stands as one row with its own icon
  and a caption from the folder names; chats, lock and memory belong to the workspace, not to one of the
  folders. Folders on disk are neither copied nor changed.

  How to create:
  1. `Menu ▾ → Project → New workspace…`.
  2. Enter a name (can be skipped — it becomes "api + web + docs").
  3. **"Add folder…"** as many times as needed (2–4 projects), or **drag folders into the window**
     right into the open dialog. The `↑` / `✕` buttons reorder and remove a folder.
  4. **"Create workspace"** — it opens, each folder at the top level in the file tree.

  Later: `Menu ▾ → Edit workspace folders…` or right-click the workspace → **"Workspace folders…"**.
  Drop a folder into the window with a workspace open — it's added right away. Drop into the window with
  a normal project — a new-workspace dialog opens with the project folder and the dropped ones.
  `Workspace from this project…` builds a workspace from the open project; the project and its chats stay
  as they were.

  How the agent works with it:

  | What | How it works across folders |
  |---|---|
  | Files, `grep`, `glob`, `find_files` | paths start with the folder name: `api/main.go`; search covers all folders, `path=api` narrows it |
  | `find_symbol`, `project_search`, project map | the index is built per folder, results are merged |
  | `run_terminal` | **`cwd` is required** — the folder name (`api` or `api/cmd`); all workspace folders count as "inside the project", no confirmation needed |
  | `git_status`, `git_diff` | without `repo` — across all folders, with a `## name` header; `repo=web` — one |
  | `git_log`, `git_commit`, `git_push` | the repo is derived from the path prefix or set via `repo`; one commit — one repo |
  | `read_lints` | `go vet` in each folder with `go.mod`; for a file — in its folder |
  | Memory (`remember` / `recall`) | one per workspace; note paths are expanded across folders |
  | Cursor rules | `.cursor/rules`, `.cursorrules`, `AGENTS.md` of **all** folders, plus global ones |

  Check: open a workspace and ask the agent "what's in `git status` per folder?" — the answer has `## api`,
  `## web` blocks. Old projects with an "anchor" and extra roots keep working as before.
- **Project lock** — 🔒 in the project row forbids sending messages and starting the agent in that
  project (so you don't accidentally write to the wrong place while another chat is open). For a locked
  project the chat header shows a "Project is locked: writing is not allowed" bar with an **"Unlock"**
  button, the input is disabled and `Send` is inactive.
- **Masking private data in chat.** You see your own message with `***` instead of passwords and logins
  (including inflected forms: "login …, password …", in `NAME=…` / `NAME: …`, `$env:NAME=…`, JSON, URL
  and Basic-auth, and in ALL-CAPS variables) — **the model still gets the original text**, the value keeps
  working, it just doesn't show up in the conversation.
- **Quote** — right-click selected text in a message inserts it as a quote (`> …`) into the input.
- **Authorship stamps.** Files the agent created or changed get a comment header
  `// Created by nc.ai 0.8.3.3 on MACHINE-NAME 2026-10-11 15:04:22` (`Edited by …` on edit); the comment
  style follows the file type (`//`, `#`, `--`, `;;`, `<!-- -->`), while JSON, binary and unknown formats
  are left alone. The line is added only if there's no stamp yet or the last entry was made by a
  **different** machine: edits from the same machine don't multiply lines.
- Clicking a file in the tree opens the editor instead of dumping its content into the chat; the file also
  lands in the agent's IDE context.
- After calling `todo_write` a task list hangs above the thread. **Right-click a ToDo block or a single
  item** opens a menu: "Remove item", "Remove completed", "Hide block in chat" (the agent keeps the plan)
  and "Remove ToDo entirely" (the list is cleared, the block hidden). Items removed manually don't come
  back on the next `todo_write` with merge — they return only with a new plan (merge=false).
- **Intermediate results in one block.** When the answer is ready, everything between your request and the
  final answer (reasoning, tool cards, service lines, drafts) folds under one triangle
  **"Intermediate results · N steps"** — expand with a click or "Expand all". Inside are the same blocks
  as in the transcript ("Explored …", "Thought" groups), but without extra nesting levels. While the agent
  works the steps are visible live; the final answer and your request are never folded.
- **Total and intermediate status.** After the answer the chat shows `⏱ Work took 1:23` (the agent knows
  the start and finish). While work takes more than a couple of steps, the app every 30 seconds reports in
  the status and transcript what's happening: `Work in progress: 1:23 · step 7/120 · Now: Reading app.go`.
- **A per-chat model.** The chat header has a selector `Auto (per settings)` / a specific provider model;
  the choice is stored in the session and survives a restart. DeepSeek escalation to `Pro` during a
  complex run is transient: after the answer the chat is back on its own model, and it doesn't leak into
  settings or neighbouring chats.
- **Chat tabs**: `+` creates a new session, double-click a tab to rename, old sessions never disappear.
- **The run and the transcript are tied to the chat.** An agent launch goes to the backend together with
  the sessionID: events, tools, the file tree and saved history belong to **that** chat and **that**
  project you sent the request from. The chat transcript is read and saved as a "session + items" pair
  from a single source (`LoadChatTranscript`), and all saves go through `persistSession(sessionId)` — so
  switching projects can't mix up chats: neither the answer nor the transcript drifts into another
  project. You can keep two or three projects running at once. If the chat isn't open or not found in the
  project, the launch fails with a clear error rather than a silent session substitution.
- **Activity indicators.** While the agent works, a small spinner spins in the project name and the chat
  name; when the work is done the spinner in the project disappears and a green check `✓` appears in the
  chat name. The project spinner is tied to the project's sessions, not to the open panel: it stays
  visible even after switching to another project (the run on the backend isn't interrupted), and the
  project row gets an accent frame. The app icon in the header animates only during a run; with no
  processes it's static (the frame animation doesn't spin idle).
- **Sound at the end of work** (on by default, a checkbox in Settings → Agent): a short signal plays once
  when **all** work is done — all chats are free — not after every step.
- **Archive**: a button in the chat header moves the session to
  `%APPDATA%/NotCursor/chat_archive/<name>.json`. The archive list opens from the UI, an archived chat is
  read-only and isn't mixed into active tabs.
- **Chat search** — **Ctrl+F**: a panel with a counter, `↑` / `↓` (and `F3`), exit with `Esc` or a second
  **Ctrl+F**. Only visible text counts: the content of collapsed Thinking / tool blocks isn't counted.
- Money: the header shows `chat $0.0043 · total $0.1231 · cache 42%` (Z.ai and OpenRouter also show a
  balance nearby). Hovering over this block expands the breakdown: in/out tokens per chat and total, plus
  chat and total cache as `… hit / … miss (N%)`. DeepSeek peak / off-peak is accounted for, the price is
  fetched automatically.
- **DeepSeek balance** is split into three parts: `bal $9.85 · grant $0.00 · topup $9.85`. It's re-requested
  every 5 minutes and right after every run, and when less than **$1** of top-up is left the `topup` figure
  turns **red** ("work will stop soon") and a warning appears in the chat before a run starts. Every check
  is written to `agent.log` (`balance total=… grant=… topup=… available=…`), so a low balance is visible
  after a failure too. Running out of account is reported separately by the provider: `402` →
  "Insufficient DeepSeek balance. Top up and press Reconnect" (this is not the same as "Connection to LLM
  lost" — that error is about the network).
- Prices are checked automatically: DeepSeek and OpenRouter — once a day after 13:05 Beijing, z.ai — once a
  week. The result arrives in the chat as a system message.
- Chat history is stored per project in `%APPDATA%/NotCursor/chats/` (on macOS —
  `~/Library/Application Support/NotCursor/`).

## Cursor rules

Rules are collected and mixed into the system prompt:

- globally: `~/.cursor/rules/*.mdc|md` (recursive), `~/.cursorrules`, `AGENTS.md`;
- in the project: `<project>/.cursor/rules/*` (recursive), `<project>/.cursorrules`.

**Settings → Cursor Rules** shows what was loaded, and there's a **Reload** button.

**Which files are read.** The rule directories are `~/.cursor/rules` (global) and
`<project>/.cursor/rules` (project). Both extensions are read, `.mdc` and `.md`, plus the legacy files
`~/.cursorrules`, `<project>/.cursorrules` and `<project>/AGENTS.md`. The canonical format for the rule
directory is **`.mdc`** (it has the `description` / `globs` / `alwaysApply` frontmatter that decides
whether a rule always applies, applies by path match, or stays in the catalog); `.md` is read too, but
frontmatter semantics don't make sense for it — such files end up in "always applied". Don't keep `.md`
and `.mdc` with the same name side by side: they're duplicates, they arrive twice and clutter the breakdown.

**What actually goes to the model.** The header has a `Rules Applied/Total` pill — it's not "how many
files were found" but how many rules actually make it into the prompt: the full rule body
(`alwaysApply: true`, `AGENTS.md`/`.cursorrules`, files without frontmatter, `globs: *`) plus those that
matched the current request's `globs` (hints come from paths in your message and attachments). **Clicking
the pill** opens a breakdown into three groups: *always applied*, *attached to this request* and *catalog
only*. In the catalog a rule sits as a `name — description` line, and the body is attached later — when a
path matching its `globs` appears in the request (e.g. `**/*.cs` for `csharp-spec`) or when the agent asks
for the rule itself. The reason is shown for each rule. Cursor/IDE-nature rules (`00-always-read-rules`,
`assistant-name-umnik`, `propose-project-ops`, `remotes-mirror-and-sanitize`, etc.) are never thrown away —
if they have `alwaysApply: true`, they apply like normal rules.

**Always-rule hygiene.** The body of an `alwaysApply: true` rule goes to the model on **every** turn, so
keep only what's needed always in always (answer style, prohibitions, language). Everything else — via
`globs` (the rule is attached when a file of that type appears in the request) or via `description` in the
catalog: the agent calls the rule itself when needed. A long "how to review C#" checklist in always costs
tokens on every turn and blurs the main rule; in the catalog it costs zero. A sign of an over-inflated
always is a `Rules Applied/Total` pill almost equal to `Total` while the request has no file of that type.

## Terminal

At the bottom there's a **terminal panel** (opens with **Ctrl+Shift+T**; app settings, including the shell
choice, are in a separate window with **Ctrl+Shift+S**). Inside the panel live **two different command
modes**, and mixing them up is not worth it — the mode decides what works at all.

**1. Interactive session — like a normal system terminal.** This is a real PTY: ConPTY on Windows, pty on
macOS and Linux. The shell comes from **Settings → Shell** (empty — auto: PowerShell 7 or 5 on Windows,
`$SHELL` on macOS/Linux), the start folder is the active project root. You can type right into the output
area: history, `cd`, Ctrl+C, vim and top, ssh with a password prompt, progress bars all work.
`TERM=xterm-256color`, `COLORTERM=truecolor`, the window size follows the panel. While the session is alive
its state (current folder, variables, activated venv) persists between commands. When the process exits,
`[shell exited]` appears in the output.

**2. One-shot — "one command, one process".** The input field next to the **Run** button (placeholder
`oneshot: date`) runs one command as a separate process: PowerShell `-Command`, POSIX-shell `-lc`, cmd
`/C`. The working folder is the project root, stdin is closed, output goes through pipes, timeout 60
seconds. The result is appended to the same xterm with `> command` … `[exit N]` lines, the field is cleared.

The difference in practice:

| | Interactive session | One-shot (Run) |
|---|---|---|
| State (`cd`, variables, venv) between commands | persists | **not** kept — each Run = a new process |
| Interactive (vim, ssh password, `git rebase -i`) | works | **doesn't** work |
| Shell profile (PATH, aliases, venv) | loaded | **not** loaded |
| Long processes (dev server, watcher) | not for this | not for this — only the agent's `run_terminal` with `is_background` |

Hence the main practical snag: if a command works in the interactive session but fails through **Run** with
`[exit 1]` and empty output — it lacked the `PATH` / aliases / venv from your profile (one-shot runs
without a profile). Use the full path to the binary or run the command in the interactive session.

## Security

- Secrets don't go to the model: `read_file` on `.env*`, `id_rsa`, `*.pem`, `*.key`, `*secret*` returns a
  stub, `KEY=…` / `TOKEN=…` values are masked as `***`.
- SSH: verified against `~/.ssh/known_hosts`, TOFU on first contact. If the host key changed, the
  connection is rejected.
- `git_commit` stages only changes to already tracked files (`git add -u`), new files don't slip in by
  accident.
- Chat and settings JSON is written atomically (temp + rename) with a rotating `.bak` copy.
- **Project boundaries.** The agent works only inside the open project folder. If a `run_terminal` command
  names a path in another known project, goes `../` outside the working folder, touches the home directory
  (`~`, `$env:USERPROFILE`) or an arbitrary path outside the project, the app asks for confirmation. What
  exactly is affected is written in the dialog ("the command enters another project: `D:\repos\other`",
  "the command changes data outside the project: `…\hosts`"), and on refusal the agent is told to work
  within the current project. This closes the "the agent climbed into a neighbouring repo and did
  something" scenario: without explicit user permission such commands aren't executed. File tools
  (`write_file`, `apply_patch`, `delete_file`, etc.) already failed closed when leaving the sandbox.
- **Reading is allowed, changing is not.** Outside the project, read-only commands (`Get-Content`,
  `Select-String`, `git log`, `dir`) don't ask for permission if the "Confirm dangerous tools" checkbox is
  off — otherwise normal reconnaissance in a neighbouring repo would need a click on every step. Any
  **change** outside the project (`Set-Content`, `Copy-Item`, `Remove-Item`, `robocopy`, `icacls`, `reg add`,
  `schtasks`, `pip install`, `winget install`, `npm ci`, `git pull`, `>` redirection to a file) is
  confirmed **always**, in both checkbox modes. Inside the project the logic is the same: reading silently,
  changes by the checkbox.
- **"Allow and don't ask".** In the confirmation dialog the answer can be remembered for a specific tool
  (across all chats) forever: the list lives in `settings.json` and survives a restart — then `run_terminal`
  no longer re-asks every step. Re-enabling "Confirm dangerous tools" clears all such answers.
- **The confirmation dialog isn't lost when working in another project.** The request goes with its own
  `sessionID` and is labeled: "Project: Rules · chat: …" — you can see who's asking even if another
  project is open now, and the answer always lands in its own run. If there's no answer for 5 minutes, the
  step finishes with an honest "confirmation not received in 5 min — `run_terminal` was not run" (it used
  to wait until the watchdog and report "tool hung"). Waiting for confirmation is a separate phase in the
  activity indicator: `waiting for confirmation`, not "running a tool".

## Screenshots

The UI reads an image from a file or the clipboard and sends it as `data:image/png;base64,…`; the backend
assembles a user message with `image_url`, then the request goes to the vision model and the normal tool
loop.

## Settings

Opens from **`Menu ▾ → Settings`** (or `Ctrl+Shift+S`), closes with the same item **`Hide Settings`** —
there's no separate header button.

- **Provider** — DeepSeek / Z.ai / OpenRouter / Qwen (DashScope) / Yandex AI Studio / Local (several LAN
  servers): key, model, base URL / folder id. For Custom: Lite mode, Auto-models, a speed indicator next
  to Rules — see [docs/local-models-ru.md](docs/local-models-ru.md).
- **Agent** — steps limit, Plan mode, dangerous-tool confirmation, "Sound at the end of work".
- **Network** — protocol to providers (Auto / HTTP1.1), HTTP/2 keepalive, retry count and retry-series
  budget, a connection-drop counter with the last error and a reset button.
- **Interface** — theme (Dark / Light), UI font and monospace font, "Show terminal" (`Ctrl-Shift-T`).
- **Updates** — installed version and build hash, a "Check for updates" button and the **update source**:
  `GitHub` (default) or **your own mirror** (e.g. a home Gitea — the check and download go there first, and
  if it's unavailable they quietly fall back to GitHub). The mirror address and an optional token are stored
  only on this machine.
- **Shell** — the shell path for the interactive terminal and an "Auto (detect)" button.
- **Cursor Rules** — the list of loaded rules and reload.
- **Git & SSH** — no logins in the UI, the system `git` and `~/.ssh` are used.

App data: `%APPDATA%\NotCursor` (Windows), `~/Library/Application Support/NotCursor` (macOS).

## Commit and push

```powershell
.\commit.ps1 "feat: description of changes"
```

```bash
./commit.sh "feat: description of changes"
```

Both scripts do `git add -A` and `git push` to the remote configured for the branch. Convenient when the
tree is clean, unpleasant when something extra is lying around.

## Releases

A GitHub release is a `vX.Y.Z` tag and one file per platform:

| Platform | File |
|---|---|
| Windows x64 | `nc.ai-X.Y.Z-windows-amd64.zip` |
| macOS arm64 | `nc.ai-X.Y.Z-macos-arm64.zip` |
| macOS universal | `nc.ai-X.Y.Z-macos-universal.zip` |

Next to **each** archive we put a `<archive name>.sha256` signature — the sha256 of the main executable
**inside** the archive (in the zip that's `NotCursor.exe`, in the macOS archive
`NotCursor.app/Contents/MacOS/NotCursor`). For macOS archives a script from the repo does it:

```bash
./scripts/macos-zip-sidecar.sh nc.ai-X.Y.Z-macos-arm64.zip nc.ai-X.Y.Z-macos-universal.zip
# verify the signature against the already installed build:
./scripts/macos-zip-sidecar.sh --check nc.ai-X.Y.Z-macos-arm64.zip /Applications/NotCursor.app/Contents/MacOS/NotCursor
```

The Windows signature is computed the same way — from `NotCursor.exe` **extracted from the archive**:

```powershell
Expand-Archive nc.ai-X.Y.Z-windows-amd64.zip -DestinationPath .\tmp
(Get-FileHash .\tmp\NotCursor.exe -Algorithm SHA256).Hash.ToLower() + "  nc.ai-X.Y.Z-windows-amd64.zip" |
  Out-File -Encoding ascii nc.ai-X.Y.Z-windows-amd64.zip.sha256
```

Why: on start the app compares the hash of its build with the signature from the latest release. So you see
not only "a newer version is out" but also a **re-upload of a file under the same version number** — by the
numbers alone such a replacement would go unnoticed. If there's no signature, the check works as before —
by version number.

## Structure

```
app.go                 # Wails façade / bindings
main.go                # window: size, position, maximised
frontend/src/          # React UI: chat, tabs, attachments, themes
internal/agent/        # agent loop, retry, compaction, plan
internal/llm/          # types, shared OpenAI-compat layer, providers/{deepseek,zai,openrouter,qwen,local,anthropic}
internal/tools/        # registry + executor
internal/netx/         # SSRF-safe HTTP
internal/redact/       # masking secrets in tool output
internal/update/       # checking GitHub releases and installing an update
internal/chime/        # sound at the end of work
internal/appmeta/      # version and build name
internal/workspace/    # projects, FS sandbox, secret guard
internal/chatstore/    # saving sessions and archives to disk
internal/rules/        # Cursor rules loader
internal/shell/        # oneshot run and interactive PTY
internal/gitx/         # system git: status / diff / commit / push
internal/sshx/         # SSH via ~/.ssh and known_hosts
internal/secrets/      # Keychain / Credential Manager / 0600 file
internal/config/       # settings: window, layout, theme, providers
internal/costing/      # tokens → USD, peak / off-peak
internal/fsx/          # atomic file writes
cmd/                   # utilities: checkconnect, migratechat
scripts/               # release sidecar signatures, brand asset build
docs/                  # spec, architecture, protocols
```

## Documentation

- [Spec (TZ)](docs/TZ.md)
- [Build, install and release](docs/build-and-release.md)
- [Roadmap](docs/roadmap.md) — subagents and local-server context plans
- [Architecture](docs/architecture/overview.md)
- [Agent memory: map, symbol index, facts, history](docs/project-memory.md)
- [AI protocols](docs/exchange-protocols/README.md)

## What's next

- diff viewer + LSP hints (edits are currently shown as an `apply_patch` card);
- split `frontend/src/App.tsx` into hooks;
- provider and model **per chat**: currently the provider is chosen globally (Settings → Provider, a single
  `a.llm` field in `app.go`), but we want DeepSeek in one window and Ollama (`local:…`) in another.
  `agent.Runner` already takes the provider as a parameter, and `chatstore.Session` already stores spend per
  provider — we need `ProviderID`/`Model` in the session, a provider factory per run and a per-chat selector
  in the UI. Model auto-routing must work inside the chat's provider.
