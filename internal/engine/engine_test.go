package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jsoprych/guff/internal/provider"
	"github.com/jsoprych/guff/internal/tools"
)

// mockProvider implements provider.Provider with a response queue.
type mockProvider struct {
	responses []*provider.ChatResponse
	streams   [][]provider.StreamChunk
	models    []provider.ModelInfo
	callCount int
	lastReqs  []provider.ChatRequest
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) ChatCompletion(_ context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
	m.lastReqs = append(m.lastReqs, req)
	idx := m.callCount
	m.callCount++
	if idx >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}
	return m.responses[idx], nil
}

func (m *mockProvider) ChatCompletionStream(_ context.Context, req provider.ChatRequest) (<-chan provider.StreamChunk, error) {
	m.lastReqs = append(m.lastReqs, req)
	idx := m.callCount
	m.callCount++
	if idx >= len(m.streams) {
		return nil, errors.New("no more mock streams")
	}
	ch := make(chan provider.StreamChunk, len(m.streams[idx]))
	for _, chunk := range m.streams[idx] {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (m *mockProvider) ListModels(_ context.Context) ([]provider.ModelInfo, error) {
	return m.models, nil
}

// newMockRouter creates a provider.Router backed by the mock.
func newMockRouter(mock *mockProvider) *provider.Router {
	r := provider.NewRouter()
	r.RegisterProvider(mock)
	r.SetFallback(mock)
	return r
}

// newTestRegistry creates a registry with a simple echo tool.
func newTestRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.ToolDef{
		Name:        "echo",
		Description: "Echoes back the input",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
	}, func(_ context.Context, args json.RawMessage) (string, error) {
		var a struct{ Text string `json:"text"` }
		json.Unmarshal(args, &a)
		return "echo: " + a.Text, nil
	})
	return reg
}

func TestChatCompletion_NoTools(t *testing.T) {
	mock := &mockProvider{
		responses: []*provider.ChatResponse{
			{Model: "test", Message: provider.Message{Role: "assistant", Content: "hello"}, Done: true},
		},
	}
	eng := New(newMockRouter(mock))

	req := provider.ChatRequest{
		Model:    "test",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	}
	resp, err := eng.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "hello" {
		t.Errorf("got %q, want %q", resp.Message.Content, "hello")
	}
	if mock.callCount != 1 {
		t.Errorf("provider called %d times, want 1", mock.callCount)
	}
}

func TestChatCompletion_ToolCallLoop(t *testing.T) {
	toolCallJSON := "```json\n{\"tool\": \"echo\", \"arguments\": {\"text\": \"test\"}}\n```"
	mock := &mockProvider{
		responses: []*provider.ChatResponse{
			{Model: "test", Message: provider.Message{Role: "assistant", Content: toolCallJSON}},
			{Model: "test", Message: provider.Message{Role: "assistant", Content: "final answer"}, Done: true},
		},
	}
	eng := New(newMockRouter(mock))
	eng.SetToolRegistry(newTestRegistry())

	req := provider.ChatRequest{
		Model:    "test",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	}
	resp, err := eng.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "final answer" {
		t.Errorf("got %q, want %q", resp.Message.Content, "final answer")
	}
	if mock.callCount != 2 {
		t.Errorf("provider called %d times, want 2", mock.callCount)
	}
	// Verify tool result was appended to messages
	lastReq := mock.lastReqs[1]
	foundToolMsg := false
	for _, m := range lastReq.Messages {
		if m.Role == "tool" && m.Content == "echo: test" {
			foundToolMsg = true
		}
	}
	if !foundToolMsg {
		t.Error("tool result message not found in second request")
	}
}

func TestChatCompletion_MaxToolRounds(t *testing.T) {
	toolCallJSON := "```json\n{\"tool\": \"echo\", \"arguments\": {\"text\": \"loop\"}}\n```"
	// Create 6 responses (5 tool calls + 1 extra that shouldn't be reached)
	responses := make([]*provider.ChatResponse, 6)
	for i := range responses {
		responses[i] = &provider.ChatResponse{
			Model:   "test",
			Message: provider.Message{Role: "assistant", Content: toolCallJSON},
		}
	}
	mock := &mockProvider{responses: responses}
	eng := New(newMockRouter(mock))
	eng.SetToolRegistry(newTestRegistry())

	req := provider.ChatRequest{
		Model:    "test",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	}
	resp, err := eng.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After 5 rounds + initial = 6 total calls
	if mock.callCount != 6 {
		t.Errorf("provider called %d times, want 6 (1 initial + 5 rounds)", mock.callCount)
	}
	// The response should still contain a tool call (loop didn't produce final answer)
	if resp.Message.Content != toolCallJSON {
		t.Errorf("expected last tool call response to be returned")
	}
}

