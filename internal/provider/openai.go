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

// OpenAIProvider proxies requests to an OpenAI-compatible API.
// Works with OpenAI, DeepSeek, Together, Groq, and any OpenAI-compatible endpoint.
type OpenAIProvider struct {
	name    string
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAIProvider creates a provider that calls the OpenAI API.
func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	return NewOpenAICompatibleProvider("openai", apiKey, baseURL, "https://api.openai.com/v1")
}

// NewDeepSeekProvider creates a provider for the DeepSeek API.
func NewDeepSeekProvider(apiKey, baseURL string) *OpenAIProvider {
	return NewOpenAICompatibleProvider("deepseek", apiKey, baseURL, "https://api.deepseek.com/v1")
}

// NewOpenAICompatibleProvider creates a provider for any OpenAI-compatible API.
func NewOpenAICompatibleProvider(name, apiKey, baseURL, defaultURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = defaultURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &OpenAIProvider{
		name:    name,
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string { return p.name }

// OpenAI request/response types (internal)

type openAIRequest struct {
	Model       string            `json:"model"`
	Messages    []openAIMessage   `json:"messages"`
	Temperature *float32          `json:"temperature,omitempty"`
	TopP        *float32          `json:"top_p,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Stop        []string          `json:"stop,omitempty"`
	Seed        *int              `json:"seed,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *OpenAIProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	oaiReq := p.toOpenAIRequest(req)
	oaiReq.Stream = false

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp openAIErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("openai error (%d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("openai error: status %d", resp.StatusCode)
	}

	var oaiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	content := ""
	if len(oaiResp.Choices) > 0 {
		content = oaiResp.Choices[0].Message.Content
	}

	return &ChatResponse{
		Model:        oaiResp.Model,
		Message:      Message{Role: "assistant", Content: content},
		PromptTokens: oaiResp.Usage.PromptTokens,
		GenTokens:    oaiResp.Usage.CompletionTokens,
		Done:         true,
	}, nil
}

func (p *OpenAIProvider) ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	oaiReq := p.toOpenAIRequest(req)
	oaiReq.Stream = true

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var errResp openAIErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("openai error (%d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("openai error: status %d", resp.StatusCode)
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
			if data == "[DONE]" {
				ch <- StreamChunk{Done: true}
				return
			}
			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta.Content
				done := chunk.Choices[0].FinishReason != nil
				ch <- StreamChunk{Delta: delta, Done: done}
				if done {
					return
				}
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			ch <- StreamChunk{Error: fmt.Errorf("stream read: %w", err)}
		}
	}()

	return ch, nil
}

func (p *OpenAIProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models: status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	infos := make([]ModelInfo, len(result.Data))
	for i, m := range result.Data {
		infos[i] = ModelInfo{
			ID:       m.ID,
			Provider: p.name,
			OwnedBy:  m.OwnedBy,
		}
	}
	return infos, nil
}

// openAIEmbeddingRequest is the request format for OpenAI's /v1/embeddings.
type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingResponse struct {
	Data  []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// Embed generates embeddings via the OpenAI-compatible /v1/embeddings endpoint.
func (p *OpenAIProvider) Embed(ctx context.Context, req EmbeddingRequest) ([]EmbeddingResult, error) {
	oaiReq := openAIEmbeddingRequest{
		Model: req.Model,
		Input: req.Input,
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp openAIErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("embedding error (%d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("embedding error: status %d", resp.StatusCode)
	}

	var oaiResp openAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	tokensPerInput := 0
	if len(req.Input) > 0 {
		tokensPerInput = oaiResp.Usage.PromptTokens / len(req.Input)
	}

	results := make([]EmbeddingResult, len(oaiResp.Data))
	for i, d := range oaiResp.Data {
		results[i] = EmbeddingResult{
			Embedding:  d.Embedding,
			Index:      d.Index,
			TokenCount: tokensPerInput,
		}
	}
	return results, nil
}

func (p *OpenAIProvider) toOpenAIRequest(req ChatRequest) openAIRequest {
	msgs := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openAIMessage{Role: m.Role, Content: m.Content}
	}

	oaiReq := openAIRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
		Stream:      req.Stream,
	}
	if req.Seed != nil {
		seed := int(*req.Seed)
		oaiReq.Seed = &seed
	}
	return oaiReq
}
