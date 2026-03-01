package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/jsoprych/guff/internal/config"
	"github.com/jsoprych/guff/internal/generate"
	"github.com/jsoprych/guff/internal/model"
)

type Server struct {
	router *chi.Mux
	model  *model.ModelManager
	config *config.Config
}

func NewServer(mm *model.ModelManager, cfg *config.Config) *Server {
	s := &Server{
		router: chi.NewRouter(),
		model:  mm,
		config: cfg,
	}
	s.setupRoutes()
	return s
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
		w.Write([]byte("guff API server"))
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Model management
		r.Get("/tags", s.handleListModels)
		r.Post("/pull", s.handlePullModel)

		// Generation
		r.Post("/generate", s.handleGenerate)
		r.Post("/chat", s.handleChat)
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
	json.NewEncoder(w).Encode(map[string]interface{}{"models": resp})
}

func (s *Server) handlePullModel(w http.ResponseWriter, r *http.Request) {
	// TODO: implement
	http.Error(w, "Not implemented", http.StatusNotImplemented)
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
		NumGpuLayers: 0,
		UseMmap:      true,
		UseMlock:     false,
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

	// Prepare generation options
	genOpts := generate.GenerationOptions{
		Temperature:      0.0,
		TopP:             0.95,
		TopK:             40,
		MaxTokens:        512,
		Stop:             []string{"\n"},
		Seed:             0,
		RepeatPenalty:    1.1,
		FrequencyPenalty: 0.0,
		PresencePenalty:  0.0,
		Mirostat:         0,
		Grammar:          "",
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
				data, _ := json.Marshal(event)
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
			data, _ := json.Marshal(resp)
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
	json.NewEncoder(w).Encode(resp)
}

// formatChatMessages converts chat messages into a prompt string.
// This is a simple implementation that prefixes each message with its role.
func formatChatMessages(messages []ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}

	var parts []string
	for _, msg := range messages {
		var prefix string
		switch msg.Role {
		case "system":
			prefix = "System: "
		case "assistant":
			prefix = "Assistant: "
		default:
			prefix = "User: "
		}
		parts = append(parts, prefix+msg.Content)
	}

	// Add a final "Assistant: " prompt if the last message is not from assistant
	lastRole := messages[len(messages)-1].Role
	if lastRole != "assistant" {
		parts = append(parts, "Assistant: ")
	}

	return strings.Join(parts, "\n\n")
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
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

	// Load model
	loadOpts := model.LoadOptions{
		NumGpuLayers: 0,
		UseMmap:      true,
		UseMlock:     false,
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

	// Format messages into prompt
	prompt := formatChatMessages(req.Messages)

	// Prepare generation options
	genOpts := generate.GenerationOptions{
		Temperature:      0.0,
		TopP:             0.95,
		TopK:             40,
		MaxTokens:        512,
		Stop:             []string{"\n"},
		Seed:             0,
		RepeatPenalty:    1.1,
		FrequencyPenalty: 0.0,
		PresencePenalty:  0.0,
		Mirostat:         0,
		Grammar:          "",
		Stream:           req.Stream,
	}

	// Override with request options if provided
	if req.Options != nil {
		if req.Options.Temperature != nil {
			genOpts.Temperature = *req.Options.Temperature
		}
		if req.Options.TopP != nil {
			genOpts.TopP = *req.Options.TopP
		}
		if req.Options.TopK != nil {
			genOpts.TopK = *req.Options.TopK
		}
		if req.Options.MaxTokens != nil {
			genOpts.MaxTokens = *req.Options.MaxTokens
		}
		if req.Options.Stop != nil {
			genOpts.Stop = req.Options.Stop
		}
		if req.Options.Seed != nil {
			genOpts.Seed = *req.Options.Seed
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

		// Start streaming generation
		ch := generator.GenerateStream(ctx, prompt, genOpts)
		var fullResponse strings.Builder

		for chunk := range ch {
			if chunk.Error != nil {
				event := map[string]interface{}{
					"error": chunk.Error.Error(),
				}
				data, _ := json.Marshal(event)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
				return
			}

			fullResponse.WriteString(chunk.Token)

			// Send progress event
			resp := ChatResponse{
				Model:     req.Model,
				CreatedAt: time.Now(),
				Message: ChatMessage{
					Role:    "assistant",
					Content: fullResponse.String(),
				},
				Done: chunk.Done,
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if chunk.Done {
				break
			}
		}
		return
	}

	// Non-streaming generation
	result, err := generator.Generate(ctx, prompt, genOpts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Prepare response
	resp := ChatResponse{
		Model:     req.Model,
		CreatedAt: time.Now(),
		Message: ChatMessage{
			Role:    "assistant",
			Content: result.Text,
		},
		Done:        true,
		TotalTokens: result.PromptTokens + result.GenTokens,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
