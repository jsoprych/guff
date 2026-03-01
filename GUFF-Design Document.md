Excellent idea! Let's create a comprehensive design doc optimized for AI agent consumption. I'll structure this for maximum clarity and actionability, with the goal of producing production-ready code.

## Project Name Suggestions

- **`gguf-run`** (simple, descriptive)
- **`granite-run`** (if focusing on IBM models initially)
- **`llamactl`** (ollama-like but different)
- **`guff`** (playful, memorable)
- **`cobblestone`** (granite + local = cobblestone?)

Let's go with **`gguf-run`** for maximum flexibility (any GGUF model, not just Granite).

---

# GGUF-Run Design Document

## A Local LLM Runtime in Go with llama.cpp Backend

**Version**: 1.0  
**Date**: February 27, 2026  
**Author**: AI-Assisted Design  
**Target AI Agents**: OpenCode, DeepSeek Coder, Claude, GPT-4

---

## 1. Executive Summary

`gguf-run` is a single-binary CLI tool and API server for running GGUF-format LLM models locally, with special optimizations for IBM Granite models. It provides Ollama-like functionality with a focus on simplicity, performance, and extensibility.

### Core Value Proposition

- **Single executable** (< 30MB) that can run any GGUF model
- **Zero-config** operation with sensible defaults
- **Full feature parity** with Ollama's best features
- **Granite-optimized** with thinking mode support
- **MCP-ready** architecture for Phase 2

---

## 2. System Architecture

### 2.1 High-Level Component Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                         gguf-run Binary                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌─────────────────┐    ┌─────────────────────────────────────┐ │
│  │   CLI Layer     │    │          API Layer (Phase 2)        │ │
│  │  - cobra CLI    │    │  - HTTP Server                      │ │
│  │  - Interactive  │    │  - OpenAI Compatible                │ │
│  │  - Color output │    │  - MCP Protocol                     │ │
│  └────────┬────────┘    └────────────────┬────────────────────┘ │
│           │                               │                      │
│           └───────────────┬───────────────┘                      │
│                           │                                       │
│  ┌────────────────────────▼────────────────────────────────────┐ │
│  │                    Core Services Layer                       │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │ │
│  │  │   Model      │  │   Prompt     │  │    Tool          │   │ │
│  │  │   Manager    │  │   Manager    │  │   Executor       │   │ │
│  │  └──────────────┘  └──────────────┘  └──────────────────┘   │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                           │                                       │
│  ┌────────────────────────▼────────────────────────────────────┐ │
│  │                 llama.cpp Go Bindings                        │ │
│  │  (github.com/ollama/ollama/llama or go-llama.cpp)           │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                           │                                       │
│  ┌────────────────────────▼────────────────────────────────────┐ │
│  │                    GGUF Model Files                          │ │
│  │  ~/.local/share/gguf-run/models/                            │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 Technology Stack

| Component     | Choice           | Rationale                                            |
| ------------- | ---------------- | ---------------------------------------------------- |
| Language      | Go 1.24+         | Single binary, excellent concurrency, great for CLIs |
| CLI Framework | Cobra + Viper    | Industry standard, config management                 |
| LLM Backend   | go-llama.cpp     | Pure Go bindings, active maintenance                 |
| Model Format  | GGUF             | Universal, self-describing, quantized                |
| Config Format | YAML + JSON      | Human-readable, API-friendly                         |
| Storage       | Local filesystem | No DB required initially                             |
| API Server    | net/http + chi   | Lightweight, performant                              |

---

## 3. Directory Structure & File Organization

```
gguf-run/
├── cmd/
│   ├── root.go              # Root command
│   ├── pull.go              # Model download
│   ├── run.go                # Run model with prompt
│   ├── chat.go               # Interactive chat
│   ├── create.go             # Create from Modelfile
│   ├── list.go               # List models
│   ├── serve.go              # API server (Phase 2)
│   └── completion.go         # Shell completion
├── internal/
│   ├── model/
│   │   ├── manager.go        # Model lifecycle
│   │   ├── loader.go         # GGUF loading
│   │   ├── config.go         # Model config
│   │   └── granite.go        # Granite-specific
│   ├── prompt/
│   │   ├── manager.go        # Prompt history
│   │   ├── template.go       # Template engine
│   │   └── context.go        # Context window
│   ├── generate/
│   │   ├── generate.go       # Text generation
│   │   ├── streaming.go      # Streaming responses
│   │   ├── grammar.go        # Structured output
│   │   └── embedding.go      # Embeddings support
│   ├── tools/
│   │   ├── executor.go       # Tool execution
│   │   ├── registry.go       # Tool registry
│   │   └── mcp.go            # MCP client (Phase 2)
│   ├── api/
│   │   ├── server.go         # HTTP server (Phase 2)
│   │   ├── handlers.go       # Route handlers
│   │   ├── middleware.go     # Auth, logging
│   │   └── openai.go         # OpenAI compatibility
│   └── config/
│       ├── config.go         # Config management
│       └── paths.go          # XDG paths
├── pkg/
│   ├── gguf/
│   │   └── metadata.go       # GGUF parsing helpers
│   └── modelfile/
│       ├── parser.go         # Modelfile parsing
│       └── schema.go         # Modelfile schema
├── scripts/
│   ├── build.sh              # Build script
│   ├── test.sh               # Test runner
│   └── install.sh            # Install helper
├── docs/
│   ├── api.md                # API documentation
│   ├── modelfile.md          # Modelfile reference
│   └── examples/             # Example usage
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## 4. Core Components Detailed Design

### 4.1 Model Manager

**File**: `internal/model/manager.go`

```go
package model

