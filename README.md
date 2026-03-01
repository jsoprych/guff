# guff - Local LLM Runtime

guff is a local LLM runtime in Go with llama.cpp backend, providing Ollama-like functionality with optimizations for IBM Granite models. Single-binary CLI tool and API server.

## Project Abstract

**guff** brings powerful language model capabilities directly to your local machine, offering a lightweight, privacy-focused alternative to cloud-based AI services. Designed for developers, researchers, and enthusiasts who need reliable, offline-accessible AI without compromising on performance or ease of use.

### Why guff?

- **🚀 Simple & Self-contained**: A single binary with no complex dependencies. Download, run, and immediately start interacting with models.
- **🔒 Privacy by Default**: All processing happens locally—your data never leaves your machine. Ideal for sensitive applications, proprietary code, or confidential documents.
- **💡 Optimized for Granite**: While guff works with any GGUF-format model, it includes special optimizations for IBM's Granite family of models, known for their efficiency and strong code-generation capabilities.
- **⚙️ Developer-Friendly**: Clean CLI, programmable API, and straightforward configuration. Integrate local AI into your scripts, tools, or applications with minimal friction.
- **🌐 Ollama-Compatible API**: Offers a familiar REST API that matches Ollama's endpoints, making migration or integration with existing tooling seamless.

### Who is it for?

- **Developers** who want to add AI features to their applications without relying on external APIs
- **Data Scientists & Researchers** needing reproducible, offline experimentation with language models
- **Privacy-Conscious Users** who prefer keeping their prompts and data entirely local
- **Hobbyists & Tinkerers** exploring the world of local AI with a simple, yet powerful tool

### Core Features

- **Model Management**: Download, list, and manage GGUF models directly from Hugging Face
- **Text Generation**: Complete prompts with configurable parameters (temperature, top-p/k, stop sequences)
- **Interactive Chat**: Engage in multi-turn conversations with full history and context
- **HTTP API Server**: Serve model endpoints over REST for remote access or integration
- **Streaming Output**: Receive tokens in real-time for responsive user experiences
- **Cross-Platform**: Runs on Linux, macOS, and Windows with consistent behavior

Whether you're building the next generation of AI-powered tools, conducting private research, or simply curious about running large language models on your own hardware, guff provides a robust, accessible foundation for local AI experimentation and deployment.

## Quick Start

### Prerequisites

1. **Go 1.25+** (tested with 1.25.0)

2. **Hugging Face token** (for model downloads):
   ```bash
   export GUFF_HUGGINGFACE_TOKEN="hf_xxxxxxxxxxxxxxxxxxxx"
   # Add to ~/.bashrc for persistence
   ```

### Build

```bash
make build
# or
go build -o guff .
```

### Download a model

```bash
# Download granite-3b (2GB, Q4_K_M quantization)
./guff pull granite-3b
# or specify quantization
./guff pull granite-3b --quantization Q4_K_M
```

### Run

```bash
# One-off completion
./guff run "Hello, world"

# Interactive chat
./guff chat "Hello"

# Start API server
./guff serve
```

## Current Status

### ✅ Accomplished (Phase 1)

- **Project skeleton**: Go module, directory structure (`cmd/`, `internal/`, `pkg/`)
- **CLI framework**: Cobra+Viper with `pull`, `run`, `chat`, `serve` commands
- **Configuration**: XDG-based paths with fallback to `./models/`
- **Model manager**: Registry, lifecycle interfaces using `hybridgroup/yzma` bindings
- **Hugging Face downloader**: HTTP client with auth, redirect handling, progress reporting
- **Model download**: Granite-3b Q4_K_M downloaded successfully (~2GB GGUF)
- **Library auto-download**: Auto-downloads llama.cpp precompiled binaries via yzma
- **Model loading verified**: Successfully loads Granite-3b model with metadata extraction
- **Generation engine**: Text generation with configurable parameters (temperature, top‑p/k, repeat penalty)
- **Streaming generation**: Real‑time token output for both CLI (`--stream`) and HTTP API
- **CLI commands**: 
  - `guff pull` - Download models from Hugging Face
  - `guff run` - Single‑prompt generation with streaming support
  - `guff chat` - Interactive chat session
  - `guff serve` - Start HTTP API server
