package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
		return r.WS.ReadFile(path)
	case "write_file":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		if err := r.WS.WriteFile(path, content); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %s (%d bytes)", path, len(content)), nil
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
	case "search_files":
		query, _ := args["query"].(string)
		hits, err := r.WS.SearchFiles(query, 40)
		if err != nil {
			return "", err
		}
		return strings.Join(hits, "\n"), nil
	case "run_terminal":
		cmd, _ := args["command"].(string)
		root, err := r.WS.ActiveRoot()
		if err != nil {
			return "", err
		}
		res, err := shell.Run(ctx, cmd, root, r.Timeout)
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

// Specs returns OpenAI-shaped tool definitions.
func Specs() []llm.ToolSpec {
	return []llm.ToolSpec{
		fn("read_file", "Read a file from the workspace", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Relative path"},
			},
			"required": []string{"path"},
		}),
		fn("write_file", "Create or overwrite a workspace file", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		}),
		fn("list_dir", "List directory entries", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
		}),
		fn("search_files", "Search filenames and file contents", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		}),
		fn("run_terminal", "Run PowerShell (Windows) or bash command in workspace", map[string]any{
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
		fn("git_push", "Push to remote (Gitea/GitHub/etc)", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"remote": map[string]any{"type": "string"},
				"branch": map[string]any{"type": "string"},
			},
		}),
		fn("ssh_exec", "Run command on remote host via SSH", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host":     map[string]any{"type": "string"},
				"command":  map[string]any{"type": "string"},
				"user":     map[string]any{"type": "string"},
				"port":     map[string]any{"type": "integer"},
				"key_name": map[string]any{"type": "string"},
				"password": map[string]any{"type": "string"},
			},
			"required": []string{"host", "command"},
		}),
		fn("ssh_keygen", "Generate ed25519 SSH key pair in app ssh dir", map[string]any{
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
