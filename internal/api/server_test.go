package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jsoprych/guff/internal/config"
	"github.com/jsoprych/guff/internal/engine"
	"github.com/jsoprych/guff/internal/model"
	"github.com/jsoprych/guff/internal/provider"
)

// mockProvider implements provider.Provider for API tests.
type mockProvider struct {
	responses []*provider.ChatResponse
	streams   [][]provider.StreamChunk
	models    []provider.ModelInfo
	callCount int
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) ChatCompletion(_ context.Context, _ provider.ChatRequest) (*provider.ChatResponse, error) {
	idx := m.callCount
	m.callCount++
	if idx >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}
	return m.responses[idx], nil
}

func (m *mockProvider) ChatCompletionStream(_ context.Context, _ provider.ChatRequest) (<-chan provider.StreamChunk, error) {
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

// newTestServer creates a Server with a mock engine for testing.
func newTestServer(mock *mockProvider) *Server {
	cfg := &config.Config{}
	cfg.Generate.Temperature = 0.8
	cfg.Generate.TopP = 0.9
	cfg.Generate.TopK = 40
	cfg.Generate.MaxTokens = 2048
	cfg.Generate.RepeatPenalty = 1.1

	mm := model.NewManager(cfg)
	s := NewServer(mm, cfg)

	router := provider.NewRouter()
	router.RegisterProvider(mock)
	router.SetFallback(mock)
	eng := engine.New(router)
	s.SetEngine(eng)

	return s
}

func TestHealthEndpoint(t *testing.T) {
	s := newTestServer(&mockProvider{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestHandleChat_NonStreaming(t *testing.T) {
	mock := &mockProvider{
		responses: []*provider.ChatResponse{
			{Model: "test", Message: provider.Message{Role: "assistant", Content: "hi there"}, Done: true},
		},
	}
	s := newTestServer(mock)

	body, _ := json.Marshal(ChatRequest{
		Model:    "test",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
		Stream:   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if chatResp.Message.Content != "hi there" {
		t.Errorf("content = %q, want %q", chatResp.Message.Content, "hi there")
	}
	if !chatResp.Done {
		t.Error("expected done=true")
	}
}

func TestHandleChat_NoEngine(t *testing.T) {
	cfg := &config.Config{}
	mm := model.NewManager(cfg)
	s := NewServer(mm, cfg)
	// Don't set engine

	body, _ := json.Marshal(ChatRequest{
		Model:    "test",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "Chat engine not configured") {
		t.Errorf("body = %q, want 'Chat engine not configured'", string(respBody))
	}
}

func TestHandleV1ChatCompletions(t *testing.T) {
	mock := &mockProvider{
		responses: []*provider.ChatResponse{
			{
				Model:        "test-model",
				Message:      provider.Message{Role: "assistant", Content: "response text"},
				PromptTokens: 5,
				GenTokens:    3,
				Done:         true,
			},
		},
	}
	s := newTestServer(mock)

	body, _ := json.Marshal(OAIChatRequest{
		Model:    "test-model",
		Messages: []OAIMessage{{Role: "user", Content: "hello"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var oaiResp OAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if oaiResp.Object != "chat.completion" {
		t.Errorf("object = %q, want %q", oaiResp.Object, "chat.completion")
	}
	if len(oaiResp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(oaiResp.Choices))
	}
	if oaiResp.Choices[0].Message.Content != "response text" {
		t.Errorf("content = %q, want %q", oaiResp.Choices[0].Message.Content, "response text")
	}
	if oaiResp.Usage.PromptTokens != 5 {
		t.Errorf("prompt_tokens = %d, want 5", oaiResp.Usage.PromptTokens)
	}
	if oaiResp.Usage.CompletionTokens != 3 {
		t.Errorf("completion_tokens = %d, want 3", oaiResp.Usage.CompletionTokens)
	}
}

func TestHandleV1ChatCompletions_Stream(t *testing.T) {
	mock := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{Delta: "hello ", Done: false},
				{Delta: "world", Done: true},
			},
		},
	}
	s := newTestServer(mock)

	body, _ := json.Marshal(OAIChatRequest{
		Model:    "test-model",
		Messages: []OAIMessage{{Role: "user", Content: "hello"}},
		Stream:   true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	respBody, _ := io.ReadAll(resp.Body)
	lines := string(respBody)

	// Should have data: lines and a [DONE] marker
	if !strings.Contains(lines, "data: ") {
		t.Error("expected SSE data: lines")
	}
	if !strings.Contains(lines, "data: [DONE]") {
		t.Error("expected [DONE] marker")
	}

	// Parse the first SSE data line
	for _, line := range strings.Split(lines, "\n") {
		if strings.HasPrefix(line, "data: {") {
			var chunk OAIChatResponse
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
				t.Fatalf("parse SSE chunk: %v", err)
			}
			if chunk.Object != "chat.completion.chunk" {
				t.Errorf("object = %q, want %q", chunk.Object, "chat.completion.chunk")
			}
			break
		}
	}
}

func TestHandleV1Models(t *testing.T) {
	mock := &mockProvider{
		models: []provider.ModelInfo{
			{ID: "model-a", Provider: "mock", OwnedBy: "test"},
			{ID: "model-b", Provider: "mock", OwnedBy: "test"},
		},
	}
	s := newTestServer(mock)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var modelList OAIModelList
	if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if modelList.Object != "list" {
		t.Errorf("object = %q, want %q", modelList.Object, "list")
	}
	if len(modelList.Data) < 2 {
		t.Errorf("models = %d, want >= 2", len(modelList.Data))
	}
}

func TestHandleV1Completions(t *testing.T) {
	mock := &mockProvider{
		responses: []*provider.ChatResponse{
			{
				Model:        "test-model",
				Message:      provider.Message{Role: "assistant", Content: "completed text"},
				PromptTokens: 4,
				GenTokens:    2,
				Done:         true,
			},
		},
	}
	s := newTestServer(mock)

	body, _ := json.Marshal(OAICompletionRequest{
		Model:  "test-model",
		Prompt: "Once upon a time",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var compResp OAICompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&compResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if compResp.Object != "text_completion" {
		t.Errorf("object = %q, want %q", compResp.Object, "text_completion")
	}
	if len(compResp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(compResp.Choices))
	}
	if compResp.Choices[0].Text != "completed text" {
		t.Errorf("text = %q, want %q", compResp.Choices[0].Text, "completed text")
	}
}

func TestHandleUI(t *testing.T) {
	s := newTestServer(&mockProvider{})

	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<title>guff</title>") {
		t.Error("expected HTML to contain <title>guff</title>")
	}
}

func TestHandleStatus(t *testing.T) {
	s := newTestServer(&mockProvider{})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if _, ok := status["uptime"]; !ok {
		t.Error("expected uptime in status")
	}
	if _, ok := status["tools_count"]; !ok {
		t.Error("expected tools_count in status")
	}
}

func TestHandleTools(t *testing.T) {
	s := newTestServer(&mockProvider{})

	req := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	tools, ok := body["tools"]
	if !ok {
		t.Error("expected tools key in response")
	}
	toolList, ok := tools.([]interface{})
	if !ok {
		t.Fatal("expected tools to be an array")
	}
	// No tools registered in test setup
	if len(toolList) != 0 {
		t.Errorf("expected 0 tools, got %d", len(toolList))
	}
}
