// Created by nc.ai 1.1.18 on new-admin-pc 2026-11-12 10:32:28
package agent

import (
	"strings"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func spec(name string) llm.ToolSpec {
	return llm.ToolSpec{Function: llm.ToolSpecFunction{Name: name}}
}

// MCP-first подсказка усиливается только когда у сервера реально есть поиск.
func TestMCPFirstHintOnlyWithSearchTools(t *testing.T) {
	withSearch := &Runner{ExtraTools: []llm.ToolSpec{spec("mcp__codebase-memory__search_graph")}}
	prompt := withSearch.systemPrompt()
	if !strings.Contains(prompt, "MCP-first rule") {
		t.Fatalf("нет жёсткой MCP-first подсказки при наличии поиска:\n%s", prompt)
	}

	noSearch := &Runner{ExtraTools: []llm.ToolSpec{spec("mcp__weather__forecast")}}
	prompt = noSearch.systemPrompt()
	if strings.Contains(prompt, "MCP-first rule") {
		t.Fatalf("жёсткая подсказка появилась без инструментов поиска")
	}
	if !strings.Contains(prompt, "MCP tools") {
		t.Fatalf("базовая подсказка про MCP пропала")
	}
}

func TestMCPHasSearchTools(t *testing.T) {
	if mcpHasSearchTools(nil) {
		t.Fatal("пустой список не содержит поиска")
	}
	if !mcpHasSearchTools([]llm.ToolSpec{spec("mcp__memory__recall")}) {
		t.Fatal("memory/recall должен считаться поиском")
	}
	if mcpHasSearchTools([]llm.ToolSpec{spec("mcp__gitea__create_issue")}) {
		t.Fatal("create_issue — не поиск")
	}
}
