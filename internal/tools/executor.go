package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"notcursor.ai/app/internal/gitx"
	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/netx"
	"notcursor.ai/app/internal/shell"
	"notcursor.ai/app/internal/sshx"
	"notcursor.ai/app/internal/workspace"
)

// Registry executes tool calls against workspace services.
type Registry struct {
	WS       *workspace.Manager
	SSHDir   string
	Git      *gitx.Service
	SSH      *sshx.Service
	Timeout  time.Duration
	Shell    string // optional; empty = auto-detect
	PlanMode bool
	Jobs     *JobStore
	Todos    *TodoStore
	// AskUser, when set, blocks until the user answers ask_user.
	AskUser func(ctx context.Context, callID, question string, options []string) (string, error)
}

func NewRegistry(ws *workspace.Manager, sshDir string) *Registry {
	return &Registry{
		WS:      ws,
		SSHDir:  sshDir,
		Git:     gitx.New(ws),
		SSH:     sshx.New(sshDir),
		Timeout: 90 * time.Second,
		Jobs:    NewJobStore(),
		Todos:   NewTodoStore(),
	}
}

// DangerousTool reports tools that may mutate the system or leave the machine
// when ToolConfirm is enabled.
func DangerousTool(name string) bool {
	switch name {
	case "write_file", "apply_patch", "delete_file", "run_terminal", "git_commit", "git_push", "ssh_exec":
		return true
	default:
		return false
	}
}

// ReadOnlyTool reports tools that can run in parallel with each other.
func ReadOnlyTool(name string) bool {
	switch name {
	case "read_file", "list_dir", "find_files", "grep", "search_files", "get_env_info",
		"git_status", "git_diff", "command_status", "web_search", "fetch_url", "read_lints":
		return true
	default:
		return false
	}
}

func PlanBlocked(name string) bool {
	switch name {
	case "write_file", "apply_patch", "delete_file", "run_terminal",
		"git_commit", "git_push", "ssh_exec", "ssh_keygen":
		return true
	default:
		return false
	}
}

