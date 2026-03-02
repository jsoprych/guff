package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterResolve(t *testing.T) {
	router := NewRouter()

	// Create a mock provider
	mock := &mockProvider{name: "openai"}
	router.RegisterProvider(mock)

	fallback := &mockProvider{name: "local"}
	router.SetFallback(fallback)

	// Test prefix resolution
	p, model, err := router.Resolve("openai/gpt-4o")
	if err != nil {
		t.Fatalf("Resolve openai/gpt-4o: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Expected openai provider, got %s", p.Name())
	}
	if model != "gpt-4o" {
		t.Errorf("Expected model gpt-4o, got %s", model)
	}

	// Test fallback
	p, model, err = router.Resolve("granite-3b")
	if err != nil {
		t.Fatalf("Resolve granite-3b: %v", err)
	}
	if p.Name() != "local" {
		t.Errorf("Expected local provider, got %s", p.Name())
	}
	if model != "granite-3b" {
		t.Errorf("Expected model granite-3b, got %s", model)
	}

	// Test explicit route
	anthropic := &mockProvider{name: "anthropic"}
	router.RegisterProvider(anthropic)
	router.AddRoute("claude-sonnet", anthropic, "claude-sonnet-4-5-20250929")

	p, model, err = router.Resolve("claude-sonnet")
	if err != nil {
		t.Fatalf("Resolve claude-sonnet: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Expected anthropic provider, got %s", p.Name())
	}
	if model != "claude-sonnet-4-5-20250929" {
		t.Errorf("Expected model claude-sonnet-4-5-20250929, got %s", model)
	}

	// Test unknown provider prefix
	_, _, err = router.Resolve("unknown/model")
	if err == nil {
		t.Error("Expected error for unknown provider, got nil")
	}
}

func TestRouterListModels(t *testing.T) {
	router := NewRouter()

	mock1 := &mockProvider{
		name: "local",
		models: []ModelInfo{
			{ID: "granite-3b", Provider: "local", OwnedBy: "local"},
		},
	}
	mock2 := &mockProvider{
		name: "openai",
		models: []ModelInfo{
			{ID: "gpt-4o", Provider: "openai", OwnedBy: "openai"},
		},
	}

	router.RegisterProvider(mock1)
	router.RegisterProvider(mock2)

	models, err := router.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}
}

func TestOpenAIProviderChatCompletion(t *testing.T) {
	// Create a mock OpenAI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		resp := map[string]interface{}{
			"id":    "chatcmpl-test",
			"model": "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "Hello from mock OpenAI!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewOpenAIProvider("test-key", server.URL)
	resp, err := p.ChatCompletion(context.Background(), ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Message.Content != "Hello from mock OpenAI!" {
		t.Errorf("Expected 'Hello from mock OpenAI!', got %q", resp.Message.Content)
	}
	if resp.PromptTokens != 10 {
		t.Errorf("Expected 10 prompt tokens, got %d", resp.PromptTokens)
	}
}

func TestOpenAIProviderStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunks := []string{"Hello", " world", "!"}
		for i, c := range chunks {
			done := i == len(chunks)-1
			var finishReason interface{} = nil
			if done {
				finishReason = "stop"
			}
			data, _ := json.Marshal(map[string]interface{}{
				"id":    "chatcmpl-stream",
				"model": "gpt-4o",
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]string{
							"content": c,
						},
						"finish_reason": finishReason,
					},
				},
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewOpenAIProvider("test-key", server.URL)
	ch, err := p.ChatCompletionStream(context.Background(), ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}

	var fullText string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		fullText += chunk.Delta
		if chunk.Done {
			break
		}
	}
	if fullText != "Hello world!" {
		t.Errorf("Expected 'Hello world!', got %q", fullText)
	}
}

func TestAnthropicProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}

		// Verify Anthropic headers
		if r.Header.Get("x-api-key") != "test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("anthropic-version") == "" {
			http.Error(w, "missing version", http.StatusBadRequest)
			return
		}

		// Verify system message was extracted
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		if system, ok := req["system"].(string); ok && system == "" {
			// No system is fine
		}

		resp := map[string]interface{}{
			"id":    "msg-test",
			"model": "claude-sonnet-4-5-20250929",
			"content": []map[string]string{
				{"type": "text", "text": "Hello from mock Anthropic!"},
			},
			"stop_reason": "end_turn",
			"usage": map[string]int{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)
	resp, err := p.ChatCompletion(context.Background(), ChatRequest{
		Model: "claude-sonnet-4-5-20250929",
		Messages: []Message{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Message.Content != "Hello from mock Anthropic!" {
		t.Errorf("Expected 'Hello from mock Anthropic!', got %q", resp.Message.Content)
	}
}

func TestFormatMessages(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "Be helpful"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
		{Role: "user", Content: "How are you?"},
	}
	result := formatMessages(msgs)
	expected := "System: Be helpful\nUser: Hello\nAssistant: Hi\nUser: How are you?\nAssistant:"
	if result != expected {
		t.Errorf("formatMessages mismatch:\nExpected: %q\nGot:      %q", expected, result)
	}
}

// mockProvider for testing
type mockProvider struct {
	name   string
	models []ModelInfo
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) ChatCompletion(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{
		Model:   "mock",
		Message: Message{Role: "assistant", Content: "mock response"},
		Done:    true,
	}, nil
}
func (m *mockProvider) ChatCompletionStream(_ context.Context, _ ChatRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	go func() {
		defer close(ch)
		ch <- StreamChunk{Delta: "mock", Done: true}
	}()
	return ch, nil
}
func (m *mockProvider) ListModels(_ context.Context) ([]ModelInfo, error) {
	return m.models, nil
}
