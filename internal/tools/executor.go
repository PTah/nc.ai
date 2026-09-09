package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"notcursor.ai/app/internal/gitx"
	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/shell"
	"notcursor.ai/app/internal/sshx"
	"notcursor.ai/app/internal/workspace"
)

// Registry executes tool calls against workspace services.
type Registry struct {
	WS      *workspace.Manager
	SSHDir  string
	Git     *gitx.Service
	SSH     *sshx.Service
	Timeout time.Duration
	Shell   string // optional; empty = auto-detect
}

func NewRegistry(ws *workspace.Manager, sshDir string) *Registry {
	return &Registry{
		WS:      ws,
		SSHDir:  sshDir,
		Git:     gitx.New(ws),
		SSH:     sshx.New(sshDir),
		Timeout: 90 * time.Second,
	}
}

// DangerousTool reports tools that may mutate the system or leave the machine
// when ToolConfirm is enabled.
func DangerousTool(name string) bool {
	switch name {
	case "write_file", "run_terminal", "git_push", "ssh_exec":
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

	switch name {
	case "read_file":
		path, _ := args["path"].(string)
		start := intArg(args, "start_line")
		end := intArg(args, "end_line")
		return r.WS.ReadFileRange(path, start, end)
	case "write_file":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		if err := r.WS.WriteFile(path, content); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %s (%d bytes)", path, len(content)), nil
	case "apply_patch":
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
		hits, err := r.WS.Grep(workspace.GrepOptions{
			Query:         query,
			PathGlob:      glob,
			Context:       intArg(args, "context"),
			CaseSensitive: caseSens,
			Limit:         50,
		})
		if err != nil {
			return "", err
		}
		return workspace.FormatGrepHits(hits), nil
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
		cmd, _ := args["command"].(string)
		root, err := r.WS.ActiveRoot()
		if err != nil {
			return "", err
		}
		res, err := shell.Run(ctx, cmd, root, r.Shell, r.Timeout)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("exit=%d\nstdout:\n%s\nstderr:\n%s", res.ExitCode, res.Stdout, res.Stderr), nil
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

// Specs returns OpenAI-shaped tool definitions.
func Specs() []llm.ToolSpec {
	return []llm.ToolSpec{
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
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
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
		fn("grep", "Search file contents (literal substring). Optional path_glob (e.g. **/*.go) and context lines (0-5 like ripgrep -C).", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":          map[string]any{"type": "string"},
				"path_glob":      map[string]any{"type": "string", "description": "Optional glob against relative paths"},
				"context":        map[string]any{"type": "integer", "description": "Lines of context before/after (0-5)"},
				"case_sensitive": map[string]any{"type": "boolean"},
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
		fn("run_terminal", "Run a command in the configured project shell (PowerShell on Windows, login shell on macOS/Linux)", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		}),
		fn("git_status", "Git status", map[string]any{"type": "object", "properties": map[string]any{}}),
		fn("git_diff", "Git diff", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string"},
				"staged": map[string]any{"type": "boolean"},
			},
		}),
		fn("git_commit", "Stage all and commit", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
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