import (
    "context"
    "sync"
    "time"

    "github.com/gguf-run/internal/config"
    llama "github.com/ollama/ollama/llama" // or alternative binding
)

// ModelManager handles model lifecycle
type ModelManager struct {
    mu           sync.RWMutex
    modelsDir    string
    current      *LoadedModel
    registry     map[string]*ModelInfo
    downloader   *Downloader
    config       *config.Config
}

// LoadedModel represents an actively loaded model
type LoadedModel struct {
    Model     *llama.Model
    Context   *llama.Context
    Batch     *llama.Batch
    Info      *ModelInfo
    LoadedAt  time.Time
    LastUsed  time.Time
    mu        sync.Mutex
}

// ModelInfo contains metadata about a model
type ModelInfo struct {
    Name        string            `json:"name"`
    Path        string            `json:"path"`
    Size        int64             `json:"size"`
    Quantization string           `json:"quantization"`
    Architecture string           `json:"architecture"` // granite, llama, etc.
    ContextLen  int               `json:"context_length"`
    Modified    time.Time         `json:"modified"`
    Digest      string            `json:"digest"`       // SHA256 of file
    Config      *ModelConfig      `json:"config,omitempty"`
}

// ModelConfig from Modelfile
type ModelConfig struct {
    From        string                 `yaml:"from"`
    Parameters  map[string]interface{} `yaml:"parameters"`
    System      string                 `yaml:"system"`
    Template    string                 `yaml:"template"`
    Adapters    []string               `yaml:"adapters"`
    License     string                 `yaml:"license"`
    Messages    []Message              `yaml:"messages"`
}

// Key methods
func (m *ModelManager) List() []*ModelInfo
func (m *ModelManager) Get(name string) (*ModelInfo, error)
func (m *ModelManager) Load(name string, opts LoadOptions) (*LoadedModel, error)
func (m *ModelManager) Unload() error
func (m *ModelManager) Pull(ctx context.Context, name string, opts PullOptions) error
func (m *ModelManager) Delete(name string) error
func (m *ModelManager) CreateFromModelfile(name string, modelfile []byte) error
```

### 4.2 GGUF Loader with Architecture Detection

**File**: `internal/model/loader.go`

```go
package model

import (
    "encoding/binary"
    "os"

    llama "github.com/ollama/ollama/llama"
)

// GGUFReader reads GGUF metadata without loading the full model
type GGUFReader struct {
    file    *os.File
    header  GGUFHeader
    metadata map[string]interface{}
}

type GGUFHeader struct {
    Magic        uint32
    Version      uint32
    TensorCount  uint64
    MetadataSize uint64
}

// DetectArchitecture reads GGUF metadata to determine model type
func (r *GGUFReader) DetectArchitecture() (string, error) {
    // Read architecture from metadata
    arch, ok := r.metadata["general.architecture"].(string)
    if !ok {
        return "", fmt.Errorf("architecture not found in GGUF metadata")
    }
    return arch, nil
}

// LoadModelWithArchitecture loads model with appropriate settings
func (m *ModelManager) LoadModelWithArchitecture(path string) (*LoadedModel, error) {
    // First read metadata to get architecture
    reader, err := NewGGUFReader(path)
    if err != nil {
        return nil, err
    }
    defer reader.Close()

    arch, err := reader.DetectArchitecture()
    if err != nil {
        return nil, err
    }

    // Get architecture-specific parameters
    params := m.getArchParams(arch, reader.metadata)

    // Load model with detected parameters
    model, err := llama.LoadModelFromFile(path, params)
    if err != nil {
        return nil, err
    }

    // Create context with appropriate size
    ctxParams := llama.ContextParams{
        ContextSize: m.getContextSize(reader.metadata),
        BatchSize:   512,
        // Other params...
    }

    ctx, err := llama.NewContextWithModel(model, ctxParams)
    if err != nil {
        model.Free()
        return nil, err
    }

    return &LoadedModel{
        Model:   model,
        Context: ctx,
        Info:    m.buildModelInfo(path, reader.metadata),
    }, nil
}

// Granite-specific parameter detection
func (m *ModelManager) getGraniteParams(metadata map[string]interface{}) llama.ModelParams {
    params := llama.ModelParams{
        UseMmap: true,
        // Defaults...
    }

    // Granite uses specific scaling factors
    if scale, ok := metadata["granite.attention_scale"].(float32); ok {
        params.AttentionScale = scale
    }
    if scale, ok := metadata["granite.embedding_scale"].(float32); ok {
        params.EmbeddingScale = scale
    }

    return params
}
```

### 4.3 Prompt Manager with Context Window

**File**: `internal/prompt/manager.go`

```go
package prompt

import (
    "container/list"
    "strings"
    "sync"

    "github.com/gguf-run/internal/model"
)

// Message roles
const (
    RoleSystem    = "system"
    RoleUser      = "user"
    RoleAssistant = "assistant"
    RoleTool      = "tool"
)

type Message struct {
    Role    string   `json:"role"`
    Content string   `json:"content"`
    Images  []string `json:"images,omitempty"`
    ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
    ID       string                 `json:"id"`
    Type     string                 `json:"type"`
    Function ToolFunction           `json:"function"`
}

type ToolFunction struct {
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"`
}

// PromptManager handles conversation state
type PromptManager struct {
    mu           sync.RWMutex
    history      *list.List // Doubly linked list for efficient pruning
    maxTokens    int
    systemPrompt string
    templates    map[string]*Template
    tokenCounter TokenCounter
}

type TokenCounter interface {
    Count(text string) (int, error)
}

type Template struct {
    Name        string
    Content     string
    Variables   []string
    Description string
}

