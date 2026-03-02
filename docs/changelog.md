# Changelog

## v0.1.3 -- Engine, UI, Samplers, Embeddings & LoRA (2026-03-02)

### Chat Engine

- New `internal/engine/` package: `ChatEngine` unifies completion + tool-call loop
- Automatic tool call parsing → execution → re-call cycle (max 5 rounds)
- Tool description injection into system prompt when tools are registered
- Single entry point for both streaming and non-streaming completions

### Chat UI

- Embedded SPA at `/ui` with inline CSS/JS (dark theme, responsive)
- Streaming chat via SSE to `/v1/chat/completions`
- Model selector from `/v1/models`, parameter sliders (temp, top_p, top_k, max_tokens)
- Dashboard sidebar: server status (`/api/status`), tool list (`/api/tools`)
- Tool call visualization in assistant messages

### Extended Sampler Chain (12 stages)

- **New samplers wired:** Grammar (GBNF), LogitBias, Typical-P, Top-N-Sigma, DRY (anti-repetition), XTC
- **Chain order:** Grammar → LogitBias → Temp → TopK → TopP → TypicalP → MinP → TopNSigma → Penalties → DRY → XTC → Terminal
- **Removed:** Mirostat field (no individual yzma init function exists)
- Config fields: `typical_p`, `top_n_sigma`, `dry_multiplier`, `dry_base`, `dry_allowed_len`, `dry_penalty_last`, `grammar`

### Embeddings

- `Generator.Embed()` for vector embedding generation with average pooling
- `POST /v1/embeddings` endpoint (OpenAI-compatible format)
- Supports string or array input

### LoRA Support

- Config: `lora.path` and `lora.scale` (default 1.0)
- Loaded automatically at model load time via `AdapterLoraInit` + `SetAdaptersLora`
- Cleaned up on model unload/switch

### Model Metadata

- `LoadedModel` now exposes: `VocabSize`, `GetMetadata(key)`, `HasEncoder()`, `IsRecurrent()`
- Model warmup (`llama.Warmup`) called after loading for GPU kernel JIT compilation

### API Endpoints

- `GET /ui` -- embedded chat/dashboard SPA
- `GET /api/status` -- server uptime, model, tool count, provider count
- `GET /api/tools` -- registered tool definitions
- `POST /v1/embeddings` -- OpenAI-compatible embeddings

### Tests

- 7 engine unit tests (mock provider with response queue)
- 10 API smoke tests (httptest + mock engine)
- 51 tests total across 9 packages, all passing

### Dead Code Cleanup

- Removed `Mirostat` field from `GenerationOptions`
- Extracted `freeModel()` helper for LoRA-aware resource cleanup

### Yzma Utilization

- Increased from ~19% to ~33% (31 → 66 of ~199 exported functions)

---

## v0.1.2 -- Model Persistence in Serve (2026-03-01)

### Model Persistence

- `guff serve` now keeps models loaded in memory across API requests
- `Load()` is idempotent -- returns cached model if already loaded, only reloads on model switch
- `Unload()` is a no-op during serve mode (`keepLoaded` flag)
- Pre-loads default model (or first available) at server startup
- Clean shutdown via `ForceUnload()` on SIGTERM/SIGINT
- **Performance**: API request latency drops from ~3-5s to <100ms (no load/unload per request)

---

## v0.1.1 -- Model Metadata & Chat Templates (2026-03-01)

### Model Metadata

- Read model metadata from GGUF via yzma at load time: `NCtxTrain`, `NEmbd`, `NLayer`, `NHead`, `ModelDesc`, `ModelSize`, `ChatTemplate`
- Context window size now auto-detected from model instead of hardcoded 2048
- Verbose output shows full model metadata (context length, layers, embedding dimensions)

### Chat Templates

- Auto-detect and apply chat templates from GGUF model metadata
- Uses `llama.ChatApplyTemplate()` for proper template rendering (ChatML, Llama-style, etc.)
- Graceful fallback to simple `Role: content` format when no template is present

### Documentation

- Added mermaid diagrams to architecture docs (system overview, data flows)
- Updated known issues: context window detection and chat template now resolved

---

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
