package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jsoprych/guff/internal/tools"
)

// ToolSpec pairs a tool definition with its handler for batch registration.
type ToolSpec struct {
	Def     tools.ToolDef
	Handler tools.ToolHandler
}

// Wrap creates a ToolSpec from a typed function. The function receives a
// decoded input of type A and returns a result of type R (marshaled to JSON)
// or an error.
func Wrap[A any, R any](name, description string, schema json.RawMessage, fn func(ctx context.Context, input A) (R, error)) ToolSpec {
	return ToolSpec{
		Def: tools.ToolDef{
			Name:        name,
			Description: description,
			Parameters:  schema,
		},
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var input A
			if err := json.Unmarshal(args, &input); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			result, err := fn(ctx, input)
			if err != nil {
				return "", err
			}
			out, err := json.Marshal(result)
			if err != nil {
				return "", fmt.Errorf("marshal result: %w", err)
			}
			return string(out), nil
		},
	}
}

// WrapVoid creates a ToolSpec from a typed function that returns only an error.
// On success the tool returns the string "ok".
func WrapVoid[A any](name, description string, schema json.RawMessage, fn func(ctx context.Context, input A) error) ToolSpec {
	return ToolSpec{
		Def: tools.ToolDef{
			Name:        name,
			Description: description,
			Parameters:  schema,
		},
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var input A
			if err := json.Unmarshal(args, &input); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if err := fn(ctx, input); err != nil {
				return "", err
			}
			return "ok", nil
		},
	}
}

// ToolName builds a conventional MCP tool name from namespace and verb parts.
// With one part: "{ns}_{verb}". With additional parts: "{ns}_{verb}_{noun}".
//
//	ToolName("memory", "search")           => "memory_search"
//	ToolName("session", "add", "message")  => "session_add_message"
func ToolName(namespace, verb string, parts ...string) string {
	name := namespace + "_" + verb
	for _, p := range parts {
		name += "_" + p
	}
	return name
}

// RegisterAll batch-registers tool specs into an existing tools.Registry.
func RegisterAll(registry *tools.Registry, specs []ToolSpec) {
	for _, s := range specs {
		registry.Register(s.Def, s.Handler)
	}
}