// Key methods
func (p *PromptManager) AddMessage(msg Message) error
func (p *PromptManager) GetHistory() []Message
func (p *PromptManager) Clear()
func (p *PromptManager) BuildPrompt() (string, error)
func (p *PromptManager) ApplyTemplate(name string, vars map[string]interface{}) (string, error)
func (p *PromptManager) PruneToFit(maxTokens int) error

// Context window management with sliding window
func (p *PromptManager) PruneToFit(maxTokens int) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    // Count total tokens
    total, err := p.countTokens()
    if err != nil {
        return err
    }

    // Prune oldest messages until under limit
    for total > maxTokens && p.history.Len() > 0 {
        front := p.history.Front()
        if front == nil {
            break
        }

        msg := front.Value.(Message)
        tokens, err := p.tokenCounter.Count(msg.Content)
        if err != nil {
            return err
        }

        p.history.Remove(front)
        total -= tokens
    }

    return nil
}

// Granite-specific chat template
func (p *PromptManager) GraniteTemplate(messages []Message, thinking bool) (string, error) {
    var builder strings.Builder

    // Add system message if present
    if p.systemPrompt != "" {
        builder.WriteString("<|system|>\n")
        builder.WriteString(p.systemPrompt)
        builder.WriteString("\n")
    }

    // Enable thinking mode if requested
    if thinking {
        builder.WriteString("<|thinking|>\n")
        builder.WriteString("Let me think through this step by step:\n")
        // The model will generate reasoning here
        builder.WriteString("<|assistant|>\n")
    }

    // Add conversation history
    for _, msg := range messages {
        builder.WriteString("<|" + msg.Role + "|>\n")
        builder.WriteString(msg.Content)
        builder.WriteString("\n")
    }

    builder.WriteString("<|assistant|>\n")
    return builder.String(), nil
}
```

### 4.4 Generation Engine with Streaming

**File**: `internal/generate/generate.go`

```go
package generate

import (
    "context"
    "io"
    "time"

    "github.com/gguf-run/internal/model"
    "github.com/gguf-run/internal/prompt"
    llama "github.com/ollama/ollama/llama"
)

type GenerationOptions struct {
    Temperature  float32
    TopP         float32
    TopK         int
    MaxTokens    int
    Stop         []string
    Seed         uint32
    RepeatPenalty float32
    FrequencyPenalty float32
    PresencePenalty float32
    Mirostat     int     // 0=disabled, 1=Mirostat, 2=Mirostat 2.0
    Grammar      string  // For structured output
    Stream       bool
}

type GenerationResult struct {
    Text      string
    Tokens    []int
    PromptTokens int
    GenTokens    int
    Duration  time.Duration
    Done      bool
}

type StreamChunk struct {
    Token     string
    Text      string
    Done      bool
    Error     error
}

type Generator struct {
    model    *model.LoadedModel
    sampler  *llama.Sampler
    tokenizer *llama.Tokenizer
}

func NewGenerator(model *model.LoadedModel) *Generator {
    return &Generator{
        model: model,
        sampler: llama.NewSampler(),
    }
}

// Generate completes a prompt
func (g *Generator) Generate(ctx context.Context, prompt string, opts GenerationOptions) (*GenerationResult, error) {
    // Tokenize prompt
    tokens, err := g.tokenizer.Encode(prompt)
    if err != nil {
        return nil, err
    }

    start := time.Now()

    // Setup sampling parameters
    g.sampler.SetTemperature(opts.Temperature)
    g.sampler.SetTopP(opts.TopP)
    g.sampler.SetTopK(opts.TopK)

    if opts.Grammar != "" {
        g.sampler.SetGrammar(opts.Grammar)
    }

    var generated []int
    var result strings.Builder

    // Generation loop
    for i := 0; i < opts.MaxTokens; i++ {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }

        // Evaluate model
        token, err := g.model.Context.Eval(tokens)
        if err != nil {
            return nil, err
        }

        // Sample next token
        next, err := g.sampler.Sample(g.model.Context, token)
        if err != nil {
            return nil, err
        }

        // Check stop conditions
        if next == g.tokenizer.EosToken() {
            break
        }

        // Decode token
        text, err := g.tokenizer.Decode([]int{next})
        if err != nil {
            return nil, err
        }

        result.WriteString(text)
        generated = append(generated, next)

        // Check stop strings
        if g.shouldStop(result.String(), opts.Stop) {
            break
        }

        // Prepare next iteration
        tokens = []int{next}
    }

    duration := time.Since(start)

    return &GenerationResult{
        Text:        result.String(),
        Tokens:      generated,
        PromptTokens: len(tokens),
        GenTokens:   len(generated),
        Duration:    duration,
        Done:        true,
    }, nil
}

// GenerateStream streams tokens as they're generated
func (g *Generator) GenerateStream(ctx context.Context, prompt string, opts GenerationOptions) (<-chan StreamChunk, error) {
    chunkChan := make(chan StreamChunk)

    go func() {
        defer close(chunkChan)

        // Similar to Generate but sends chunks
        // Implementation details...
    }()

    return chunkChan, nil
}

// Grammar for structured output
func (g *Generator) GrammarFromJSONSchema(schema []byte) (string, error) {
    // Convert JSON schema to GBNF grammar
    // This enables structured output generation
    return llama.SchemaToGrammar(schema)
}
```

### 4.5 Modelfile Parser

**File**: `pkg/modelfile/parser.go`

```go
package modelfile

import (
    "bufio"
    "fmt"
    "io"
    "strings"

    "gopkg.in/yaml.v3"
)

// Parser handles Modelfile format
type Parser struct {
    seen map[string]bool
}