func (r *Registry) Execute(ctx context.Context, call llm.ToolCall) (string, error) {
	name := call.Function.Name
	args := map[string]any{}
	raw := strings.TrimSpace(call.Function.Arguments)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", fmt.Errorf("invalid tool arguments JSON: %w", err)
		}
	}
	if r.PlanMode && PlanBlocked(name) {
		return "", fmt.Errorf("plan mode: %s is disabled — explore, then present a plan; wait for Act", name)
	}

	switch name {
	case "read_file":
		path, _ := args["path"].(string)
		start := intArg(args, "start_line")
		end := intArg(args, "end_line")
		return r.WS.ReadFileRange(path, start, end)
	case "write_file":
		if r.PlanMode {
			return "", fmt.Errorf("plan mode: write_file is disabled")
		}
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		if err := r.WS.WriteFile(path, content); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %s (%d bytes)", path, len(content)), nil
	case "delete_file":
		if r.PlanMode {
			return "", fmt.Errorf("plan mode: delete_file is disabled")
		}
		path, _ := args["path"].(string)
		if strings.TrimSpace(path) == "" {
			return "", fmt.Errorf("path is empty")
		}
		if err := r.WS.DeletePath(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Sprintf("already absent: %s", path), nil
			}
			return "", err
		}
		return fmt.Sprintf("deleted %s", path), nil
	case "apply_patch":
		if r.PlanMode {
			return "", fmt.Errorf("plan mode: apply_patch is disabled")
		}
		path, _ := args["path"].(string)
		oldStr, _ := args["old_string"].(string)
		newStr, _ := args["new_string"].(string)
		replaceAll, _ := args["replace_all"].(bool)
		if patch, _ := args["patch"].(string); strings.TrimSpace(patch) != "" {
			var err error
			oldStr, newStr, err = ParseSearchReplacePatch(patch)
			if err != nil {
				return "", err
			}
		}
		raw, err := r.WS.ReadFileRaw(path)
		if err != nil {
			return "", err
		}
		next, n, err := ApplySearchReplace(raw, oldStr, newStr, replaceAll)
		if err != nil {
			return "", err
		}
		if err := r.WS.WriteFile(path, next); err != nil {
			return "", err
		}
		return fmt.Sprintf("patched %s (%d replacement(s), %d → %d bytes)", path, n, len(raw), len(next)), nil
	case "list_dir":
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		entries, err := r.WS.ListDir(path)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(entries, "", "  ")
		return string(b), nil
	case "find_files":
		query, _ := args["query"].(string)
		hits, err := r.WS.FindFiles(query, 40)
		if err != nil {
			return "", err
		}
		if len(hits) == 0 {
			return "(no files matched)", nil
		}
		return strings.Join(hits, "\n"), nil
	case "grep":
		query, _ := args["query"].(string)
		glob, _ := args["path_glob"].(string)
		caseSens, _ := args["case_sensitive"].(bool)
		multiline, _ := args["multiline"].(bool)
		mode, _ := args["output_mode"].(string)
		hits, err := r.WS.Grep(workspace.GrepOptions{
			Query:         query,
			PathGlob:      glob,
			Context:       intArg(args, "context"),
			Before:        intArg(args, "before"),
			After:         intArg(args, "after"),
			CaseSensitive: caseSens,
			Multiline:     multiline,
			OutputMode:    mode,
			Limit:         50,
		})
		if err != nil {
			return "", err
		}
		out := workspace.FormatGrepHitsMode(hits, mode)
		if len(hits) >= 50 {
			out += "\n(truncated; at least 50 matches — narrow path_glob or query)"
		}
		return out, nil
	case "search_files":
		// Legacy combined search: filename hits first, then content grep.
		query, _ := args["query"].(string)
		var parts []string
		if names, err := r.WS.FindFiles(query, 20); err == nil && len(names) > 0 {
			parts = append(parts, "## filenames\n"+strings.Join(names, "\n"))
		}
		if hits, err := r.WS.Grep(workspace.GrepOptions{Query: query, Context: 0, Limit: 20}); err == nil && len(hits) > 0 {
			parts = append(parts, "## content\n"+workspace.FormatGrepHits(hits))
		}
		if len(parts) == 0 {
			return "(no matches)", nil
		}
		return strings.Join(parts, "\n\n"), nil
	case "get_env_info":
		return r.envInfo(), nil
	case "run_terminal":
		if r.PlanMode {
			return "", fmt.Errorf("plan mode: run_terminal is disabled")
		}
		cmd, _ := args["command"].(string)
		if strings.ContainsAny(cmd, "\r\n") {
			return "", fmt.Errorf("command must be a single line (no newlines)")
		}
		relCwd, _ := args["cwd"].(string)
		cwd, err := r.WS.ActiveRoot()
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(relCwd) != "" && relCwd != "." {
			full, err := r.WS.Resolve(relCwd)
			if err != nil {
				return "", fmt.Errorf("cwd: %w", err)
			}
			st, err := os.Stat(full)
			if err != nil || !st.IsDir() {
				return "", fmt.Errorf("cwd is not a directory: %s", relCwd)
			}
			cwd = full
		}
		timeout := r.Timeout
		if sec := intArg(args, "timeout_sec"); sec > 0 {
			timeout = time.Duration(sec) * time.Second
		}
		bg, _ := args["is_background"].(bool)
		if bg {
			if r.Jobs == nil {
				r.Jobs = NewJobStore()
			}
			id, err := r.Jobs.Start(cmd, cwd, r.Shell, timeout)
			if err != nil {
				return "", err
			}
			rel := relCwd
			if rel == "" {
				rel = "."
			}
			return fmt.Sprintf("started background job %s in %s\nUse command_status with this id.", id, filepath.ToSlash(rel)), nil
		}
		res, err := shell.Run(ctx, cmd, cwd, r.Shell, timeout)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("exit=%d\nstdout:\n%s\nstderr:\n%s", res.ExitCode, res.Stdout, res.Stderr), nil
	case "command_status":
		id, _ := args["id"].(string)
		prio, _ := args["output_priority"].(string)
		if r.Jobs == nil {
			return "", fmt.Errorf("no background jobs")
		}
		return r.Jobs.Status(id, intArg(args, "wait_sec"), intArg(args, "char_count"), prio)
	case "todo_write":
		merge, _ := args["merge"].(bool)
		rawTodos := args["todos"]
		b, _ := json.Marshal(rawTodos)
		var incoming []Todo
		if err := json.Unmarshal(b, &incoming); err != nil {
			return "", fmt.Errorf("todos: %w", err)
		}
		if r.Todos == nil {
			r.Todos = NewTodoStore()
		}
		items, err := r.Todos.Apply(merge, incoming)
		if err != nil {
			return "", err
		}
		return formatTodos(items), nil
	case "ask_user":
		q, _ := args["question"].(string)
		if strings.TrimSpace(q) == "" {
			return "", fmt.Errorf("question is empty")
		}
		var opts []string
		if raw, ok := args["options"].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					opts = append(opts, s)
				}
			}
		}
		if r.AskUser == nil {
			return "", fmt.Errorf("ask_user is not available")
		}
		return r.AskUser(ctx, call.ID, q, opts)
	case "read_lints":
		var paths []string
		if raw, ok := args["paths"].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					paths = append(paths, s)
				}
			}
		}
		if p, _ := args["path"].(string); strings.TrimSpace(p) != "" {
			paths = append(paths, p)
		}
		return r.readLints(paths)
	case "web_search":
		q, _ := args["query"].(string)
		return netx.SearchDuckDuckGo(q)
	case "fetch_url":
		u, _ := args["url"].(string)
		return netx.FetchURL(u)
	case "git_status":
		return r.Git.Status()
	case "git_diff":
		path, _ := args["path"].(string)
		staged, _ := args["staged"].(bool)
		return r.Git.Diff(path, staged)
	case "git_commit":
		msg, _ := args["message"].(string)
		return r.Git.Commit(msg)
	case "git_push":
		remote, _ := args["remote"].(string)
		branch, _ := args["branch"].(string)
		return r.Git.Push(remote, branch)
	case "ssh_exec":
		host, _ := args["host"].(string)
		command, _ := args["command"].(string)
		user, _ := args["user"].(string)
		port := 22
		if p, ok := args["port"].(float64); ok {
			port = int(p)
		}
		keyName, _ := args["key_name"].(string)
		password, _ := args["password"].(string)
		return r.SSH.Exec(host, user, port, keyName, password, command, r.Timeout)
	case "ssh_keygen":
		nameKey, _ := args["name"].(string)
		pub, err := r.SSH.Keygen(nameKey)
		if err != nil {
			return "", err
		}
		return "public key:\n" + pub, nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func (r *Registry) envInfo() string {
	root, _ := r.WS.ActiveRoot()
	sh := shell.ResolveShell(r.Shell)
	var b strings.Builder
	fmt.Fprintf(&b, "os=%s\n", runtime.GOOS)
	fmt.Fprintf(&b, "arch=%s\n", runtime.GOARCH)
	fmt.Fprintf(&b, "shell=%s\n", sh)
	if root != "" {
		fmt.Fprintf(&b, "workspace=%s\n", root)
	}
	if v := toolVersion("go", "version"); v != "" {
		fmt.Fprintf(&b, "go=%s\n", v)
	}
	if v := toolVersion("node", "-v"); v != "" {
		fmt.Fprintf(&b, "node=%s\n", v)
	}
	if v := toolVersion("npm", "-v"); v != "" {
		fmt.Fprintf(&b, "npm=%s\n", v)
	}
	if v := toolVersion("python", "--version"); v != "" {
		fmt.Fprintf(&b, "python=%s\n", v)
	} else if v := toolVersion("python3", "--version"); v != "" {
		fmt.Fprintf(&b, "python=%s\n", v)
	}
	if v := os.Getenv("TERM"); v != "" {
		fmt.Fprintf(&b, "term=%s\n", v)
	}
	return strings.TrimSpace(b.String())
}

