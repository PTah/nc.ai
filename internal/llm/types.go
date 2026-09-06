package llm

import "context"

// ContentPart is an OpenAI/DeepSeek multimodal content block.
type ContentPart struct {
	Type     string    `json:"type"` // text | image_url
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // low | high | original | auto
}

// Message is the canonical OpenAI-shaped chat message.
type Message struct {
	Role             string        `json:"role"`
	Content          string        `json:"content,omitempty"`
	Parts            []ContentPart `json:"-"` // if set, marshaled as content array
	Name             string        `json:"name,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall    `json:"tool_calls,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
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
	Type     string           `json:"type"`
	Function ToolSpecFunction `json:"function"`
}

type ToolSpecFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ChatRequest struct {
	Model           string         `json:"model,omitempty"`
	Messages        []Message      `json:"messages"`
	Tools           []ToolSpec     `json:"tools,omitempty"`
	ToolChoice      any            `json:"tool_choice,omitempty"`
	Stream          bool           `json:"stream"`
	Temperature     *float64       `json:"temperature,omitempty"`
	MaxTokens       *int           `json:"max_tokens,omitempty"`
	Thinking        map[string]any `json:"thinking,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Extra           map[string]any `json:"-"`
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
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}

type StreamEvent struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Raw     any    `json:"raw,omitempty"`
}

type Provider interface {
	Name() string
	ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
}

func ToolResultMessage(callID, content string) Message {
	return Message{Role: "tool", ToolCallID: callID, Content: content}
}

// UserText builds a plain user message.
func UserText(text string) Message {
	return Message{Role: "user", Content: text}
}

// UserMultimodal builds a user message with text + optional images (data URLs or http).
func UserMultimodal(text string, imageDataURLs []string) Message {
	parts := make([]ContentPart, 0, 1+len(imageDataURLs))
	if text != "" {
		parts = append(parts, ContentPart{Type: "text", Text: text})
	}
	for _, u := range imageDataURLs {
		if u == "" {
			continue
		}
		parts = append(parts, ContentPart{
			Type:     "image_url",
			ImageURL: &ImageURL{URL: u, Detail: "original"},
		})
	}
	if len(parts) == 1 && parts[0].Type == "text" {
		return Message{Role: "user", Content: parts[0].Text}
	}
	return Message{Role: "user", Parts: parts, Content: text}
}

func (m Message) HasImages() bool {
	for _, p := range m.Parts {
		if p.Type == "image_url" {
			return true
		}
	}
	return false
}