// Modelfile structure (Ollama-compatible)
type Modelfile struct {
    From        string                 `yaml:"from"`
    Parameters  map[string]interface{} `yaml:"parameters,omitempty"`
    System      string                 `yaml:"system,omitempty"`
    Template    string                 `yaml:"template,omitempty"`
    Context     int                    `yaml:"context,omitempty"`
    Stop        []string               `yaml:"stop,omitempty"`
    License     string                 `yaml:"license,omitempty"`
    Messages    []Message              `yaml:"messages,omitempty"`
    Adapters    []string               `yaml:"adapters,omitempty"`
}

type Message struct {
    Role    string `yaml:"role"`
    Content string `yaml:"content"`
}

// Parse reads a Modelfile from reader
func (p *Parser) Parse(r io.Reader) (*Modelfile, error) {
    scanner := bufio.NewScanner(r)
    mf := &Modelfile{
        Parameters: make(map[string]interface{}),
    }

    var current strings.Builder
    var inBlock bool
    var blockType string

    for scanner.Scan() {
        line := scanner.Text()

        // Handle multi-line blocks (e.g., SYSTEM, TEMPLATE)
        if inBlock {
            if line == "```" {
                // End of block
                inBlock = false
                p.processBlock(blockType, current.String(), mf)
                current.Reset()
            } else {
                current.WriteString(line)
                current.WriteString("\n")
            }
            continue
        }

        // Parse directives
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }

        parts := strings.SplitN(line, " ", 2)
        if len(parts) < 2 {
            continue
        }

        directive := strings.ToUpper(parts[0])
        value := strings.TrimSpace(parts[1])

        switch directive {
        case "FROM":
            mf.From = value
        case "PARAMETER":
            paramParts := strings.SplitN(value, " ", 2)
            if len(paramParts) == 2 {
                mf.Parameters[paramParts[0]] = paramParts[1]
            }
        case "SYSTEM":
            if value == "```" {
                inBlock = true
                blockType = "SYSTEM"
            } else {
                mf.System = value
            }
        case "TEMPLATE":
            if value == "```" {
                inBlock = true
                blockType = "TEMPLATE"
            } else {
                mf.Template = value
            }
        case "LICENSE":
            if value == "```" {
                inBlock = true
                blockType = "LICENSE"
            } else {
                mf.License = value
            }
        case "MESSAGE":
            msgParts := strings.SplitN(value, " ", 2)
            if len(msgParts) == 2 {
                mf.Messages = append(mf.Messages, Message{
                    Role:    msgParts[0],
                    Content: msgParts[1],
                })
            }
        case "ADAPTER":
            mf.Adapters = append(mf.Adapters, value)
        }
    }

    return mf, scanner.Err()
}

// ParseFile reads a Modelfile from disk
func (p *Parser) ParseFile(path string) (*Modelfile, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    return p.Parse(f)
}

// ToYAML converts Modelfile to YAML for storage
func (mf *Modelfile) ToYAML() ([]byte, error) {
    return yaml.Marshal(mf)
}
```

### 4.6 Tool Executor with MCP Support

**File**: `internal/tools/executor.go`

```go
package tools

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"

    "github.com/gguf-run/internal/model"
)

// Tool definition (OpenAI-compatible)
type Tool struct {
    Type     string       `json:"type"` // "function"
    Function Function     `json:"function"`
}

type Function struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ToolCall request from model
type ToolCall struct {
    ID       string          `json:"id"`
    Type     string          `json:"type"`
    Function ToolFunction    `json:"function"`
}

type ToolFunction struct {
    Name      string          `json:"name"`
    Arguments json.RawMessage `json:"arguments"`
}

// ToolResult returned to model
type ToolResult struct {
    ToolCallID string          `json:"tool_call_id"`
    Content    string          `json:"content"`
}

// Executor handles tool execution
type Executor struct {
    mu       sync.RWMutex
    registry map[string]Tool
    clients  map[string]MCPClient
}

// MCPClient connects to Model Context Protocol servers
type MCPClient struct {
    Name    string
    URL     string
    Tools   []Tool
    client  *http.Client
}

// Register adds a tool to the registry
func (e *Executor) Register(tool Tool) error {
    e.mu.Lock()
    defer e.mu.Unlock()

    if _, exists := e.registry[tool.Function.Name]; exists {
        return fmt.Errorf("tool %s already registered", tool.Function.Name)
    }

    e.registry[tool.Function.Name] = tool
    return nil
}

// Execute runs a tool call
func (e *Executor) Execute(ctx context.Context, call ToolCall) (*ToolResult, error) {
    e.mu.RLock()
    tool, exists := e.registry[call.Function.Name]
    e.mu.RUnlock()

    if !exists {
        return nil, fmt.Errorf("tool %s not found", call.Function.Name)
    }

    // Parse arguments
    var args map[string]interface{}
    if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
        return nil, fmt.Errorf("invalid arguments: %w", err)
    }

    // Execute based on tool type
    var result interface{}
    var err error

    // Check if this is a built-in tool or MCP tool
    if strings.HasPrefix(tool.Function.Name, "mcp_") {
        // Route to MCP server
        result, err = e.executeMCP(ctx, tool.Function.Name, args)
    } else {
        // Execute built-in tool
        result, err = e.executeBuiltin(ctx, tool.Function.Name, args)
    }

    if err != nil {
        return nil, err
    }

    // Convert result to string
    resultBytes, err := json.Marshal(result)
    if err != nil {
        return nil, err
    }

    return &ToolResult{
        ToolCallID: call.ID,
        Content:    string(resultBytes),
    }, nil
}