func TestChatCompletion_ToolPromptInjection(t *testing.T) {
	mock := &mockProvider{
		responses: []*provider.ChatResponse{
			{Model: "test", Message: provider.Message{Role: "assistant", Content: "ok"}},
		},
	}
	eng := New(newMockRouter(mock))
	eng.SetToolRegistry(newTestRegistry())

	req := provider.ChatRequest{
		Model:    "test",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	}
	_, err := eng.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Check that system message was injected
	lastReq := mock.lastReqs[0]
	if len(lastReq.Messages) < 2 {
		t.Fatal("expected at least 2 messages (system + user)")
	}
	if lastReq.Messages[0].Role != "system" {
		t.Errorf("first message role = %q, want %q", lastReq.Messages[0].Role, "system")
	}
	if lastReq.Messages[0].Content == "" {
		t.Error("system message content is empty")
	}
}

func TestChatCompletionWithToolCallback(t *testing.T) {
	toolCallJSON := "Some text before\n```json\n{\"tool\": \"echo\", \"arguments\": {\"text\": \"cb\"}}\n```"
	mock := &mockProvider{
		responses: []*provider.ChatResponse{
			{Model: "test", Message: provider.Message{Role: "assistant", Content: toolCallJSON}},
			{Model: "test", Message: provider.Message{Role: "assistant", Content: "done"}},
		},
	}
	eng := New(newMockRouter(mock))
	eng.SetToolRegistry(newTestRegistry())

	var callbackResults []ToolCallResult
	resp, err := eng.ChatCompletionWithToolCallback(
		context.Background(),
		provider.ChatRequest{
			Model:    "test",
			Messages: []provider.Message{{Role: "user", Content: "hi"}},
		},
		func(tcr ToolCallResult) error {
			callbackResults = append(callbackResults, tcr)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "done" {
		t.Errorf("got %q, want %q", resp.Message.Content, "done")
	}
	if len(callbackResults) != 1 {
		t.Fatalf("callback called %d times, want 1", len(callbackResults))
	}
	if callbackResults[0].Call.Name != "echo" {
		t.Errorf("tool name = %q, want %q", callbackResults[0].Call.Name, "echo")
	}
	if callbackResults[0].PrefixText != "Some text before" {
		t.Errorf("prefix = %q, want %q", callbackResults[0].PrefixText, "Some text before")
	}
	if callbackResults[0].Result.Content != "echo: cb" {
		t.Errorf("result = %q, want %q", callbackResults[0].Result.Content, "echo: cb")
	}
}

func TestChatCompletionWithToolCallback_ErrorAbort(t *testing.T) {
	toolCallJSON := "```json\n{\"tool\": \"echo\", \"arguments\": {\"text\": \"x\"}}\n```"
	mock := &mockProvider{
		responses: []*provider.ChatResponse{
			{Model: "test", Message: provider.Message{Role: "assistant", Content: toolCallJSON}},
		},
	}
	eng := New(newMockRouter(mock))
	eng.SetToolRegistry(newTestRegistry())

	abortErr := errors.New("user aborted")
	_, err := eng.ChatCompletionWithToolCallback(
		context.Background(),
		provider.ChatRequest{
			Model:    "test",
			Messages: []provider.Message{{Role: "user", Content: "hi"}},
		},
		func(ToolCallResult) error {
			return abortErr
		},
	)
	if !errors.Is(err, abortErr) {
		t.Errorf("got error %v, want %v", err, abortErr)
	}
}

func TestChatCompletionStream_Passthrough(t *testing.T) {
	mock := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{Delta: "hello ", Done: false},
				{Delta: "world", Done: true},
			},
		},
	}
	eng := New(newMockRouter(mock))

	ch, err := eng.ChatCompletionStream(context.Background(), provider.ChatRequest{
		Model:    "test",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []provider.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Delta != "hello " {
		t.Errorf("chunk 0 delta = %q, want %q", chunks[0].Delta, "hello ")
	}
	if !chunks[1].Done {
		t.Error("last chunk should be done")
	}
}

func TestListModels_Passthrough(t *testing.T) {
	mock := &mockProvider{
		models: []provider.ModelInfo{
			{ID: "model-a", Provider: "mock", OwnedBy: "test"},
			{ID: "model-b", Provider: "mock", OwnedBy: "test"},
		},
	}
	eng := New(newMockRouter(mock))

	models, err := eng.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].ID != "model-a" {
		t.Errorf("model 0 id = %q, want %q", models[0].ID, "model-a")
	}
}
