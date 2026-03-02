package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jsoprych/guff/internal/generate"
	"github.com/jsoprych/guff/internal/model"
	"github.com/jsoprych/guff/internal/provider"
)

// OpenAI-compatible request/response types

type OAIChatRequest struct {
	Model            string         `json:"model"`
	Messages         []OAIMessage   `json:"messages"`
	Temperature      *float32       `json:"temperature,omitempty"`
	TopP             *float32       `json:"top_p,omitempty"`
	TopK             *int           `json:"top_k,omitempty"`
	MinP             *float32       `json:"min_p,omitempty"`
	MaxTokens        *int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int        `json:"max_completion_tokens,omitempty"`
	Stop             interface{}    `json:"stop,omitempty"` // string or []string
	Stream           bool           `json:"stream,omitempty"`
	Seed             *int           `json:"seed,omitempty"`
	N                int            `json:"n,omitempty"`

	// Extended sampler parameters (passed through to local provider)
	Grammar       string                `json:"grammar,omitempty"`
	LogitBias     map[string]float32    `json:"logit_bias,omitempty"` // OAI uses string keys
	TypicalP      *float32              `json:"typical_p,omitempty"`
	TopNSigma     *float32              `json:"top_n_sigma,omitempty"`
	DryMultiplier *float32              `json:"dry_multiplier,omitempty"`
}

type OAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OAIChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []OAIChoice  `json:"choices"`
	Usage   OAIUsage     `json:"usage"`
}

type OAIChoice struct {
	Index        int        `json:"index"`
	Message      OAIMessage `json:"message,omitempty"`
	Delta        *OAIMessage `json:"delta,omitempty"`
	FinishReason *string    `json:"finish_reason"`
}

type OAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OAIModelList struct {
	Object string       `json:"object"`
	Data   []OAIModel   `json:"data"`
}

type OAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type OAICompletionRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Temperature *float32 `json:"temperature,omitempty"`
	TopP        *float32 `json:"top_p,omitempty"`
	Stop        interface{} `json:"stop,omitempty"`
	Stream      bool     `json:"stream,omitempty"`
	Seed        *int     `json:"seed,omitempty"`
}

type OAICompletionResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []OAICompletionChoice `json:"choices"`
	Usage   OAIUsage            `json:"usage"`
}

type OAICompletionChoice struct {
	Index        int     `json:"index"`
	Text         string  `json:"text"`
	FinishReason *string `json:"finish_reason"`
}

type OAIErrorResponse struct {
	Error OAIError `json:"error"`
}

type OAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// parseStop handles the stop field which can be string or []string.
func parseStop(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []interface{}:
		var stops []string
		for _, s := range v {
			if str, ok := s.(string); ok {
				stops = append(stops, str)
			}
		}
		return stops
	}
	return nil
}

// generateID returns a simple unique ID for responses.
func generateID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func oaiError(w http.ResponseWriter, status int, msg, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(OAIErrorResponse{
		Error: OAIError{Message: msg, Type: errType},
	})
}