// RegisterMCPServer connects to an MCP server and imports its tools
func (e *Executor) RegisterMCPServer(name, url string) error {
    // Discover tools from MCP server
    resp, err := http.Get(url + "/tools")
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    var tools []Tool
    if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
        return err
    }

    // Register each tool with mcp_ prefix
    for _, tool := range tools {
        tool.Function.Name = "mcp_" + name + "_" + tool.Function.Name
        if err := e.Register(tool); err != nil {
            return err
        }
    }

    e.mu.Lock()
    e.clients[name] = MCPClient{
        Name:   name,
        URL:    url,
        Tools:  tools,
        client: &http.Client{},
    }
    e.mu.Unlock()

    return nil
}
```

### 4.7 API Server (Phase 2)

**File**: `internal/api/server.go`

```go
package api

import (
    "context"
    "encoding/json"
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/go-chi/cors"

    "github.com/gguf-run/internal/model"
    "github.com/gguf-run/internal/generate"
)

type Server struct {
    modelManager *model.ModelManager
    generator    *generate.Generator
    httpServer   *http.Server
    config       *ServerConfig
}

type ServerConfig struct {
    Host         string
    Port         int
    ReadTimeout  time.Duration
    WriteTimeout time.Duration
    MaxBodySize  int64
    EnableCORS   bool
}

// OpenAI-compatible request/response structures
type ChatCompletionRequest struct {
    Model       string          `json:"model"`
    Messages    []Message       `json:"messages"`
    Temperature *float32        `json:"temperature,omitempty"`
    TopP        *float32        `json:"top_p,omitempty"`
    MaxTokens   *int            `json:"max_tokens,omitempty"`
    Stream      bool            `json:"stream,omitempty"`
    Tools       []model.Tool    `json:"tools,omitempty"`
    ToolChoice  interface{}     `json:"tool_choice,omitempty"` // string or object
}

type ChatCompletionResponse struct {
    ID      string   `json:"id"`
    Object  string   `json:"object"`
    Created int64    `json:"created"`
    Model   string   `json:"model"`
    Choices []Choice `json:"choices"`
    Usage   Usage    `json:"usage"`
}

type Choice struct {
    Index        int     `json:"index"`
    Message      Message `json:"message"`
    FinishReason string  `json:"finish_reason"`
}

type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}

// Routes setup
func (s *Server) SetupRoutes() http.Handler {
    r := chi.NewRouter()

    // Middleware
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(middleware.Timeout(60 * time.Second))

    if s.config.EnableCORS {
        r.Use(cors.Handler(cors.Options{
            AllowedOrigins: []string{"*"},
            AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
            AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
            MaxAge:         300,
        }))
    }

    // Health check
    r.Get("/health", s.healthCheck)

    // OpenAI-compatible endpoints
    r.Route("/v1", func(r chi.Router) {
        r.Get("/models", s.listModels)
        r.Get("/models/{model}", s.getModel)
        r.Post("/completions", s.completions)
        r.Post("/chat/completions", s.chatCompletions)
        r.Post("/embeddings", s.embeddings)
    })

    // MCP endpoints
    r.Route("/mcp", func(r chi.Router) {
        r.Get("/tools", s.listTools)
        r.Post("/tools/{tool}/execute", s.executeTool)
    })

    return r
}

// Chat completions handler
func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
    var req ChatCompletionRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    // Load model if not already loaded
    loadedModel, err := s.modelManager.Load(req.Model, model.LoadOptions{})
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Prepare generation options
    opts := generate.GenerationOptions{
        Temperature: defaultValue(req.Temperature, 0.8),
        TopP:        defaultValue(req.TopP, 0.9),
        MaxTokens:   defaultValue(req.MaxTokens, 2048),
        Stream:      req.Stream,
    }

    if req.Stream {
        // Handle streaming response
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")

        flusher, ok := w.(http.Flusher)
        if !ok {
            http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
            return
        }

        // Build prompt from messages
        prompt := s.buildPrompt(req.Messages)

        // Stream generation
        stream, err := s.generator.GenerateStream(r.Context(), prompt, opts)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        for chunk := range stream {
            // Format as SSE
            data, _ := json.Marshal(map[string]interface{}{
                "choices": []map[string]interface{}{
                    {
                        "delta": map[string]string{
                            "content": chunk.Token,
                        },
                    },
                },
            })
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()

            if chunk.Done {
                fmt.Fprintf(w, "data: [DONE]\n\n")
                flusher.Flush()
                break
            }
        }
    } else {
        // Handle non-streaming response
        prompt := s.buildPrompt(req.Messages)

        result, err := s.generator.Generate(r.Context(), prompt, opts)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        resp := ChatCompletionResponse{
            ID:      "chatcmpl-" + generateID(),
            Object:  "chat.completion",
            Created: time.Now().Unix(),
            Model:   req.Model,
            Choices: []Choice{
                {
                    Index: 0,
                    Message: Message{
                        Role:    "assistant",
                        Content: result.Text,
                    },
                    FinishReason: "stop",
                },
            },
            Usage: Usage{
                PromptTokens:     result.PromptTokens,
                CompletionTokens: result.GenTokens,
                TotalTokens:      result.PromptTokens + result.GenTokens,
            },
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    }
}
```

---

## 5. Configuration & Storage

### 5.1 XDG Base Directory Support

**File**: `internal/config/paths.go`

```go
package config

import (
    "os"
    "path/filepath"
    "runtime"
)

// Paths manages all directory locations following XDG spec
type Paths struct {
    // Data directory: ~/.local/share/gguf-run/
    DataDir string

    // Config directory: ~/.config/gguf-run/
    ConfigDir string

    // Cache directory: ~/.cache/gguf-run/
    CacheDir string
}

