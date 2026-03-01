# AGENTS.md - Development Guide for guff

This document provides guidelines for AI agents working on the guff project.

## Project Overview

guff is a local LLM runtime in Go with llama.cpp backend, providing Ollama-like functionality with optimizations for IBM Granite models. The project is structured as a single-binary CLI tool and API server.

## Prerequisites

### System Dependencies

**Linux (Debian/Ubuntu):**
```bash
sudo apt-get update
sudo apt-get install -y build-essential cmake git
```

**macOS:**
```bash
brew install cmake git
```

**Windows:**
- Install [MSYS2](https://www.msys2.org/) or WSL
- Install CMake and Git

### llama.cpp Development Headers

guff depends on llama.cpp C headers (`common.h`, `llama.h`, `ggml.h`). You must build and install llama.cpp:

```bash
# Clone and build llama.cpp
git clone https://github.com/ggerganov/llama.cpp.git third_party/llama.cpp
cd third_party/llama.cpp
mkdir -p build && cd build
cmake .. -DLLAMA_CUBLAS=OFF -DLLAMA_METAL=OFF -DLLAMA_BLAS=OFF
make -j$(nproc)

# Install headers to system path (requires sudo)
sudo cp -r ../include/llama.h /usr/local/include/
sudo cp -r ../common/common.h /usr/local/include/
sudo cp -r ../ggml/include/ggml.h /usr/local/include/
sudo ldconfig
```

Alternatively, use the provided install script:
```bash
chmod +x install-llama-headers.sh
sudo ./install-llama-headers.sh
```

### Hugging Face Token (for model downloads)

Set your Hugging Face token as an environment variable:
```bash
export GUFF_HUGGINGFACE_TOKEN="hf_xxxxxxxxxxxxxxxxxxxx"
# Or add to ~/.bashrc
```

### Go Toolchain

- Go 1.25 or later (tested with 1.25.0)

## Build System

### Makefile Commands

The project uses a Makefile with the following targets:

```bash
# Build for current platform
make build

# Build for all platforms (Linux, macOS, Windows)
make build-all

# Install to GOPATH/bin
make install

# Run all tests with verbose output
make test

# Run benchmarks
make bench

# Clean build artifacts
make clean

# Format code
make fmt

# Lint with golangci-lint
make lint

# Install dependencies
make deps

# Generate shell completions
make completions

# Create release archives
make release
```

### Go-Specific Commands

```bash
# Build the binary
go build -o guff .

# Run tests for a specific package
go test -v ./internal/model

# Run a specific test
go test -v ./internal/model -run TestModelManager

# Run tests with coverage
go test -cover ./...

# Format code
go fmt ./...

# Tidy module dependencies
go mod tidy

# Download dependencies
go mod download

# Vet code for suspicious constructs
go vet ./...

# Lint with golangci-lint (if installed)
golangci-lint run
```

## Code Style Guidelines

### General Principles

1. **Production-ready code**: All code should be production quality with proper error handling, logging, and documentation.
2. **Simplicity**: Prefer simple, readable solutions over clever optimizations.
3. **Consistency**: Follow existing patterns in the codebase.

### Go Conventions

#### Imports

- Group imports: standard library, third-party, internal
- Use `goimports` to maintain proper grouping
- Import aliases should be meaningful (e.g., `llama "github.com/ollama/ollama/llama"`)

Example:
```go
import (
    "context"
    "fmt"
    "os"
    "sync"
    "time"

    "github.com/spf13/cobra"
    "github.com/spf13/viper"
    
    "github.com/guff/internal/config"
)
```

#### Naming Conventions

- **Packages**: lowercase, single-word, descriptive (e.g., `model`, `generate`, `tools`)
- **Interfaces**: Use `-er` suffix when appropriate (e.g., `Reader`, `Writer`, `Loader`)
- **Methods**: Mixed case, with receiver names as short abbreviations (e.g., `(m *ModelManager) Load()`)
- **Variables**: Use descriptive names, avoid single letters except for loops
- **Constants**: Uppercase with underscores
- **Error variables**: Prefix with `Err` (e.g., `ErrModelNotFound`)

#### Error Handling

- Always check errors immediately after function calls
- Return errors with context using `fmt.Errorf` with `%w` for wrapping
- Use sentinel errors for expected error conditions
- For HTTP handlers, use appropriate status codes and error responses

Example pattern:
```go
func LoadModel(path string) (*Model, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, fmt.Errorf("failed to open model file: %w", err)
    }
    defer file.Close()
    
    // ... rest of function
}
```

#### Structs and Interfaces

- Use struct tags for JSON/YAML serialization
- Embed interfaces for composition
- Keep structs focused on a single responsibility
- Document exported types and methods

Example:
```go
// ModelInfo contains metadata about a model
type ModelInfo struct {
    Name        string    `json:"name"`
    Path        string    `json:"path"`
    Size        int64     `json:"size"`
    Modified    time.Time `json:"modified"`
}

// ModelManager handles model lifecycle
type ModelManager interface {
    Load(name string) (*Model, error)
    Unload() error
    List() []*ModelInfo
}
```

#### Logging

- Use structured logging (log/slog) for production code
- Log at appropriate levels: debug, info, warn, error
- Include context in log messages (model names, request IDs, etc.)
- Avoid logging sensitive information

#### Testing

- Write table-driven tests for functions with multiple test cases
- Use test helpers for common setup/teardown
- Mock external dependencies (HTTP clients, file system)
- Benchmark critical paths
- Test error conditions and edge cases

Example test structure:
```go
func TestModelManager_Load(t *testing.T) {
    tests := []struct {
        name    string
        model   string
        wantErr bool
    }{
        {"valid model", "granite-3b", false},
        {"nonexistent model", "fake-model", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mgr := NewModelManager()
            _, err := mgr.Load(tt.model)
            if (err != nil) != tt.wantErr {
                t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Project Structure

```
guff/
├── cmd/                    # CLI command implementations
│   ├── root.go            # Root command
│   ├── pull.go            # Model download
│   ├── run.go             # Run model with prompt
│   ├── chat.go            # Interactive chat
│   └── serve.go           # API server (Phase 2)
├── internal/              # Private application code
│   ├── model/             # Model management
│   ├── generate/          # Text generation
│   ├── prompt/            # Prompt management
│   ├── tools/             # Tool calling framework
│   ├── api/               # HTTP API (Phase 2)
│   └── config/            # Configuration management
├── pkg/                   # Public libraries
│   ├── gguf/              # GGUF file utilities
│   └── modelfile/         # Modelfile parsing
├── main.go                # Application entry point
├── go.mod                 # Go module definition
├── Makefile               # Build automation
└── README.md              # Project documentation
```

### Development Workflow

1. **Start with tests**: Write tests before implementing new features
2. **Follow TDD**: Red → Green → Refactor cycle
3. **Run linting**: Always run `make lint` before committing
4. **Format code**: Use `make fmt` to ensure consistent formatting
5. **Check dependencies**: Run `go mod tidy` to keep dependencies clean
6. **Verify builds**: Test cross-compilation with `make build-all`

### Dependencies

Key external dependencies:
- `github.com/ollama/ollama/llama` or `go-skynet/go-llama.cpp` for llama.cpp bindings
- `github.com/spf13/cobra` for CLI framework
- `github.com/spf13/viper` for configuration
- `github.com/go-chi/chi` for HTTP routing (Phase 2)
- `gopkg.in/yaml.v3` for YAML parsing

### Special Considerations for LLM Integration

1. **Streaming responses**: Implement proper streaming for chat interfaces
2. **Context management**: Handle context windows and token counting
3. **Model compatibility**: Support various GGUF model architectures
4. **Performance**: Optimize for memory usage and inference speed
5. **Granite models**: Special handling for IBM Granite thinking mode

### Git Practices

- Write descriptive commit messages following conventional commits
- Keep commits focused on single logical changes
- Use feature branches for development
- Rebase before merging to main
- Tag releases with semantic versioning (v1.0.0)

### CI/CD

- GitHub Actions workflow for automated testing and releases
- Run tests on all PRs
- Automated releases when tags are pushed
- Cross-platform binary distribution

## Agent Instructions

When working on this codebase:

1. **Always run tests** after making changes
2. **Follow existing patterns** in similar files
3. **Add tests** for new functionality
4. **Update documentation** when changing interfaces
5. **Check for lint errors** before completing work
6. **Use the Makefile** commands for common tasks
7. **Respect the internal package boundary** - don't export internal types

For questions about implementation details, refer to the GUFF-Design Document.md for architecture decisions and component specifications.