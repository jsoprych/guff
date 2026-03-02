package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseToolCall attempts to extract a tool call from model output.
// Supports two formats:
//  1. JSON block in markdown: ```json\n{"tool": "name", "arguments": {...}}\n```
//  2. Raw JSON object: {"tool": "name", "arguments": {...}}
func ParseToolCall(output string) (*ToolCall, string, bool) {
	// Try markdown code block first
	if idx := strings.Index(output, "```json"); idx >= 0 {
		prefix := output[:idx]
		jsonStart := idx + len("```json")
		if end := strings.Index(output[jsonStart:], "```"); end >= 0 {
			jsonStr := strings.TrimSpace(output[jsonStart : jsonStart+end])
			if call, ok := tryParseCall(jsonStr); ok {
				return call, strings.TrimSpace(prefix), true
			}
		}
	}

	// Try raw JSON — find the outermost {...}
	if idx := strings.Index(output, `{"tool"`); idx >= 0 {
		prefix := output[:idx]
		depth := 0
		for i := idx; i < len(output); i++ {
			switch output[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					jsonStr := output[idx : i+1]
					if call, ok := tryParseCall(jsonStr); ok {
						return call, strings.TrimSpace(prefix), true
					}
				}
			}
		}
	}

	return nil, output, false
}

func tryParseCall(jsonStr string) (*ToolCall, bool) {
	var raw struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, false
	}
	if raw.Tool == "" {
		return nil, false
	}

	return &ToolCall{
		ID:        fmt.Sprintf("call_%d", len(jsonStr)), // simple ID
		Name:      raw.Tool,
		Arguments: raw.Arguments,
	}, true
}