func GetPaths() (*Paths, error) {
    paths := &Paths{}

    // Data directory (models)
    if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
        paths.DataDir = filepath.Join(xdgData, "gguf-run")
    } else {
        home, err := os.UserHomeDir()
        if err != nil {
            return nil, err
        }
        paths.DataDir = filepath.Join(home, ".local", "share", "gguf-run")
    }

    // Config directory
    if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
        paths.ConfigDir = filepath.Join(xdgConfig, "gguf-run")
    } else {
        home, err := os.UserHomeDir()
        if err != nil {
            return nil, err
        }
        paths.ConfigDir = filepath.Join(home, ".config", "gguf-run")
    }

    // Cache directory
    if xdgCache := os.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
        paths.CacheDir = filepath.Join(xdgCache, "gguf-run")
    } else {
        home, err := os.UserHomeDir()
        if err != nil {
            return nil, err
        }
        paths.CacheDir = filepath.Join(home, ".cache", "gguf-run")
    }

    // Create directories if they don't exist
    for _, dir := range []string{paths.DataDir, paths.ConfigDir, paths.CacheDir} {
        if err := os.MkdirAll(dir, 0755); err != nil {
            return nil, err
        }
    }

    return paths, nil
}

// ModelsDir returns the directory where models are stored
func (p *Paths) ModelsDir() string {
    return filepath.Join(p.DataDir, "models")
}

// ModelPath returns the path for a specific model
func (p *Paths) ModelPath(name, quantization string) string {
    return filepath.Join(p.ModelsDir(), name, "model."+quantization+".gguf")
}
```

### 5.2 Configuration Management

**File**: `internal/config/config.go`

```go
package config

import (
    "encoding/json"
    "os"

    "github.com/spf13/viper"
)

type Config struct {
    // Server settings
    Server struct {
        Host        string `mapstructure:"host"`
        Port        int    `mapstructure:"port"`
        ReadTimeout int    `mapstructure:"read_timeout"`
    } `mapstructure:"server"`

    // Model settings
    Model struct {
        DefaultModel    string  `mapstructure:"default_model"`
        DefaultQuant    string  `mapstructure:"default_quant"`
        NumGpuLayers    int     `mapstructure:"num_gpu_layers"`
        UseMmap         bool    `mapstructure:"use_mmap"`
        UseMlock        bool    `mapstructure:"use_mlock"`
    } `mapstructure:"model"`

    // Generation defaults
    Generate struct {
        Temperature  float32  `mapstructure:"temperature"`
        TopP         float32  `mapstructure:"top_p"`
        TopK         int      `mapstructure:"top_k"`
        MaxTokens    int      `mapstructure:"max_tokens"`
        RepeatPenalty float32 `mapstructure:"repeat_penalty"`
    } `mapstructure:"generate"`

    // Paths
    paths *Paths
}

func Load() (*Config, error) {
    paths, err := GetPaths()
    if err != nil {
        return nil, err
    }

    v := viper.New()
    v.SetConfigName("config")
    v.SetConfigType("yaml")
    v.AddConfigPath(paths.ConfigDir)
    v.AddConfigPath(".")

    // Set defaults
    v.SetDefault("server.host", "127.0.0.1")
    v.SetDefault("server.port", 8080)
    v.SetDefault("server.read_timeout", 60)

    v.SetDefault("model.default_quant", "q4_K_M")
    v.SetDefault("model.num_gpu_layers", 35)
    v.SetDefault("model.use_mmap", true)
    v.SetDefault("model.use_mlock", false)

    v.SetDefault("generate.temperature", 0.8)
    v.SetDefault("generate.top_p", 0.9)
    v.SetDefault("generate.top_k", 40)
    v.SetDefault("generate.max_tokens", 2048)
    v.SetDefault("generate.repeat_penalty", 1.1)

    // Read config
    if err := v.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            return nil, err
        }
        // Config file not found - use defaults
    }

    var config Config
    if err := v.Unmarshal(&config); err != nil {
        return nil, err
    }

    config.paths = paths
    return &config, nil
}
```

---

## 6. CLI Implementation

**File**: `cmd/root.go`

```go
package cmd

import (
    "fmt"
    "os"
    "strings"

    "github.com/spf13/cobra"
    "github.com/spf13/viper"

    "github.com/gguf-run/internal/config"
)

var (
    cfgFile string
    config  *config.Config
)

var rootCmd = &cobra.Command{
    Use:   "gguf-run",
    Short: "Run GGUF models locally",
    Long: `gguf-run - A local LLM runtime for GGUF models

Single binary that can run any GGUF model with features like:
  • Interactive chat
  • Modelfile support for custom models
  • OpenAI-compatible API server
  • Tool calling and MCP integration
  • Optimized for IBM Granite models`,
    PersistentPreRun: func(cmd *cobra.Command, args []string) {
        // Load config
        var err error
        config, err = config.Load()
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
            os.Exit(1)
        }
    },
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func init() {
    cobra.OnInitialize(initConfig)

    rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/gguf-run/config.yaml)")
    rootCmd.PersistentFlags().StringP("model", "m", "", "Model name to use")
    rootCmd.PersistentFlags().IntP("ctx-size", "c", 0, "Context size")
    rootCmd.PersistentFlags().Float32P("temperature", "t", 0, "Temperature")
    rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose output")

    viper.BindPFlag("model.default_model", rootCmd.PersistentFlags().Lookup("model"))
    viper.BindPFlag("model.context_size", rootCmd.PersistentFlags().Lookup("ctx-size"))
    viper.BindPFlag("generate.temperature", rootCmd.PersistentFlags().Lookup("temperature"))
}