// handleV1ChatCompletions handles POST /v1/chat/completions
func (s *Server) handleV1ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		oaiError(w, http.StatusInternalServerError, "chat engine not configured", "server_error")
		return
	}

	var req OAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oaiError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err), "invalid_request_error")
		return
	}

	if req.Model == "" {
		oaiError(w, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}

	// Convert to provider request
	msgs := make([]provider.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = provider.Message{Role: m.Role, Content: m.Content}
	}

	maxTokens := 1024
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	} else if req.MaxCompletionTokens != nil {
		maxTokens = *req.MaxCompletionTokens
	}

	var seed *uint32
	if req.Seed != nil {
		s := uint32(*req.Seed)
		seed = &s
	}

	// Convert OAI logit_bias (string keys) to provider format (int32 keys)
	var logitBias map[int32]float32
	if len(req.LogitBias) > 0 {
		logitBias = make(map[int32]float32, len(req.LogitBias))
		for k, v := range req.LogitBias {
			var tokenID int32
			if _, err := fmt.Sscanf(k, "%d", &tokenID); err == nil {
				logitBias[tokenID] = v
			}
		}
	}

	provReq := provider.ChatRequest{
		Model:         req.Model,
		Messages:      msgs,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		TopK:          req.TopK,
		MinP:          req.MinP,
		MaxTokens:     maxTokens,
		Stop:          parseStop(req.Stop),
		Seed:          seed,
		Stream:        req.Stream,
		Grammar:       req.Grammar,
		LogitBias:     logitBias,
		TypicalP:      req.TypicalP,
		TopNSigma:     req.TopNSigma,
		DryMultiplier: req.DryMultiplier,
	}

	ctx := r.Context()

	if req.Stream {
		s.handleV1ChatCompletionsStream(w, ctx, req.Model, provReq)
		return
	}

	// Non-streaming — engine handles tool loop
	resp, err := s.engine.ChatCompletion(ctx, provReq)
	if err != nil {
		oaiError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}

	finishReason := "stop"
	oaiResp := OAIChatResponse{
		ID:      generateID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   resp.Model,
		Choices: []OAIChoice{
			{
				Index:        0,
				Message:      OAIMessage{Role: "assistant", Content: resp.Message.Content},
				FinishReason: &finishReason,
			},
		},
		Usage: OAIUsage{
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.GenTokens,
			TotalTokens:      resp.PromptTokens + resp.GenTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(oaiResp); err != nil {
		log.Printf("write error: %v", err)
	}
}

func (s *Server) handleV1ChatCompletionsStream(w http.ResponseWriter, ctx context.Context, model string, req provider.ChatRequest) {
	ch, err := s.engine.ChatCompletionStream(ctx, req)
	if err != nil {
		oaiError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		oaiError(w, http.StatusInternalServerError, "streaming not supported", "server_error")
		return
	}

	id := generateID("chatcmpl")

	for chunk := range ch {
		if chunk.Error != nil {
			errData, _ := json.Marshal(OAIErrorResponse{
				Error: OAIError{Message: chunk.Error.Error(), Type: "server_error"},
			})
			fmt.Fprintf(w, "data: %s\n\n", errData)
			flusher.Flush()
			return
		}

		var finishReason *string
		if chunk.Done {
			fr := "stop"
			finishReason = &fr
		}

		oaiChunk := OAIChatResponse{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []OAIChoice{
				{
					Index:        0,
					Delta:        &OAIMessage{Role: "assistant", Content: chunk.Delta},
					FinishReason: finishReason,
				},
			},
		}

		data, err := json.Marshal(oaiChunk)
		if err != nil {
			log.Printf("json marshal error: %v", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		if chunk.Done {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}
}

// handleV1Models handles GET /v1/models
func (s *Server) handleV1Models(w http.ResponseWriter, r *http.Request) {
	var models []OAIModel

	if s.engine != nil {
		infos, err := s.engine.ListModels(r.Context())
		if err == nil {
			for _, m := range infos {
				models = append(models, OAIModel{
					ID:      m.ID,
					Object:  "model",
					Created: time.Now().Unix(),
					OwnedBy: m.OwnedBy,
				})
			}
		}
	}

	// Also include local models from the model manager
	if s.model != nil {
		for _, m := range s.model.List() {
			// Avoid duplicates
			found := false
			for _, existing := range models {
				if existing.ID == m.Name {
					found = true
					break
				}
			}
			if !found {
				models = append(models, OAIModel{
					ID:      m.Name,
					Object:  "model",
					Created: time.Now().Unix(),
					OwnedBy: "local",
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(OAIModelList{Object: "list", Data: models}); err != nil {
		log.Printf("write error: %v", err)
	}
}

// handleV1Completions handles POST /v1/completions (legacy completions endpoint)
func (s *Server) handleV1Completions(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		oaiError(w, http.StatusInternalServerError, "chat engine not configured", "server_error")
		return
	}

	var req OAICompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oaiError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err), "invalid_request_error")
		return
	}

	if req.Model == "" {
		oaiError(w, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}

	// Convert to a chat request with the prompt as a user message
	maxTokens := 1024
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	var seed *uint32
	if req.Seed != nil {
		s := uint32(*req.Seed)
		seed = &s
	}

	provReq := provider.ChatRequest{
		Model:       req.Model,
		Messages:    []provider.Message{{Role: "user", Content: req.Prompt}},
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   maxTokens,
		Stop:        parseStop(req.Stop),
		Seed:        seed,
	}

	ctx := r.Context()

	if req.Stream {
		// Streaming completions
		ch, err := s.engine.ChatCompletionStream(ctx, provReq)
		if err != nil {
			oaiError(w, http.StatusInternalServerError, err.Error(), "server_error")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			oaiError(w, http.StatusInternalServerError, "streaming not supported", "server_error")
			return
		}

		id := generateID("cmpl")
		for chunk := range ch {
			if chunk.Error != nil {
				break
			}
			var finishReason *string
			if chunk.Done {
				fr := "stop"
				finishReason = &fr
			}
			resp := OAICompletionResponse{
				ID:      id,
				Object:  "text_completion",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []OAICompletionChoice{
					{Index: 0, Text: chunk.Delta, FinishReason: finishReason},
				},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if chunk.Done {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
		}
		return
	}

	// Non-streaming
	resp, err := s.engine.ChatCompletion(ctx, provReq)
	if err != nil {
		oaiError(w, http.StatusInternalServerError, err.Error(), "server_error")
		return
	}

	finishReason := "stop"
	oaiResp := OAICompletionResponse{
		ID:      generateID("cmpl"),
		Object:  "text_completion",
		Created: time.Now().Unix(),
		Model:   resp.Model,
		Choices: []OAICompletionChoice{
			{
				Index:        0,
				Text:         resp.Message.Content,
				FinishReason: &finishReason,
			},
		},
		Usage: OAIUsage{
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.GenTokens,
			TotalTokens:      resp.PromptTokens + resp.GenTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(oaiResp); err != nil {
		log.Printf("write error: %v", err)
	}
}

// OpenAI embeddings types

type OAIEmbeddingRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"` // string or []string
}

type OAIEmbeddingResponse struct {
	Object string         `json:"object"`
	Data   []OAIEmbedding `json:"data"`
	Model  string         `json:"model"`
	Usage  OAIUsage       `json:"usage"`
}

type OAIEmbedding struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// handleV1Embeddings handles POST /v1/embeddings
func (s *Server) handleV1Embeddings(w http.ResponseWriter, r *http.Request) {
	var req OAIEmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oaiError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err), "invalid_request_error")
		return
	}

	if req.Model == "" {
		oaiError(w, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}

	// Parse input — can be string or []string
	var inputs []string
	switch v := req.Input.(type) {
	case string:
		inputs = []string{v}
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				inputs = append(inputs, str)
			}
		}
	default:
		oaiError(w, http.StatusBadRequest, "input must be string or array of strings", "invalid_request_error")
		return
	}

	if len(inputs) == 0 {
		oaiError(w, http.StatusBadRequest, "input is empty", "invalid_request_error")
		return
	}

	ctx := r.Context()

	// Try remote provider first (e.g., openai/text-embedding-3-small)
	if s.engine != nil {
		provReq := provider.EmbeddingRequest{Model: req.Model, Input: inputs}
		results, err := s.engine.Embed(ctx, provReq)
		if err == nil {
			var embeddings []OAIEmbedding
			totalTokens := 0
			for _, res := range results {
				embeddings = append(embeddings, OAIEmbedding{
					Object:    "embedding",
					Embedding: res.Embedding,
					Index:     res.Index,
				})
				totalTokens += res.TokenCount
			}
			embResp := OAIEmbeddingResponse{
				Object: "list",
				Data:   embeddings,
				Model:  req.Model,
				Usage: OAIUsage{
					PromptTokens: totalTokens,
					TotalTokens:  totalTokens,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(embResp); err != nil {
				log.Printf("write error: %v", err)
			}
			return
		}
		// Fall through to local model if remote provider doesn't support embeddings
	}

	// Local model embeddings
	loadOpts := model.LoadOptions{
		NumGpuLayers: s.config.Model.NumGpuLayers,
		UseMmap:      s.config.Model.UseMmap,
		UseMlock:     s.config.Model.UseMlock,
	}
	loaded, err := s.model.Load(req.Model, loadOpts)
	if err != nil {
		oaiError(w, http.StatusInternalServerError, fmt.Sprintf("failed to load model: %v", err), "server_error")
		return
	}
	defer s.model.Unload()

	gen := generate.NewGenerator(loaded)
	defer gen.Close()

	var embeddings []OAIEmbedding
	totalTokens := 0

	for i, input := range inputs {
		result, err := gen.Embed(ctx, input)
		if err != nil {
			oaiError(w, http.StatusInternalServerError, fmt.Sprintf("embedding failed: %v", err), "server_error")
			return
		}
		embeddings = append(embeddings, OAIEmbedding{
			Object:    "embedding",
			Embedding: result.Embedding,
			Index:     i,
		})
		totalTokens += result.TokenCount
	}

	embResp := OAIEmbeddingResponse{
		Object: "list",
		Data:   embeddings,
		Model:  req.Model,
		Usage: OAIUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(embResp); err != nil {
		log.Printf("write error: %v", err)
	}
}
