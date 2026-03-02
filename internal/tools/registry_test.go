package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistryRegisterAndList(t *testing.T) {
	r := NewRegistry()

	r.Register(ToolDef{
		Name:        "search",
		Description: "Search the web",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
	}, func(_ context.Context, args json.RawMessage) (string, error) {
		return "results", nil
	})

	r.Register(ToolDef{
		Name:        "calculator",
		Description: "Do math",
	}, func(_ context.Context, args json.RawMessage) (string, error) {
		return "42", nil
	})

	tools := r.List()
	if len(tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "search" {
		t.Errorf("Expected first tool 'search', got %q", tools[0].Name)
	}
	if tools[1].Name != "calculator" {
		t.Errorf("Expected second tool 'calculator', got %q", tools[1].Name)
	}
}

func TestRegistryExecute(t *testing.T) {
	r := NewRegistry()

	r.Register(ToolDef{Name: "echo", Description: "Echo input"}, func(_ context.Context, args json.RawMessage) (string, error) {
		var input struct {
			Text string `json:"text"`
		}
		json.Unmarshal(args, &input)
		return input.Text, nil
	})

	result := r.Execute(context.Background(), ToolCall{
		ID:        "1",
		Name:      "echo",
		Arguments: json.RawMessage(`{"text":"hello"}`),
	})

	if result.IsError {
		t.Fatalf("Unexpected error: %s", result.Content)
	}
	if result.Content != "hello" {
		t.Errorf("Expected 'hello', got %q", result.Content)
	}
}

func TestRegistryExecuteNotFound(t *testing.T) {
	r := NewRegistry()

	result := r.Execute(context.Background(), ToolCall{
		ID:   "1",
		Name: "nonexistent",
	})

	if !result.IsError {
		t.Error("Expected error for missing tool")
	}
}

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register(ToolDef{Name: "a", Description: "A"}, nil)
	r.Register(ToolDef{Name: "b", Description: "B"}, nil)

	r.Unregister("a")

	tools := r.List()
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "b" {
		t.Errorf("Expected 'b', got %q", tools[0].Name)
	}
}

func TestFormatForPrompt(t *testing.T) {
	r := NewRegistry()
	r.Register(ToolDef{
		Name:        "search",
		Description: "Search the web",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}, nil)

	text := r.FormatForPrompt()
	if !strings.Contains(text, "### search") {
		t.Error("Expected tool header in prompt")
	}
	if !strings.Contains(text, "Search the web") {
		t.Error("Expected description in prompt")
	}
	if !strings.Contains(text, `"tool"`) {
		t.Error("Expected JSON usage instructions")
	}
}

func TestFormatForPromptEmpty(t *testing.T) {
	r := NewRegistry()
	if r.FormatForPrompt() != "" {
		t.Error("Expected empty string for no tools")
	}
}
