package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/jsoprych/guff/internal/api/ui"
	"github.com/jsoprych/guff/internal/config"
	"github.com/jsoprych/guff/internal/engine"
	"github.com/jsoprych/guff/internal/generate"
	"github.com/jsoprych/guff/internal/model"
	"github.com/jsoprych/guff/internal/provider"
)

type Server struct {
	router    *chi.Mux
	model     *model.ModelManager
	config    *config.Config
	engine    *engine.ChatEngine
	startedAt time.Time
}

func NewServer(mm *model.ModelManager, cfg *config.Config) *Server {
	s := &Server{
		router:    chi.NewRouter(),
		model:     mm,
		config:    cfg,
		startedAt: time.Now(),
	}
	s.setupRoutes()
	return s
}

// SetEngine sets the chat engine for all completion endpoints.
func (s *Server) SetEngine(e *engine.ChatEngine) {
	s.engine = e
}

func (s *Server) setupRoutes() {
	r := s.router

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health check
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("guff API server")); err != nil {
			log.Printf("write error: %v", err)
		}
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
			log.Printf("write error: %v", err)
		}
	})

	// Embedded chat UI
	uiContent, _ := fs.Sub(ui.Assets, ".")
	r.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := fs.ReadFile(uiContent, "index.html")
		if err != nil {
			http.Error(w, "UI not found", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Ollama-compatible API routes
	r.Route("/api", func(r chi.Router) {
		// Model management
		r.Get("/tags", s.handleListModels)
		r.Post("/pull", s.handlePullModel)

		// Generation
		r.Post("/generate", s.handleGenerate)
		r.Post("/chat", s.handleChat)

		// Dashboard endpoints
		r.Get("/status", s.handleStatus)
		r.Get("/tools", s.handleTools)
	})

	// OpenAI-compatible API routes
	r.Route("/v1", func(r chi.Router) {
		r.Post("/chat/completions", s.handleV1ChatCompletions)
		r.Post("/completions", s.handleV1Completions)
		r.Post("/embeddings", s.handleV1Embeddings)
		r.Get("/models", s.handleV1Models)
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Request/Response types

type GenerateRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	Stream      bool     `json:"stream"`
	Temperature *float32 `json:"temperature,omitempty"`
	TopP        *float32 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Stop        []string `json:"stop,omitempty"`
	Seed        *uint32  `json:"seed,omitempty"`
}

type GenerateResponse struct {
	Model       string    `json:"model"`
	CreatedAt   time.Time `json:"created_at"`
	Response    string    `json:"response"`
	Done        bool      `json:"done"`
	TotalTokens int       `json:"total_tokens,omitempty"`
}

type ChatRequest struct {
	Model    string           `json:"model"`
	Messages []ChatMessage    `json:"messages"`
	Stream   bool             `json:"stream"`
	Options  *GenerateRequest `json:"options,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	Model       string      `json:"model"`
	CreatedAt   time.Time   `json:"created_at"`
	Message     ChatMessage `json:"message"`
	Done        bool        `json:"done"`
	TotalTokens int         `json:"total_tokens,omitempty"`
}

// Handler stubs

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models := s.model.List()
	resp := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		resp = append(resp, map[string]interface{}{
			"name":   m.Name,
			"size":   m.Size,
			"digest": m.Digest,
		})
	}
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"models": resp}); err != nil {
		log.Printf("write error: %v", err)
	}
}

func (s *Server) handlePullModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		Quantization string `json:"quantization,omitempty"`
		Stream       *bool  `json:"stream,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "model name required", http.StatusBadRequest)
		return
	}
	quant := req.Quantization
	if quant == "" {
		quant = "q4_k_m"
	}

	stream := req.Stream == nil || *req.Stream // default true

	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, canFlush := w.(http.Flusher)

	writeJSON := func(v interface{}) {
		data, _ := json.Marshal(v)
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n"))
		if canFlush {
			flusher.Flush()
		}
	}

	opts := model.PullOptions{
		Quantization: quant,
	}
	if stream {
		opts.Progress = func(downloaded, total int64) {
			if total == 0 {
				total = downloaded
			}
			writeJSON(map[string]interface{}{
				"status":    "downloading",
				"completed": downloaded,
				"total":     total,
			})
		}
	}

	if err := s.model.Pull(r.Context(), req.Name, opts); err != nil {
		writeJSON(map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(map[string]interface{}{"status": "success"})
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		// Use default model
		models := s.model.List()
		if len(models) == 0 {
			http.Error(w, "No models available", http.StatusBadRequest)
			return
		}
		req.Model = models[0].Name
	}

	// Load model
	loadOpts := model.LoadOptions{
		NumGpuLayers: s.config.Model.NumGpuLayers,
		UseMmap:      s.config.Model.UseMmap,
		UseMlock:     s.config.Model.UseMlock,
	}
	loaded, err := s.model.Load(req.Model, loadOpts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load model: %v", err), http.StatusInternalServerError)
		return
	}
	defer s.model.Unload()

	// Create generator
	generator := generate.NewGenerator(loaded)
	defer generator.Close()

	// Prepare generation options from config defaults
	genOpts := generate.GenerationOptions{
		Temperature:      s.config.Generate.Temperature,
		TopP:             s.config.Generate.TopP,
		TopK:             s.config.Generate.TopK,
		MaxTokens:        s.config.Generate.MaxTokens,
		Stop:             []string{"\n"},
		Seed:             0,
		RepeatPenalty:    s.config.Generate.RepeatPenalty,
		FrequencyPenalty: 0.0,
		PresencePenalty:  0.0,
		Stream:           req.Stream,
	}

	// Override with request values
	if req.Temperature != nil {
		genOpts.Temperature = *req.Temperature
	}
	if req.TopP != nil {
		genOpts.TopP = *req.TopP
	}
	if req.TopK != nil {
		genOpts.TopK = *req.TopK
	}
	if req.MaxTokens != nil {
		genOpts.MaxTokens = *req.MaxTokens
	}
	if req.Stop != nil {
		genOpts.Stop = req.Stop
	}
	if req.Seed != nil {
		genOpts.Seed = *req.Seed
	}

	ctx := r.Context()

	if req.Stream {
		// Setup server-sent events
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Start streaming generation
		ch := generator.GenerateStream(ctx, req.Prompt, genOpts)

		for chunk := range ch {
			if chunk.Error != nil {
				// Send error as JSON event
				event := map[string]interface{}{
					"error": chunk.Error.Error(),
				}
				data, err := json.Marshal(event)
				if err != nil {
					log.Printf("json marshal error: %v", err)
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				return
			}

			// Send progress event
			resp := GenerateResponse{
				Model:     req.Model,
				CreatedAt: time.Now(),
				Response:  chunk.Text,
				Done:      chunk.Done,
			}
			data, err := json.Marshal(resp)
			if err != nil {
				log.Printf("json marshal error: %v", err)
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if chunk.Done {
				break
			}
		}
		return
	}

	// Non-streaming generation
	result, err := generator.Generate(ctx, req.Prompt, genOpts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Prepare response
	resp := GenerateResponse{
		Model:       req.Model,
		CreatedAt:   time.Now(),
		Response:    result.Text,
		Done:        true,
		TotalTokens: result.PromptTokens + result.GenTokens,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("write error: %v", err)
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		http.Error(w, "Chat engine not configured", http.StatusInternalServerError)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		// Use default model
		models := s.model.List()
		if len(models) == 0 {
			http.Error(w, "No models available", http.StatusBadRequest)
			return
		}
		req.Model = models[0].Name
	}

	// Convert to provider messages
	msgs := make([]provider.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = provider.Message{Role: m.Role, Content: m.Content}
	}

	// Build provider request with config defaults
	maxTokens := s.config.Generate.MaxTokens
	temp := s.config.Generate.Temperature
	topP := s.config.Generate.TopP

	// Override with request options if provided
	if req.Options != nil {
		if req.Options.Temperature != nil {
			temp = *req.Options.Temperature
		}
		if req.Options.TopP != nil {
			topP = *req.Options.TopP
		}
		if req.Options.MaxTokens != nil {
			maxTokens = *req.Options.MaxTokens
		}
	}

	provReq := provider.ChatRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   maxTokens,
		Stop:        []string{"\n"},
		Stream:      req.Stream,
	}

	// Override additional options
	if req.Options != nil {
		if req.Options.Stop != nil {
			provReq.Stop = req.Options.Stop
		}
		if req.Options.Seed != nil {
			provReq.Seed = req.Options.Seed
		}
	}

	ctx := r.Context()

	if req.Stream {
		// Setup server-sent events
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		ch, err := s.engine.ChatCompletionStream(ctx, provReq)
		if err != nil {
			event := map[string]interface{}{"error": err.Error()}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}

		var fullResponse strings.Builder
		for chunk := range ch {
			if chunk.Error != nil {
				event := map[string]interface{}{"error": chunk.Error.Error()}
				data, _ := json.Marshal(event)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				return
			}

			fullResponse.WriteString(chunk.Delta)

			resp := ChatResponse{
				Model:     req.Model,
				CreatedAt: time.Now(),
				Message: ChatMessage{
					Role:    "assistant",
					Content: fullResponse.String(),
				},
				Done: chunk.Done,
			}
			data, err := json.Marshal(resp)
			if err != nil {
				log.Printf("json marshal error: %v", err)
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if chunk.Done {
				break
			}
		}
		return
	}

	// Non-streaming: use engine with tool loop
	resp, err := s.engine.ChatCompletion(ctx, provReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	chatResp := ChatResponse{
		Model:     req.Model,
		CreatedAt: time.Now(),
		Message: ChatMessage{
			Role:    "assistant",
			Content: strings.TrimSpace(resp.Message.Content),
		},
		Done:        true,
		TotalTokens: resp.PromptTokens + resp.GenTokens,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(chatResp); err != nil {
		log.Printf("write error: %v", err)
	}
}

// handleStatus returns server status for the dashboard.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"uptime": time.Since(s.startedAt).Truncate(time.Second).String(),
	}

	// Current model info
	models := s.model.List()
	if len(models) > 0 {
		status["model"] = models[0].Name
	}

	// Tool count
	toolCount := 0
	if s.engine != nil {
		if reg := s.engine.ToolRegistry(); reg != nil {
			toolCount = len(reg.List())
		}
	}
	status["tools_count"] = toolCount

	// Provider count
	providerCount := 0
	if s.config != nil {
		providerCount = len(s.config.Providers)
	}
	status["providers"] = providerCount

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.Printf("write error: %v", err)
	}
}

// handleTools returns tool definitions for the dashboard.
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	var toolList []toolInfo
	if s.engine != nil {
		if reg := s.engine.ToolRegistry(); reg != nil {
			for _, td := range reg.List() {
				toolList = append(toolList, toolInfo{
					Name:        td.Name,
					Description: td.Description,
				})
			}
		}
	}

	if toolList == nil {
		toolList = []toolInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"tools": toolList}); err != nil {
		log.Printf("write error: %v", err)
	}
}