func initConfig() {
    if cfgFile != "" {
        viper.SetConfigFile(cfgFile)
    } else {
        paths, _ := config.GetPaths()
        viper.AddConfigPath(paths.ConfigDir)
        viper.SetConfigName("config")
        viper.SetConfigType("yaml")
    }

    viper.AutomaticEnv()
    viper.SetEnvPrefix("GGUF")
    viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

    if err := viper.ReadInConfig(); err == nil {
        fmt.Println("Using config file:", viper.ConfigFileUsed())
    }
}
```

**File**: `cmd/pull.go`

```go
package cmd

import (
    "context"
    "fmt"
    "time"

    "github.com/spf13/cobra"
    "github.com/gguf-run/internal/model"
)

var pullCmd = &cobra.Command{
    Use:   "pull [model]",
    Short: "Download a model from Hugging Face",
    Args:  cobra.ExactArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        modelName := args[0]
        quantization, _ := cmd.Flags().GetString("quantization")

        // Create model manager
        mm := model.NewManager(config)

        // Setup progress reporting
        ctx := context.Background()

        fmt.Printf("Pulling %s (%s)...\n", modelName, quantization)

        start := time.Now()
        err := mm.Pull(ctx, modelName, model.PullOptions{
            Quantization: quantization,
            Progress: func(downloaded, total int64) {
                percent := float64(downloaded) / float64(total) * 100
                fmt.Printf("\rDownloaded: %.1f%% (%d/%d MB)", 
                    percent, downloaded/1024/1024, total/1024/1024)
            },
        })

        if err != nil {
            fmt.Printf("\nError: %v\n", err)
            return
        }

        duration := time.Since(start)
        fmt.Printf("\n✅ Pulled %s in %.1fs\n", modelName, duration.Seconds())
    },
}

func init() {
    pullCmd.Flags().StringP("quantization", "q", "q4_K_M", "Quantization type")
    rootCmd.AddCommand(pullCmd)
}
```

**File**: `cmd/chat.go`

```go
package cmd

import (
    "bufio"
    "fmt"
    "os"
    "strings"

    "github.com/fatih/color"
    "github.com/spf13/cobra"

    "github.com/gguf-run/internal/generate"
    "github.com/gguf-run/internal/model"
    "github.com/gguf-run/internal/prompt"
)

var chatCmd = &cobra.Command{
    Use:   "chat [model]",
    Short: "Start an interactive chat session",
    Args:  cobra.MaximumNArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        var modelName string
        if len(args) > 0 {
            modelName = args[0]
        } else {
            modelName = config.Model.DefaultModel
        }

        if modelName == "" {
            fmt.Println("Error: no model specified. Use --model or set default in config")
            return
        }

        // Initialize components
        mm := model.NewManager(config)
        pm := prompt.NewManager(config)
        system, _ := cmd.Flags().GetString("system")

        if system != "" {
            pm.SetSystemPrompt(system)
        }

        // Load model
        fmt.Printf("Loading %s...\n", modelName)
        loaded, err := mm.Load(modelName, model.LoadOptions{})
        if err != nil {
            fmt.Printf("Error loading model: %v\n", err)
            return
        }

        gen := generate.NewGenerator(loaded)

        // Setup colors
        userColor := color.New(color.FgGreen).SprintFunc()
        assistantColor := color.New(color.FgCyan).SprintFunc()
        systemColor := color.New(color.FgYellow).SprintFunc()

        fmt.Println(systemColor("\nChat session started. Type /help for commands, /exit to quit.\n"))

        scanner := bufio.NewScanner(os.Stdin)

        for {
            fmt.Print(userColor("\nYou: "))
            if !scanner.Scan() {
                break
            }

            input := scanner.Text()

            // Handle commands
            if strings.HasPrefix(input, "/") {
                if !handleChatCommand(input, pm, gen) {
                    break
                }
                continue
            }

            // Add user message
            pm.AddMessage(prompt.Message{
                Role:    "user",
                Content: input,
            })

            // Build prompt
            promptText, err := pm.BuildPrompt()
            if err != nil {
                fmt.Printf("Error building prompt: %v\n", err)
                continue
            }

            // Generate response
            fmt.Print(assistantColor("\nAssistant: "))

            opts := generate.GenerationOptions{
                Temperature: config.Generate.Temperature,
                TopP:        config.Generate.TopP,
                MaxTokens:   config.Generate.MaxTokens,
                Stream:      true,
            }

            stream, err := gen.GenerateStream(cmd.Context(), promptText, opts)
            if err != nil {
                fmt.Printf("Error: %v\n", err)
                continue
            }

            var fullResponse strings.Builder
            for chunk := range stream {
                if chunk.Error != nil {
                    fmt.Printf("Error: %v\n", chunk.Error)
                    break
                }
                fmt.Print(chunk.Token)
                fullResponse.WriteString(chunk.Token)
            }
            fmt.Println()

            // Add assistant response to history
            pm.AddMessage(prompt.Message{
                Role:    "assistant",
                Content: fullResponse.String(),
            })
        }
    },
}

func handleChatCommand(cmd string, pm *prompt.PromptManager, gen *generate.Generator) bool {
    switch cmd {
    case "/exit", "/quit":
        fmt.Println("Goodbye!")
        return false
    case "/clear":
        pm.Clear()
        fmt.Println("Conversation cleared.")
    case "/help":
        fmt.Println("Commands:")
        fmt.Println("  /exit, /quit  - Exit chat")
        fmt.Println("  /clear         - Clear conversation")
        fmt.Println("  /save [name]   - Save conversation")
        fmt.Println("  /load [name]   - Load conversation")
        fmt.Println("  /system [text] - Set system prompt")
    case "/save":
        // Implementation
    case "/load":
        // Implementation
    default:
        if strings.HasPrefix(cmd, "/system ") {
            systemPrompt := strings.TrimPrefix(cmd, "/system ")
            pm.SetSystemPrompt(systemPrompt)
            fmt.Println("System prompt updated.")
        } else {
            fmt.Printf("Unknown command: %s\n", cmd)
        }
    }
    return true
}

