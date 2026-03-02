package tools

import (
	"testing"
)

func TestParseToolCallMarkdown(t *testing.T) {
	output := "Let me search for that.\n```json\n{\"tool\": \"search\", \"arguments\": {\"query\": \"golang\"}}\n```"

	call, prefix, ok := ParseToolCall(output)
	if !ok {
		t.Fatal("Expected to parse tool call")
	}
	if call.Name != "search" {
		t.Errorf("Expected tool 'search', got %q", call.Name)
	}
	if prefix != "Let me search for that." {
		t.Errorf("Expected prefix 'Let me search for that.', got %q", prefix)
	}
	if string(call.Arguments) != `{"query": "golang"}` {
		t.Errorf("Unexpected arguments: %s", call.Arguments)
	}
}

func TestParseToolCallRawJSON(t *testing.T) {
	output := `I'll check that. {"tool": "calculator", "arguments": {"expr": "2+2"}}`

	call, prefix, ok := ParseToolCall(output)
	if !ok {
		t.Fatal("Expected to parse tool call")
	}
	if call.Name != "calculator" {
		t.Errorf("Expected tool 'calculator', got %q", call.Name)
	}
	if prefix != "I'll check that." {
		t.Errorf("Expected prefix, got %q", prefix)
	}
}

func TestParseToolCallNoCall(t *testing.T) {
	output := "This is just regular text without any tool calls."

	_, _, ok := ParseToolCall(output)
	if ok {
		t.Error("Expected no tool call to be found")
	}
}

func TestParseToolCallEmptyTool(t *testing.T) {
	output := `{"tool": "", "arguments": {}}`

	_, _, ok := ParseToolCall(output)
	if ok {
		t.Error("Expected empty tool name to be rejected")
	}
}