func toolVersion(bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Specs returns OpenAI-shaped tool definitions. Plan mode hides mutating tools.
func Specs() []llm.ToolSpec {
	return SpecsFor(false)
}

func SpecsFor(plan bool) []llm.ToolSpec {
	all := []llm.ToolSpec{
		fn("read_file", "Read a workspace file. Optional start_line/end_line (1-based) to read only a slice — prefer ranges for large files.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string", "description": "Existing relative path, e.g. internal/config/store.go"},
				"start_line": map[string]any{"type": "integer", "description": "Optional 1-based start line"},
				"end_line":   map[string]any{"type": "integer", "description": "Optional 1-based end line (inclusive)"},
			},
			"required": []string{"path"},
		}),
		fn("write_file", "Create or overwrite a whole workspace file. Prefer apply_patch for small edits to save output tokens.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string"},
				"content":     map[string]any{"type": "string"},
				"explanation": map[string]any{"type": "string", "description": "Short why, shown in the approval dialog"},
			},
			"required": []string{"path", "content"},
		}),
		fn("apply_patch", "Apply a precise edit to an existing file. Prefer this over write_file when changing part of a file. Provide old_string+new_string (exact match) OR a patch block with <<<<<<< SEARCH / ======= / >>>>>>> REPLACE.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string", "description": "Existing relative path"},
				"old_string":  map[string]any{"type": "string", "description": "Exact text to find (include enough context to be unique)"},
				"new_string":  map[string]any{"type": "string", "description": "Replacement text (may be empty to delete)"},
				"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence (default false = exactly one match required)"},
				"patch":       map[string]any{"type": "string", "description": "Optional SEARCH/REPLACE block instead of old_string/new_string"},
			},
			"required": []string{"path"},
		}),
		fn("delete_file", "Delete a workspace file or empty directory. Fails closed on path escape. Prefer this over rm in the shell.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string"},
				"explanation": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		}),
		fn("list_dir", "List directory entries", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
		}),
		fn("find_files", "Fuzzy/filename search for paths (by name). Use before read_file when you know part of the filename.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Substring or fuzzy name, e.g. store.go or auth"},
			},
			"required": []string{"query"},
		}),
		fn("grep", "Ripgrep-like content search. query is a regex (escape special chars). Prefer this over shell rg/grep.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":          map[string]any{"type": "string", "description": "Regex, e.g. func\\s+Routes or foo\\.bar\\("},
				"path_glob":      map[string]any{"type": "string", "description": "Optional glob against relative paths, e.g. **/*.go"},
				"context":        map[string]any{"type": "integer", "description": "Lines before/after (like rg -C, 0-10)"},
				"before":         map[string]any{"type": "integer", "description": "Lines before match (rg -B)"},
				"after":          map[string]any{"type": "integer", "description": "Lines after match (rg -A)"},
				"output_mode":    map[string]any{"type": "string", "description": "content (default) | files_with_matches | count"},
				"case_sensitive": map[string]any{"type": "boolean"},
				"multiline":      map[string]any{"type": "boolean", "description": "Let . match newlines"},
			},
			"required": []string{"query"},
		}),
		fn("search_files", "Legacy combined filename+content search. Prefer find_files or grep for precision.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		}),
		fn("get_env_info", "OS, arch, shell, workspace root, and available toolchain versions (go/node/python).", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}),
		fn("run_terminal", "Run a command in the project shell. Pass cwd instead of cd. Non-interactive only. Long servers: is_background=true then command_status.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":       map[string]any{"type": "string", "description": "Single line, no newlines; add -y/--yes; no pagers"},
				"cwd":           map[string]any{"type": "string", "description": "Relative directory under the workspace root"},
				"timeout_sec":   map[string]any{"type": "integer"},
				"is_background": map[string]any{"type": "boolean"},
				"explanation":   map[string]any{"type": "string", "description": "Short why, shown in the approval dialog"},
			},
			"required": []string{"command"},
		}),
		fn("command_status", "Poll a background run_terminal job by id.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":              map[string]any{"type": "string"},
				"wait_sec":        map[string]any{"type": "integer", "description": "Wait up to this many seconds (0-60) for completion"},
				"char_count":      map[string]any{"type": "integer", "description": "Max output characters (default 4000)"},
				"output_priority": map[string]any{"type": "string", "description": "top | bottom | split"},
			},
			"required": []string{"id"},
		}),
		fn("todo_write", "Create or update the session task list. merge=true updates by id; merge=false replaces the list. Do not narrate todo updates to the user.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"merge": map[string]any{"type": "boolean"},
				"todos": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":      map[string]any{"type": "string"},
							"content": map[string]any{"type": "string"},
							"status":  map[string]any{"type": "string", "description": "pending | in_progress | completed | cancelled"},
						},
						"required": []string{"id", "content", "status"},
					},
				},
			},
			"required": []string{"todos"},
		}),
		fn("ask_user", "Ask the user a blocking question when a real decision is required. Prefer options. Do not use for facts you can look up in the repo.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string"},
				"options": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"required": []string{"question"},
		}),
		fn("read_lints", "Diagnostics for files you just edited. Go: go vet on the package. Call only on files you changed.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"path":  map[string]any{"type": "string"},
			},
		}),
		fn("web_search", "Search the public web for current docs, APIs, or facts not in the repo. Results are snippets + URLs.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		}),
		fn("fetch_url", "Fetch a public http(s) URL as text. Private/loopback/metadata addresses are blocked.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string"},
			},
			"required": []string{"url"},
		}),
		fn("git_status", "Git status", map[string]any{"type": "object", "properties": map[string]any{}}),
		fn("git_diff", "Git diff", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string"},
				"staged": map[string]any{"type": "boolean"},
			},
		}),
		fn("git_commit", "Stage tracked changes and commit. Only when the user explicitly asked to commit.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message":     map[string]any{"type": "string"},
				"explanation": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		}),
		fn("git_push", "Push to remote using OS git (credential helper / SSH keys in ~/.ssh — no in-app passwords)", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"remote": map[string]any{"type": "string"},
				"branch": map[string]any{"type": "string"},
			},
		}),
		fn("ssh_exec", "Run command on remote host via SSH (keys from ~/.ssh)", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host":     map[string]any{"type": "string"},
				"command":  map[string]any{"type": "string"},
				"user":     map[string]any{"type": "string"},
				"port":     map[string]any{"type": "integer"},
				"key_name": map[string]any{"type": "string", "description": "Key filename under ~/.ssh, e.g. id_ed25519"},
				"password": map[string]any{"type": "string"},
			},
			"required": []string{"host", "command"},
		}),
		fn("ssh_keygen", "Generate ed25519 SSH key pair in ~/.ssh", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required": []string{"name"},
		}),
	}
	if !plan {
		return all
	}
	out := make([]llm.ToolSpec, 0, len(all))
	for _, s := range all {
		if PlanBlocked(s.Function.Name) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func fn(name, desc string, params map[string]any) llm.ToolSpec {
	return llm.ToolSpec{
		Type: "function",
		Function: llm.ToolSpecFunction{
			Name:        name,
			Description: desc,
			Parameters:  params,
		},
	}
}
