package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/jsoprych/guff/internal/config"
	llamaensure "github.com/jsoprych/guff/internal/llama"
)

var (
	backendOnce sync.Once
)

func initBackend(libDir string) {
	backendOnce.Do(func() {
		if err := llamaensure.EnsureLibraries(libDir); err != nil {
			panic(fmt.Sprintf("failed to ensure llama.cpp libraries: %v", err))
		}
		libPath := libDir // directory containing llama.cpp libraries
		if err := llama.Load(libPath); err != nil {
			panic(fmt.Sprintf("failed to load llama.cpp library: %v", err))
		}
		llama.LogSet(llama.LogNormal)
		llama.Init()
	})
}

// ModelManager handles model lifecycle
type ModelManager struct {
	mu         sync.RWMutex
	modelsDir  string
	current    *LoadedModel
	registry   map[string]*ModelInfo
	downloader *HuggingFaceDownloader
	config     *config.Config
}

// LoadedModel represents an actively loaded model
type LoadedModel struct {
	Model    llama.Model
	Ctx      llama.Context
	Vocab    llama.Vocab
	Info     *ModelInfo
	LoadedAt time.Time
	LastUsed time.Time
	mu       sync.Mutex
}

// ModelInfo contains metadata about a model
type ModelInfo struct {
	Name         string       `json:"name"`
	Path         string       `json:"path"`
	Size         int64        `json:"size"`
	Quantization string       `json:"quantization"`
	Architecture string       `json:"architecture"` // granite, llama, etc.
	ContextLen   int          `json:"context_length"`
	Modified     time.Time    `json:"modified"`
	Digest       string       `json:"digest"` // SHA256 of file
	Config       *ModelConfig `json:"config,omitempty"`
}

// ModelConfig from Modelfile
type ModelConfig struct {
	From       string                 `yaml:"from"`
	Parameters map[string]interface{} `yaml:"parameters"`
	System     string                 `yaml:"system"`
	Template   string                 `yaml:"template"`
	Adapters   []string               `yaml:"adapters"`
	License    string                 `yaml:"license"`
	Messages   []Message              `yaml:"messages"`
}

