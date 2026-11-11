// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:27:37
// Created by nc.ai 1.1.16 on macbook 2026-11-11
package mcp

import "context"

// liveSession — общий контракт живого соединения с MCP-сервером. Manager не
// знает, локальный это процесс (stdio) или hosted-эндпоинт (HTTP): оба обязаны
// уметь отдать список инструментов, вызвать инструмент и честно сказать, живы
// ли они ещё.
type liveSession interface {
	Alive() bool
	Tools() []mcpTool
	Logs() []string
	Close()
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
}