- **HTTP API server**: REST API with Ollama‑compatible endpoints:
  - `GET /api/tags` – list available models
  - `POST /api/generate` – text generation (supports streaming)
  - `POST /api/chat` – chat completion (supports streaming)

### 🔄 In Progress

- **Model persistence**: Keep loaded models in memory across requests
- **GPU acceleration**: Configure yzma for CUDA/Metal support
- **Sampler configuration improvements**: Add distribution sampler for true random sampling with temperature

### 🚧 Known Issues

1. **Code-instruct model**: The default model (granite-3b-code-instruct) is optimized for code generation, not general chat. Natural language prompts produce code-like output.
2. **Performance**: First load takes ~3.5s, generation ~1s for few tokens (CPU-only).
3. **Limited parameters**: Temperature, top‑p/k, repeat penalty implemented using greedy sampler (deterministic). Missing distribution sampler for true random sampling.
4. **No GPU acceleration**: Currently CPU-only; yzma supports CUDA/Metal but not configured.

### 📋 Next Steps (Priority)

1. **Model persistence** – Keep loaded models in memory across API requests to reduce latency
2. **GPU acceleration** – Configure yzma for CUDA/Metal support
3. **Distribution sampler** – Add random sampling with temperature for non‑deterministic outputs
4. **Chat template improvements** – Use model‑specific templates (e.g., Granite code‑instruct format)
5. **Performance optimizations** – Benchmark and optimize loading/generation speed

## Technical Details

### Architecture

guff uses `hybridgroup/yzma` (v1.10.0) for llama.cpp bindings, which provides pure Go FFI bindings and auto-downloads precompiled llama.cpp libraries. This eliminates the need for manual llama.cpp header installation.

### Project Structure

```
guff/
├── cmd/                    # CLI commands
│   ├── root.go            # Root command & config
│   ├── pull.go            # Model download from Hugging Face
│   ├── run.go             # Single prompt generation
│   ├── chat.go            # Interactive chat
│   └── serve.go           # API server (Phase 2)
├── internal/              # Private application code
│   ├── model/             # Model management (load/unload)
│   ├── generate/          # Text generation engine
│   ├── config/            # Configuration management
│   ├── llama/             # Library auto-download (ensure.go)
│   └── api/               # HTTP API (Phase 2)
├── pkg/                   # Public libraries (future)
├── models/                # Downloaded GGUF models
├── test/                  # Test programs
│   ├── load/             # Model loading test
│   └── generate/         # Generation test
├── main.go                # Application entry point
├── go.mod                 # Go module definition
├── Makefile               # Build automation
└── README.md              # This file
```

### Dependencies

- **`github.com/hybridgroup/yzma`** - llama.cpp Go bindings (auto-downloads libraries)
- **`github.com/spf13/cobra`** - CLI framework
- **`github.com/spf13/viper`** - Configuration
- **`github.com/go-chi/chi`** - HTTP router (for Phase 2 API)

## Development

See [AGENTS.md](AGENTS.md) for detailed development guidelines, code style, and testing procedures.

```bash
# Build
make build

# Run tests
make test

# Format code
make fmt

# Lint
make lint

# Clean
make clean
```

## Testing

```bash
# Test model loading
go run test/load/main.go

# Test generation
go run test/generate/main.go
```

## Configuration

guff uses XDG directories by default:
- **Config**: `~/.config/guff/config.yaml`
- **Data**: `~/.local/share/guff/` (models, libraries)
- **Cache**: `~/.cache/guff/`

Environment variables:
- `GUFF_HUGGINGFACE_TOKEN` - Hugging Face API token (required for downloads)
- `YZMA_LIB` - Path to llama.cpp libraries (auto-managed)

## License

Apache 2.0 (see LICENSE file)