// Message represents a chat message
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Images    []string   `json:"images,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// LoadOptions configures model loading
type LoadOptions struct {
	NumGpuLayers int
	UseMmap      bool
	UseMlock     bool
}

// PullOptions configures model download
type PullOptions struct {
	Quantization string
	Progress     func(downloaded, total int64)
}

// NewManager creates a new ModelManager
func NewManager(cfg *config.Config) *ModelManager {
	modelsDir := ""
	if cfg.Paths() != nil {
		modelsDir = cfg.Paths().ModelsDir()
	}
	return &ModelManager{
		config:     cfg,
		modelsDir:  modelsDir,
		registry:   make(map[string]*ModelInfo),
		downloader: NewHuggingFaceDownloader(cfg.HuggingFace.Token),
	}
}

// ScanModels scans the models directory and populates the registry
func (m *ModelManager) ScanModels() error {
	if m.modelsDir == "" {
		m.modelsDir = "./models"
	}
	if err := os.MkdirAll(m.modelsDir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.modelsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		modelName := entry.Name()
		modelDir := filepath.Join(m.modelsDir, modelName)
		ggufFiles, err := filepath.Glob(filepath.Join(modelDir, "*.gguf"))
		if err != nil {
			continue
		}
		for _, ggufPath := range ggufFiles {
			base := filepath.Base(ggufPath)
			quant := ""
			if len(base) > 5 && strings.HasSuffix(base, ".gguf") {
				parts := strings.Split(base, ".")
				if len(parts) >= 3 {
					quant = parts[len(parts)-2]
				}
			}
			if err := m.updateModelInfo(modelName, ggufPath, quant); err != nil {
				// log error
			}
		}
	}
	return nil
}

// List returns all available models
func (m *ModelManager) List() []*ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	models := make([]*ModelInfo, 0, len(m.registry))
	for _, info := range m.registry {
		models = append(models, info)
	}
	return models
}

// Get retrieves model info by name
func (m *ModelManager) Get(name string) (*ModelInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.registry[name]
	if !ok {
		return nil, ErrModelNotFound
	}
	return info, nil
}

// Load loads a model by name
func (m *ModelManager) Load(name string, opts LoadOptions) (*LoadedModel, error) {
	// Determine library directory
	libDir := "./lib"
	if m.config != nil && m.config.Paths() != nil {
		libDir = m.config.Paths().LibDir()
	}
	initBackend(libDir)
	m.mu.RLock()
	info, ok := m.registry[name]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrModelNotFound
	}

	// Prepare model loading parameters
	modelParams := llama.ModelDefaultParams()
	modelParams.NGpuLayers = int32(opts.NumGpuLayers)
	if opts.UseMmap {
		modelParams.UseMmap = 1
	}
	if opts.UseMlock {
		modelParams.UseMlock = 1
	}

	// Load the model
	model, err := llama.ModelLoadFromFile(info.Path, modelParams)
	if err != nil {
		return nil, fmt.Errorf("failed to load model: %w", err)
	}

	// Create context with default parameters
	ctxParams := llama.ContextDefaultParams()
	ctxParams.NCtx = 2048 // default context size

	ctx, err := llama.InitFromModel(model, ctxParams)
	if err != nil {
		llama.ModelFree(model)
		return nil, fmt.Errorf("failed to create context: %w", err)
	}

	vocab := llama.ModelGetVocab(model)
	loaded := &LoadedModel{
		Model:    model,
		Ctx:      ctx,
		Vocab:    vocab,
		Info:     info,
		LoadedAt: time.Now(),
		LastUsed: time.Now(),
	}

	m.mu.Lock()
	m.current = loaded
	m.mu.Unlock()

	return loaded, nil
}

// Unload unloads the current model
func (m *ModelManager) Unload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil {
		if m.current.Ctx != 0 {
			llama.Free(m.current.Ctx)
		}
		if m.current.Model != 0 {
			llama.ModelFree(m.current.Model)
		}
		m.current = nil
	}
	return nil
}

// Pull downloads a model from Hugging Face
func (m *ModelManager) Pull(ctx context.Context, name string, opts PullOptions) error {
	if m.downloader == nil {
		return errors.New("downloader not initialized")
	}

	// Map model name to Hugging Face repo
	var repo, fileName string
	switch name {
	case "tinyllama":
		repo = "TheBloke/TinyLlama-1.1B-GGUF"
		fileName = fmt.Sprintf("tinyllama-1.1b-%s.gguf", opts.Quantization)
	case "granite-3b":
		// Official IBM Granite 3B GGUF model (code-instruct, 2k context)
		repo = "ibm-granite/granite-3b-code-instruct-2k-GGUF"
		// File naming pattern: granite-3b-code-instruct.Q4_K_M.gguf
		fileName = fmt.Sprintf("granite-3b-code-instruct.%s.gguf", opts.Quantization)
	default:
		return fmt.Errorf("unknown model: %s", name)
	}

	// Get possible download URLs
	urls := m.downloader.GetHuggingFaceFileURLs(repo, fileName)

	// Create destination path - always use project directory for development
	modelsDir := "./models"
	modelDir := filepath.Join(modelsDir, name)
	destFile := filepath.Join(modelDir, fmt.Sprintf("model.%s.gguf", opts.Quantization))

	// Try each URL pattern
	var lastErr error
	for i, url := range urls {
		fmt.Printf("Trying URL %d: %s\n", i+1, url)
		err := m.downloader.DownloadFile(ctx, url, destFile, opts.Progress)
		if err == nil {
			fmt.Printf("Downloaded to %s\n", destFile)
			// Update registry
			return m.updateModelInfo(name, destFile, opts.Quantization)
		}
		lastErr = err
		fmt.Printf("URL %d failed: %v\n", i+1, err)
	}

	return fmt.Errorf("all download attempts failed. Last error: %w", lastErr)
}

// updateModelInfo updates the registry with information about a downloaded model
func (m *ModelManager) updateModelInfo(name, path, quantization string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat model file: %w", err)
	}

	// Compute SHA256 digest
	digest := ""
	file, err := os.Open(path)
	if err == nil {
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err == nil {
			digest = hex.EncodeToString(hash.Sum(nil))
		}
	}

	// TODO: parse GGUF metadata for architecture, context length, etc.
	modelInfo := &ModelInfo{
		Name:         name,
		Path:         path,
		Size:         info.Size(),
		Quantization: quantization,
		Architecture: "unknown",
		ContextLen:   2048, // default
		Modified:     info.ModTime(),
		Digest:       digest,
	}

	m.registry[name] = modelInfo
	return nil
}

// Delete removes a model
func (m *ModelManager) Delete(name string) error {
	// TODO: implement deletion
	return ErrNotImplemented
}

// CreateFromModelfile creates a model from a Modelfile
func (m *ModelManager) CreateFromModelfile(name string, modelfile []byte) error {
	// TODO: implement Modelfile parsing
	return ErrNotImplemented
}

// Errors
var (
	ErrModelNotFound  = errors.New("model not found")
	ErrNotImplemented = errors.New("not implemented")
)
