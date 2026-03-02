package provider

import (
	"context"
	"errors"
)

var (
	ErrProviderNotFound = errors.New("provider not found")
	ErrModelNotFound    = errors.New("model not found")
)

// Message represents a chat message in the unified format.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the unified request format for all providers.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float32  `json:"temperature,omitempty"`
	TopP        *float32  `json:"top_p,omitempty"`
	TopK        *int      `json:"top_k,omitempty"`
	MinP        *float32  `json:"min_p,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
	Seed        *uint32   `json:"seed,omitempty"`
	Stream      bool      `json:"stream,omitempty"`

	// Extended sampler parameters (used by local provider)
	TypicalP      *float32           `json:"typical_p,omitempty"`
	TopNSigma     *float32           `json:"top_n_sigma,omitempty"`
	DryMultiplier *float32           `json:"dry_multiplier,omitempty"`
	Grammar       string             `json:"grammar,omitempty"`
	LogitBias     map[int32]float32  `json:"logit_bias,omitempty"`

	// Tool calling fields (used by providers that support function calling)
	Tools    []Tool `json:"tools,omitempty"`
	ToolCall bool   `json:"tool_call,omitempty"` // hint that tool use is expected
}

// Tool describes a callable function for tool use / function calling.
type Tool struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a function that the model can call.
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // JSON Schema object
}

// ChatResponse is the unified response format.
type ChatResponse struct {
	Model        string  `json:"model"`
	Message      Message `json:"message"`
	PromptTokens int     `json:"prompt_tokens"`
	GenTokens    int     `json:"gen_tokens"`
	Done         bool    `json:"done"`
}

// StreamChunk is a single chunk from a streaming response.
type StreamChunk struct {
	Delta string // incremental text
	Done  bool
	Error error
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	OwnedBy  string `json:"owned_by"`
}

// Provider is the interface that all backends (local, openai, anthropic) implement.
type Provider interface {
	// Name returns the provider identifier.
	Name() string

	// ChatCompletion performs a non-streaming chat completion.
	ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// ChatCompletionStream performs a streaming chat completion.
	ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)

	// ListModels returns available models for this provider.
	ListModels(ctx context.Context) ([]ModelInfo, error)
}
