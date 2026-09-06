package llm

import "context"

// Message is the canonical OpenAI-shaped chat message.
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	Name             string     `json:"name,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolSpec struct {
	Type     string             `json:"type"`
	Function ToolSpecFunction   `json:"function"`
}

type ToolSpecFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ChatRequest struct {
	Model            string         `json:"model,omitempty"`
	Messages         []Message      `json:"messages"`
	Tools            []ToolSpec     `json:"tools,omitempty"`
	ToolChoice       any            `json:"tool_choice,omitempty"`
	Stream           bool           `json:"stream"`
	Temperature      *float64       `json:"temperature,omitempty"`
	MaxTokens        *int           `json:"max_tokens,omitempty"`
	Thinking         map[string]any `json:"thinking,omitempty"`
	ReasoningEffort  string         `json:"reasoning_effort,omitempty"`
	Extra            map[string]any `json:"-"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	FinishReason string  `json:"finish_reason"`
	Message      Message `json:"message"`
}

type Usage struct {
	PromptTokens            int `json:"prompt_tokens"`
	CompletionTokens        int `json:"completion_tokens"`
	TotalTokens             int `json:"total_tokens"`
	PromptCacheHitTokens    int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens   int `json:"prompt_cache_miss_tokens,omitempty"`
}

type StreamEvent struct {
	Type    string // delta | reasoning | tool_call | done | error
	Content string
	Raw     any
}

// Provider is implemented by DeepSeek, OpenAI, OpenRouter, Ollama, etc.
type Provider interface {
	Name() string
	ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, req *ChatRequest, out chan<- StreamEvent) error
}
