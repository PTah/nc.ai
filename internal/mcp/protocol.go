// Edited by nc.ai 0.8.5.4 on new-admin-pc 2026-10-13 10:27:58
// Package mcp implements a minimal Model Context Protocol client (stdio MVP).
package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

const protocolVersion = "2024-11-05"

// ServerConfig is one MCP server entry in Settings.
type ServerConfig struct {
	ID        string            `json:"id"`
	Label     string            `json:"label,omitempty"`
	Enabled   bool              `json:"enabled"`
	Transport string            `json:"transport,omitempty"` // stdio | http (MVP: stdio)
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	CWD       string            `json:"cwd,omitempty"`
	TimeoutSec int              `json:"timeoutSec,omitempty"`
	// EnabledTools — allowlist of local tool names (without the mcp__ prefix).
	// Empty = none exposed to the model (safe default).
	EnabledTools []string `json:"enabledTools,omitempty"`
}

// Normalize fills defaults and trims ids.
func (c *ServerConfig) Normalize() {
	if c == nil {
		return
	}
	c.ID = sanitizeID(c.ID)
	c.Label = strings.TrimSpace(c.Label)
	if c.Label == "" {
		c.Label = c.ID
	}
	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	if c.Transport == "" {
		c.Transport = "stdio"
	}
	c.Command = strings.TrimSpace(c.Command)
	if c.TimeoutSec <= 0 {
		c.TimeoutSec = 60
	}
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "mcp"
	}
	return out
}

// NamePrefix — MCP tools are exposed to the model as mcp__<server>__<tool>.
// OpenAI and Anthropic allow only [A-Za-z0-9_-] in a function name, so the old
// "mcp:server:tool" (with colons) was not portable and broke strict providers.
const NamePrefix = "mcp__"

// PrefixedName builds a provider-safe tool name: mcp__<server>__<tool>.
func PrefixedName(serverID, toolName string) string {
	return NamePrefix + safePart(serverID, true) + "__" + safePart(toolName, false)
}

// SplitPrefixed parses mcp__<server>__<tool>. ok=false if not an MCP tool name.
func SplitPrefixed(name string) (serverID, toolName string, ok bool) {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, NamePrefix) {
		return "", "", false
	}
	rest := name[len(NamePrefix):]
	i := strings.Index(rest, "__")
	if i <= 0 || i+2 >= len(rest) {
		return "", "", false
	}
	return rest[:i], rest[i+2:], true
}

// safePart maps s to [A-Za-z0-9_-]. When collapse is set, runs of separators
// become a single "_" so the first "__" always separates server from tool.
func safePart(s string, collapse bool) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	prevSep := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
			prevSep = false
		default:
			if collapse && prevSep {
				continue
			}
			b.WriteByte('_')
			prevSep = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "x"
	}
	return out
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"` // notifications / server requests
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("mcp rpc %d: %s", e.Code, e.Message)
}

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      map[string]any `json:"clientInfo"`
}

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      map[string]any `json:"serverInfo"`
}

type toolListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type callToolResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
