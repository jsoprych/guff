package engine

import (
	"context"
	"log"
	"strings"

	"github.com/jsoprych/guff/internal/provider"
	"github.com/jsoprych/guff/internal/tools"
)

// ChatEngine wraps a provider.Router and an optional tools.Registry to provide
// a unified chat completion interface with automatic tool call looping.
type ChatEngine struct {
	router        *provider.Router
	registry      *tools.Registry
	maxToolRounds int
	isLocal       func(string) bool // defaults to router.IsLocal; overridable in tests
}

// New creates a ChatEngine backed by the given provider router.
func New(router *provider.Router) *ChatEngine {
	e := &ChatEngine{
		router:        router,
		maxToolRounds: 5,
	}
	e.isLocal = router.IsLocal
	return e
}

// SetToolRegistry attaches a tool registry. When set, the engine will inject
// tool descriptions into prompts and execute a parse→call→re-generate loop.
func (e *ChatEngine) SetToolRegistry(r *tools.Registry) {
	e.registry = r
}

// ToolRegistry returns the engine's tool registry (may be nil).
func (e *ChatEngine) ToolRegistry() *tools.Registry {
	return e.registry
}

// ChatCompletion performs a non-streaming chat completion. If a tool registry
// is set, it injects tool descriptions and loops on tool calls (up to
// maxToolRounds).
func (e *ChatEngine) ChatCompletion(ctx context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
	// Inject tool prompt if registry has tools
	if e.registry != nil {
		if toolPrompt := e.registry.FormatForPrompt(); toolPrompt != "" {
			req.Messages = append(
				[]provider.Message{{Role: "system", Content: toolPrompt}},
				req.Messages...,
			)
		}
	}

	// Grammar-constrained tool calling: force valid JSON output for local models.
	// Only active when: tools are registered, model is local, caller hasn't set grammar.
	// Cleared before re-calls so the model can respond naturally after tool execution.
	if e.registry != nil && req.Grammar == "" {
		if g := tools.ToolGrammar(e.registry.List()); g != "" && e.isLocal(req.Model) {
			req.Grammar = g
		}
	}

	resp, err := e.router.ChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	req.Grammar = "" // allow natural response on re-calls after tool execution

	// Tool call loop
	if e.registry != nil {
		for i := 0; i < e.maxToolRounds; i++ {
			call, _, found := tools.ParseToolCall(resp.Message.Content)
			if !found {
				break
			}
			log.Printf("[tool call: %s]", call.Name)

			toolResult := e.registry.Execute(ctx, *call)
			log.Printf("[tool result: %d chars, error=%v]", len(toolResult.Content), toolResult.IsError)

			// Append assistant response and tool result, then re-generate
			req.Messages = append(req.Messages,
				provider.Message{Role: "assistant", Content: resp.Message.Content},
				provider.Message{Role: "tool", Content: toolResult.Content},
			)

			resp, err = e.router.ChatCompletion(ctx, req)
			if err != nil {
				return nil, err
			}
		}
	}

	return resp, nil
}

// ChatCompletionStream passes through to the router (no tool loop for streaming).
func (e *ChatEngine) ChatCompletionStream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	return e.router.ChatCompletionStream(ctx, req)
}

// ListModels passes through to the router.
func (e *ChatEngine) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	return e.router.ListModels(ctx)
}

// Embed routes an embedding request through the router.
func (e *ChatEngine) Embed(ctx context.Context, req provider.EmbeddingRequest) ([]provider.EmbeddingResult, error) {
	return e.router.Embed(ctx, req)
}

// ToolCallResult holds the output of a single tool call iteration, used by
// callers that need to inspect intermediate results (e.g. the chat command
// which prints prefix text and saves messages to session storage).
type ToolCallResult struct {
	Call       tools.ToolCall
	PrefixText string
	Result     tools.ToolResult
}

// ChatCompletionWithToolCallback performs a non-streaming chat completion with
// a callback invoked for each tool call. This lets callers hook into the loop
// (e.g. to persist messages, print status). The callback receives each tool
// call result and should return an error to abort the loop, or nil to continue.
func (e *ChatEngine) ChatCompletionWithToolCallback(
	ctx context.Context,
	req provider.ChatRequest,
	onToolCall func(ToolCallResult) error,
) (*provider.ChatResponse, error) {
	// Inject tool prompt if registry has tools
	if e.registry != nil {
		if toolPrompt := e.registry.FormatForPrompt(); toolPrompt != "" {
			req.Messages = append(
				[]provider.Message{{Role: "system", Content: toolPrompt}},
				req.Messages...,
			)
		}
	}

	// Grammar-constrained tool calling: force valid JSON output for local models.
	// Only active when: tools are registered, model is local, caller hasn't set grammar.
	// Cleared before re-calls so the model can respond naturally after tool execution.
	if e.registry != nil && req.Grammar == "" {
		if g := tools.ToolGrammar(e.registry.List()); g != "" && e.isLocal(req.Model) {
			req.Grammar = g
		}
	}

	resp, err := e.router.ChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	req.Grammar = "" // allow natural response on re-calls after tool execution

	// Tool call loop
	if e.registry != nil {
		for i := 0; i < e.maxToolRounds; i++ {
			call, prefixText, found := tools.ParseToolCall(resp.Message.Content)
			if !found {
				break
			}
			log.Printf("[tool call: %s]", call.Name)

			toolResult := e.registry.Execute(ctx, *call)
			log.Printf("[tool result: %d chars, error=%v]", len(toolResult.Content), toolResult.IsError)

			if onToolCall != nil {
				if err := onToolCall(ToolCallResult{
					Call:       *call,
					PrefixText: strings.TrimSpace(prefixText),
					Result:     toolResult,
				}); err != nil {
					return nil, err
				}
			}

			req.Messages = append(req.Messages,
				provider.Message{Role: "assistant", Content: resp.Message.Content},
				provider.Message{Role: "tool", Content: toolResult.Content},
			)

			resp, err = e.router.ChatCompletion(ctx, req)
			if err != nil {
				return nil, err
			}
		}
	}

	return resp, nil
}
