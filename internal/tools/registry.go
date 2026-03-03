package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrToolNotFound   = errors.New("tool not found")
	ErrToolExecFailed = errors.New("tool execution failed")
)

// ToolDef describes a tool that can be called by the model.
// Compatible with the OpenAI function calling schema.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the result of executing a tool.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// ToolHandler executes a tool call and returns the result.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// Registry holds registered tools and their handlers.
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]ToolDef
	handlers map[string]ToolHandler
	order    []string // preserves registration order
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:    make(map[string]ToolDef),
		handlers: make(map[string]ToolHandler),
	}
}

// Register adds a tool with its handler.
func (r *Registry) Register(def ToolDef, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[def.Name]; !exists {
		r.order = append(r.order, def.Name)
	}
	r.tools[def.Name] = def
	r.handlers[def.Name] = handler
}

// Unregister removes a tool by name.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
	delete(r.handlers, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// List returns all registered tool definitions in registration order.
func (r *Registry) List() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDef, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, r.tools[name])
	}
	return defs
}

// Get returns a tool definition by name.
func (r *Registry) Get(name string) (ToolDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.tools[name]
	return def, ok
}

// Execute runs a tool call and returns the result.
func (r *Registry) Execute(ctx context.Context, call ToolCall) ToolResult {
	r.mu.RLock()
	handler, ok := r.handlers[call.Name]
	r.mu.RUnlock()

	if !ok {
		return ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("tool %q not found", call.Name),
			IsError: true,
		}
	}

	result, err := handler(ctx, call.Arguments)
	if err != nil {
		return ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("error: %v", err),
			IsError: true,
		}
	}

	return ToolResult{
		CallID:  call.ID,
		Content: result,
	}
}

// FormatForPrompt generates a text description of available tools
// suitable for injection into a system prompt (for local models that
// don't natively support function calling).
func (r *Registry) FormatForPrompt() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.tools) == 0 {
		return ""
	}

	var b []byte
	b = append(b, "You have access to the following tools:\n\n"...)

	for _, name := range r.order {
		def := r.tools[name]
		b = append(b, fmt.Sprintf("### %s\n%s\n", def.Name, def.Description)...)
		if len(def.Parameters) > 0 && string(def.Parameters) != "null" {
			b = append(b, fmt.Sprintf("Parameters: %s\n", string(def.Parameters))...)
		}
		b = append(b, '\n')
	}

	b = append(b, "To use a tool, respond with a JSON block:\n"...)
	b = append(b, "```json\n{\"tool\": \"tool_name\", \"arguments\": {\"arg\": \"value\"}}\n```\n"...)

	return string(b)
}
