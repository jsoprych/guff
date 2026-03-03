package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jsoprych/guff/internal/tools"
)

// --- Wrap tests ---

type searchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type searchResult struct {
	Matches int    `json:"matches"`
	Status  string `json:"status"`
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		fn      func(ctx context.Context, input searchInput) (searchResult, error)
		wantErr bool
		wantOut string
	}{
		{
			name: "happy path",
			args: `{"query": "hello", "limit": 5}`,
			fn: func(_ context.Context, input searchInput) (searchResult, error) {
				return searchResult{Matches: 3, Status: "ok"}, nil
			},
			wantOut: `{"matches":3,"status":"ok"}`,
		},
		{
			name: "invalid JSON",
			args: `not json`,
			fn: func(_ context.Context, _ searchInput) (searchResult, error) {
				t.Fatal("should not be called")
				return searchResult{}, nil
			},
			wantErr: true,
		},
		{
			name: "function error",
			args: `{"query": "fail"}`,
			fn: func(_ context.Context, _ searchInput) (searchResult, error) {
				return searchResult{}, errors.New("search failed")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := Wrap("test_search", "test", json.RawMessage(`{}`), tt.fn)

			result, err := spec.Handler(context.Background(), json.RawMessage(tt.args))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.wantOut {
				t.Errorf("got %q, want %q", result, tt.wantOut)
			}
		})
	}
}

// --- WrapVoid tests ---

type storeInput struct {
	Content string `json:"content"`
}

func TestWrapVoid(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		fn      func(ctx context.Context, input storeInput) error
		wantErr bool
		wantOut string
	}{
		{
			name: "happy path",
			args: `{"content": "remember this"}`,
			fn: func(_ context.Context, input storeInput) error {
				if input.Content != "remember this" {
					t.Fatalf("got content %q", input.Content)
				}
				return nil
			},
			wantOut: "ok",
		},
		{
			name: "invalid JSON",
			args: `{bad`,
			fn: func(_ context.Context, _ storeInput) error {
				t.Fatal("should not be called")
				return nil
			},
			wantErr: true,
		},
		{
			name: "function error",
			args: `{"content": "fail"}`,
			fn: func(_ context.Context, _ storeInput) error {
				return errors.New("store failed")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := WrapVoid("test_store", "test", json.RawMessage(`{}`), tt.fn)

			result, err := spec.Handler(context.Background(), json.RawMessage(tt.args))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.wantOut {
				t.Errorf("got %q, want %q", result, tt.wantOut)
			}
		})
	}
}

// --- ToolName tests ---

func TestToolName(t *testing.T) {
	tests := []struct {
		namespace string
		verb      string
		parts     []string
		want      string
	}{
		{"memory", "search", nil, "memory_search"},
		{"memory", "store", nil, "memory_store"},
		{"session", "add", []string{"message"}, "session_add_message"},
		{"context", "set", []string{"strategy"}, "context_set_strategy"},
		{"provider", "chat", []string{"stream"}, "provider_chat_stream"},
		{"embed", "generate", nil, "embed_generate"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := ToolName(tt.namespace, tt.verb, tt.parts...)
			if got != tt.want {
				t.Errorf("ToolName(%q, %q, %v) = %q, want %q",
					tt.namespace, tt.verb, tt.parts, got, tt.want)
			}
		})
	}
}

// --- RegisterAll tests ---

func TestRegisterAll(t *testing.T) {
	registry := tools.NewRegistry()

	specs := []ToolSpec{
		Wrap[searchInput, searchResult](
			"memory_search", "Search memories",
			json.RawMessage(`{"type":"object"}`),
			func(_ context.Context, _ searchInput) (searchResult, error) {
				return searchResult{Matches: 1}, nil
			},
		),
		WrapVoid[storeInput](
			"memory_store", "Store a memory",
			json.RawMessage(`{"type":"object"}`),
			func(_ context.Context, _ storeInput) error {
				return nil
			},
		),
	}

	RegisterAll(registry, specs)

	defs := registry.List()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}
	if defs[0].Name != "memory_search" {
		t.Errorf("first tool = %q, want %q", defs[0].Name, "memory_search")
	}
	if defs[1].Name != "memory_store" {
		t.Errorf("second tool = %q, want %q", defs[1].Name, "memory_store")
	}

	// Verify tools are executable via the registry
	result := registry.Execute(context.Background(), tools.ToolCall{
		ID:        "1",
		Name:      "memory_search",
		Arguments: json.RawMessage(`{"query":"test","limit":1}`),
	})
	if result.IsError {
		t.Fatalf("execute returned error: %s", result.Content)
	}
	if result.Content != `{"matches":1,"status":""}` {
		t.Errorf("execute result = %q", result.Content)
	}
}

// --- ToolSpec Def fields ---

func TestWrapSetsDefFields(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
	spec := Wrap[searchInput, searchResult](
		"memory_search", "Search memories", schema,
		func(_ context.Context, _ searchInput) (searchResult, error) {
			return searchResult{}, nil
		},
	)

	if spec.Def.Name != "memory_search" {
		t.Errorf("Name = %q", spec.Def.Name)
	}
	if spec.Def.Description != "Search memories" {
		t.Errorf("Description = %q", spec.Def.Description)
	}
	if string(spec.Def.Parameters) != string(schema) {
		t.Errorf("Parameters = %s", spec.Def.Parameters)
	}
}

// --- Namespace constants ---

func TestNamespaceConstants(t *testing.T) {
	// Verify all namespace constants are non-empty and distinct
	namespaces := []string{NSMemory, NSSession, NSContext, NSState, NSProvider, NSEmbed}
	seen := make(map[string]bool)
	for _, ns := range namespaces {
		if ns == "" {
			t.Error("empty namespace constant")
		}
		if seen[ns] {
			t.Errorf("duplicate namespace: %q", ns)
		}
		seen[ns] = true
	}
}
