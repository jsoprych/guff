package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnthropicProvider proxies requests to the Anthropic Messages API,
// translating between the unified provider format and Anthropic's format.
type AnthropicProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewAnthropicProvider creates a provider that calls the Anthropic Messages API.
func NewAnthropicProvider(apiKey, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

// Anthropic request/response types (internal)

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature *float32           `json:"temperature,omitempty"`
	TopP        *float32           `json:"top_p,omitempty"`
	TopK        *int               `json:"top_k,omitempty"`
	StopSeqs    []string           `json:"stop_sequences,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicStreamEvent struct {
	Type  string          `json:"type"`
	Delta json.RawMessage `json:"delta,omitempty"`
	Index int             `json:"index,omitempty"`
}

type anthropicContentDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *AnthropicProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	antReq := p.toAnthropicRequest(req)
	antReq.Stream = false

	body, err := json.Marshal(antReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp anthropicErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("anthropic error (%d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic error: status %d", resp.StatusCode)
	}

	var antResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&antResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	content := ""
	for _, block := range antResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &ChatResponse{
		Model:        antResp.Model,
		Message:      Message{Role: "assistant", Content: content},
		PromptTokens: antResp.Usage.InputTokens,
		GenTokens:    antResp.Usage.OutputTokens,
		Done:         true,
	}, nil
}

func (p *AnthropicProvider) ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	antReq := p.toAnthropicRequest(req)
	antReq.Stream = true

	body, err := json.Marshal(antReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var errResp anthropicErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("anthropic error (%d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic error: status %d", resp.StatusCode)
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var event anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			switch event.Type {
			case "content_block_delta":
				var delta anthropicContentDelta
				if err := json.Unmarshal(event.Delta, &delta); err == nil {
					ch <- StreamChunk{Delta: delta.Text}
				}
			case "message_stop":
				ch <- StreamChunk{Done: true}
				return
			case "message_delta":
				// Final delta with stop reason
				ch <- StreamChunk{Done: true}
				return
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			ch <- StreamChunk{Error: fmt.Errorf("stream read: %w", err)}
		}
	}()

	return ch, nil
}

func (p *AnthropicProvider) ListModels(_ context.Context) ([]ModelInfo, error) {
	// Anthropic doesn't have a list models endpoint; return known models.
	return []ModelInfo{
		{ID: "claude-opus-4-6", Provider: "anthropic", OwnedBy: "anthropic"},
		{ID: "claude-sonnet-4-5-20250929", Provider: "anthropic", OwnedBy: "anthropic"},
		{ID: "claude-haiku-4-5-20251001", Provider: "anthropic", OwnedBy: "anthropic"},
	}, nil
}

func (p *AnthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}

// toAnthropicRequest translates the unified format to Anthropic's format.
// Key difference: system messages are extracted into the top-level "system" field.
func (p *AnthropicProvider) toAnthropicRequest(req ChatRequest) anthropicRequest {
	var systemParts []string
	var msgs []anthropicMessage

	for _, m := range req.Messages {
		if m.Role == "system" {
			systemParts = append(systemParts, m.Content)
			continue
		}
		// Anthropic only accepts "user" and "assistant" roles
		role := m.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}
		msgs = append(msgs, anthropicMessage{Role: role, Content: m.Content})
	}

	// Anthropic requires messages to start with a user message
	if len(msgs) == 0 || msgs[0].Role != "user" {
		msgs = append([]anthropicMessage{{Role: "user", Content: "Hello"}}, msgs...)
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1024
	}

	return anthropicRequest{
		Model:       req.Model,
		MaxTokens:   maxTokens,
		System:      strings.Join(systemParts, "\n\n"),
		Messages:    msgs,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
		StopSeqs:    req.Stop,
		Stream:      req.Stream,
	}
}
