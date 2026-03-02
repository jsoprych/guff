# Changelog

## v0.1.0 -- Community Edition MVP (2026-03-01)

Initial release of guff Community Edition.

### Core

- Local LLM inference via llama.cpp (yzma FFI bindings)
- GGUF model loading with GPU acceleration (CUDA, Metal, Vulkan auto-detected)
- Full sampler chain: temperature, top-k, top-p, min-p, repeat/frequency/presence penalties
- Distribution sampling (`SamplerInitDist`) for proper probabilistic output
- Streaming generation via buffered channels

### CLI

- `guff pull` -- Download models from HuggingFace with progress reporting
- `guff run` -- Single-prompt generation with `--stream` support and stdin input
- `guff chat` -- Interactive chat with SQLite session persistence
  - `/status`, `/clear`, `/exit` commands
  - Compact context status line after each response
  - `--system` and `--system-file` flags for custom system prompts
  - `--session` to resume sessions, `--list-sessions` to list them
  - `--no-persist` for in-memory-only mode
- `guff serve` -- HTTP API server

### Provider Routing

- Unified `Provider` interface for local and remote backends
- `Router` with resolution: explicit routes -> provider prefix -> fallback
- Built-in providers: local (llama.cpp), OpenAI, Anthropic, DeepSeek
- `openai-compatible` type for any OpenAI-protocol API (Together, Groq, etc.)
- Model route aliases in config
- API key `${ENV_VAR}` expansion

### API Server

- Ollama-compatible endpoints: `/api/tags`, `/api/generate`, `/api/chat`
- OpenAI-compatible endpoints: `/v1/chat/completions`, `/v1/completions`, `/v1/models`
- Both streaming (SSE) and non-streaming
- Provider routing on model field (e.g., `openai/gpt-4o`)

### Context Management

- Pluggable `ContextStrategy` interface
- `SlidingWindowStrategy` (default): keeps newest messages within token budget
- `FailStrategy`: error on overflow (for testing)
- Real token counting via yzma vocabulary
- Context budget calculation: `contextSize - maxGenTokens`
- Live context status display in chat

### MCP & Tools

- Tool registry with definitions, handlers, and execution
- MCP client (stdio JSON-RPC 2.0): initialize, list tools, call tools
- Tool call parser for local model output (markdown JSON blocks, raw JSON)
- `FormatForPrompt()` for injecting tool descriptions into system prompts

### Prompt Builder

- Multi-part system prompt assembly (base, project, tools, user sections)
- Auto-discovery: `.guff/prompt.md` (walks up from CWD), `user-prompt.txt`
- Per-model section overrides
- Runtime section injection for tools/MCP context

### Configuration

- Viper-based YAML config with XDG directories
- `GUFF_` environment variable prefix
- Provider configs, model routes, MCP server configs
- System prompt resolution with 5-level priority chain

### Bug Fixes

- **Fixed top-p sampling**: Was using `SamplerInitGreedy()` as terminal sampler regardless of temperature, making all probabilistic sampling ineffective. Now uses `SamplerInitDist(seed)` when temperature > 0.
- **Fixed context budget**: Was passing generation token budget as context budget, allowing prompt + generation to exceed context window. Now correctly computes `contextSize - maxGenTokens`.

---

## v0.0.1 -- Initial Commit (2026-02-28)

Project skeleton with basic model loading and generation.
