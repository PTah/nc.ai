package tools

// Package tools registers agent function-calling handlers (read/write/shell/git/ssh).
// See docs/exchange-protocols/tools-and-agent-loop.md

import "notcursor.ai/app/internal/llm"

// Specs returns the stage-1 tool definitions for the provider payload.
func Specs() []llm.ToolSpec {
	return []llm.ToolSpec{
		fn("read_file", "Read a file from the workspace", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
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
		fn("list_dir", "List directory entries in the workspace", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
		}),
		fn("run_terminal", "Run a shell/PowerShell command in the workspace", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		}),
		fn("git_status", "Show git status for the workspace repo", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}),
		fn("git_commit", "Create a git commit", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		}),
		fn("git_push", "Push commits to remote (e.g. Gitea)", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"remote": map[string]any{"type": "string"},
				"branch": map[string]any{"type": "string"},
			},
		}),
		fn("ssh_exec", "Execute a command over SSH", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host":    map[string]any{"type": "string"},
				"command": map[string]any{"type": "string"},
				"user":    map[string]any{"type": "string"},
			},
			"required": []string{"host", "command"},
		}),
		fn("ssh_keygen", "Generate a local SSH key pair", map[string]any{
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