func init() {
    chatCmd.Flags().StringP("system", "s", "", "System prompt")
    rootCmd.AddCommand(chatCmd)
}
```

---

## 7. Build & Distribution

### 7.1 Makefile

```makefile
.PHONY: build clean test install

BINARY_NAME=gguf-run
VERSION=$(shell git describe --tags --always --dirty)
COMMIT=$(shell git rev-parse --short HEAD)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_TIME)"

# Build for current platform
build:
    go build $(LDFLAGS) -o $(BINARY_NAME) .

# Build for all platforms
build-all:
    GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-amd64 .
    GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-arm64 .
    GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-amd64 .
    GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-arm64 .
    GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-windows-amd64.exe .

# Install to GOPATH/bin
install:
    go install $(LDFLAGS)

# Clean build artifacts
clean:
    rm -f $(BINARY_NAME)
    rm -rf dist/

# Run tests
test:
    go test -v ./...

# Run benchmarks
bench:
    go test -bench=. ./...

# Generate shell completions
completions:
    mkdir -p completions
    for sh in bash zsh fish; do \
        go run main.go completion $$sh > completions/$(BINARY_NAME).$$sh; \
    done

# Create a release archive
release: build-all
    cd dist && for f in *; do \
        tar czf $$f.tar.gz $$f; \
    done

# Install dependencies
deps:
    go mod download

# Format code
fmt:
    go fmt ./...

# Lint code
lint:
    golangci-lint run
```

### 7.2 GitHub Actions Workflow

**.github/workflows/release.yml**

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'

      - name: Build
        run: make build-all

      - name: Create Release
        uses: softprops/action-gh-release@v1
        with:
          files: dist/*
          generate_release_notes: true
```

---

## 8. Development Roadmap

### Phase 1: Core CLI (Weeks 1-4)

- [x] Project structure and build system
- [ ] Basic model loading with llama.cpp bindings
- [ ] Model pull from Hugging Face
- [ ] Simple completion command
- [ ] Interactive chat with history
- [ ] Model listing and management

### Phase 2: Modelfile & Customization (Weeks 5-8)

- [ ] Modelfile parser (Ollama-compatible)
- [ ] Model creation from Modelfile
- [ ] Parameter tuning (temperature, top_p, etc.)
- [ ] System prompt support
- [ ] Chat templates
- [ ] Granite optimizations (thinking mode)

### Phase 3: Advanced Features (Weeks 9-12)

- [ ] Grammar-based structured output
- [ ] Embeddings support
- [ ] Tool calling framework
- [ ] LoRA adapter support
- [ ] Context window management
- [ ] Performance optimizations

### Phase 4: API Server (Weeks 13-16)

- [ ] HTTP server with chi
- [ ] OpenAI-compatible endpoints
- [ ] Streaming responses
- [ ] Multi-model serving
- [ ] MCP protocol support
- [ ] Authentication (API keys)

### Phase 5: Polish & Distribution (Weeks 17-20)

- [ ] Comprehensive documentation
- [ ] Website and examples
- [ ] Package managers (Homebrew, APT, etc.)
- [ ] Performance benchmarking
- [ ] Memory usage optimization
- [ ] User feedback incorporation

---

## 9. Testing Strategy

### Unit Tests

```go
func TestModelManager_Load(t *testing.T) {
    // Test with mock GGUF files
}

func TestPromptManager_PruneToFit(t *testing.T) {
    // Test context window management
}

func TestModelfileParser_Parse(t *testing.T) {
    // Test Modelfile parsing with various formats
}
```

### Integration Tests

```go
func TestEndToEnd_Generation(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Download tiny test model
    // Generate text
    // Verify output
}
```

### Benchmark Tests

```go
func BenchmarkGeneration(b *testing.B) {
    for i := 0; i < b.N; i++ {
        // Benchmark generation speed
    }
}
```

---

## 10. Questions for AI Agent

When implementing this design, please consider:

1. **llama.cpp bindings**: Should we use `ollama/ollama/llama` or `go-skynet/go-llama.cpp`? The former is more production-tested, the latter is simpler.

2. **GGUF metadata parsing**: Do we need a full GGUF parser or can we rely on llama.cpp's built-in metadata access?

3. **Granite optimizations**: Are there any Granite-specific features we should prioritize beyond attention/embedding scales?

4. **MCP support**: For Phase 2, should we implement a full MCP client or start with a simpler tool registry?

5. **Memory management**: How should we handle multiple loaded models? Unload after inactivity? Keep one active?

6. **Distribution**: Should we provide pre-built binaries for all platforms or focus on Linux first?

7. **Configuration**: YAML, TOML, or JSON? YAML seems most user-friendly.

8. **Testing**: How to test with actual models without including large files in repo?

---

## 11. Success Criteria

The project will be considered successful when:

1. **Single binary** < 30MB can run any GGUF model
2. **Model pull** works from Hugging Face
3. **Interactive chat** with history works smoothly
4. **Modelfile creation** matches Ollama's feature set
5. **Granite models** run with optimal parameters
6. **API server** provides OpenAI compatibility
7. **Performance** matches or exceeds Ollama
8. **Documentation** is comprehensive and clear
9. **Tests** cover critical paths
10. **Installation** is one command

---

This design document provides a comprehensive blueprint for an AI coding agent to implement `gguf-run`. The modular structure, clear interfaces, and detailed code examples should enable efficient development. The project is sized for a 4-5 month development effort with a clear path to a production-ready tool.

Would you like me to elaborate on any section or provide more detailed code for specific components?
