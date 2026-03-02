# guff

**The local-first AI runtime. Inference, routing, tools -- one binary, zero dependencies.**

guff is the missing runtime layer for AI agents. Run GGUF models locally via llama.cpp with GPU acceleration, route seamlessly to frontier APIs (OpenAI, Anthropic, DeepSeek), serve MCP tool servers, and expose it all through Ollama-compatible and OpenAI-compatible endpoints -- from a single Go binary.

**Built for the agentic stack.** Use guff as the inference backend for [OpenClaw](https://github.com/openclaw/openclaw) agents, MCP-powered tool chains, or your own autonomous systems. Local models + MCP tools + provider routing = agents that run anywhere, call anything, and don't need the cloud.

```bash
# Local inference
guff chat --model granite-3b

# Route to DeepSeek
guff chat --model deepseek/deepseek-chat

# Route to OpenAI
guff chat --model openai/gpt-4o

# Serve as an API (Ollama + OpenAI compatible)
guff serve
```

---

## What Makes guff Different

**One tool, every model.** Local GGUF models and remote APIs share the same interface. Switch between a 3B local model and GPT-4o with a prefix change. No config surgery, no separate tools.

**Drop-in API compatibility.** guff serves both Ollama-compatible AND OpenAI-compatible endpoints simultaneously. Any tool that speaks to Ollama or OpenAI works with guff out of the box.

**MCP-native tool use.** First-class MCP server integration over stdio JSON-RPC. Connect filesystem, database, GitHub, or any MCP-compatible tool server -- guff discovers tools at startup, injects them into the prompt, parses tool calls, executes them, and feeds results back. Your local 3B model gets the same tool-calling loop as Claude.

**Proper sampling.** Full sampler chain with correct ordering: temperature, top-k, top-p, min-p, penalties, distribution sampling. Not just greedy argmax pretending to be random.

**Context that doesn't explode.** Pluggable context management with real token counting, sliding window truncation, and live context status display. You always know how much runway you have left.

---

## Quick Start

```bash
# Build
make build

# Pull a model from HuggingFace
export GUFF_HUGGINGFACE_TOKEN="hf_xxxx"
guff pull granite-3b

# Run a single prompt
guff run "Explain quicksort in 3 sentences"

# Interactive chat with context tracking
guff chat

# Start the API server
guff serve --port 8080
```

## Provider Routing

guff routes model requests to the right backend automatically:

| Syntax | Backend | Example |
|--------|---------|---------|
| `model-name` | Local llama.cpp | `granite-3b` |
| `openai/model` | OpenAI API | `openai/gpt-4o` |
| `anthropic/model` | Anthropic API | `anthropic/claude-sonnet-4-5-20250929` |
| `deepseek/model` | DeepSeek API | `deepseek/deepseek-chat` |
| Custom alias | Configured route | `my-model` -> any provider |

Configure in `~/.config/guff/config.yaml`:

```yaml
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
  anthropic:
    type: anthropic
    api_key: ${ANTHROPIC_API_KEY}
  deepseek:
    type: deepseek
    api_key: ${DEEPSEEK_API_KEY}
  together:
    type: openai-compatible
    api_key: ${TOGETHER_API_KEY}
    base_url: https://api.together.xyz/v1

model_routes:
  gpt-4o:
    provider: openai
    model: gpt-4o
  sonnet:
    provider: anthropic
    model: claude-sonnet-4-5-20250929
```

## API Server

guff serves two API dialects simultaneously:

```bash
guff serve --host 0.0.0.0 --port 8080
```

**OpenAI-compatible** (works with any OpenAI SDK):
```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "granite-3b", "messages": [{"role": "user", "content": "Hello"}]}'
```

**Ollama-compatible** (works with Ollama clients):
```bash
curl http://localhost:8080/api/chat \
  -d '{"model": "granite-3b", "messages": [{"role": "user", "content": "Hello"}]}'
```

Both support streaming (SSE). Both route through the same provider system.

## MCP Tool Use

Connect MCP servers to give models access to tools:

```yaml
# ~/.config/guff/config.yaml
mcp:
  filesystem:
    command: npx
    args: ["-y", "@anthropic/mcp-filesystem", "/home/user/projects"]
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: ${GITHUB_TOKEN}
```

guff discovers tools from MCP servers at startup, injects tool descriptions into the system prompt, parses tool calls from model output, executes them, and feeds results back. Works with both local models and remote APIs.

## Agent Interoperability

guff exposes OpenAI-compatible endpoints, making it a **drop-in local inference backend** for agent frameworks:

- **[OpenClaw](https://github.com/openclaw/openclaw)** -- Point your OpenClaw agent at `http://localhost:8080/v1/` and run fully local, private agent loops with no cloud dependency
- **LangChain / LlamaIndex** -- Any framework that speaks the OpenAI protocol works out of the box
- **Custom agents** -- Build autonomous systems that hot-swap between local and remote models with a single config change

```bash
# Start guff as your agent's inference backend
guff serve --host 0.0.0.0 --port 8080

# Your agent framework connects like it would to OpenAI
OPENAI_BASE_URL=http://localhost:8080/v1 openclaw start
```

Local inference + MCP tools + provider routing = agents that run on your hardware, use your tools, and fall back to frontier APIs only when you choose.

## Chat Features

```
/status    - Show context window usage, token counts, strategy
/clear     - Clear conversation history
/exit      - Exit chat

[12 msgs | 847/1024 tokens | sliding_window]   <- live status after each turn
```

**Context strategies:**
- `sliding_window` (default) -- keeps newest messages within token budget, preserves system messages
- `fail` -- returns error on overflow (for testing/strict budgets)
- More coming: summarization, hybrid approaches

**System prompts** with 5-level priority:
1. `--system` flag
2. `--system-file` flag
3. Per-model config
4. Config default
5. `~/.config/guff/system-prompt.txt`

## Multi-Part Prompt Builder

For advanced prompt engineering, guff supports composable prompt sections:

```yaml
prompt:
  sections:
    - type: base
      content: "You are a helpful coding assistant."
    - type: project
      auto: true          # auto-discovers .guff/prompt.md walking up from CWD
    - type: user
      auto: true          # loads ~/.config/guff/user-prompt.txt
    - type: tools
      auto: true          # injected at runtime from MCP/tool registry
  models:
    granite-3b:
      sections:
        - type: base
          content: "You are a concise coding assistant. Keep responses short."
```

## Architecture

```
guff
 |-- cmd/                    CLI commands (pull, run, chat, serve)
 |-- internal/
 |   |-- model/              Model lifecycle (load/unload/pull via yzma)
 |   |-- generate/           Text generation with full sampler chain
 |   |-- chat/
 |   |   |-- session/        Session management + context budgets
 |   |   |-- context/        Pluggable context strategies + token counting
 |   |   |-- storage/        SQLite persistence
 |   |-- provider/           Unified Provider interface + Router
 |   |   |-- local.go        llama.cpp backend
 |   |   |-- openai.go       OpenAI/DeepSeek/compatible APIs
 |   |   |-- anthropic.go    Anthropic Messages API
 |   |   |-- router.go       Prefix routing + aliases + fallback
 |   |-- tools/              Tool registry + MCP client
 |   |   |-- registry.go     Tool definitions + execution
 |   |   |-- mcp.go          MCP stdio JSON-RPC 2.0 client
 |   |   |-- parser.go       Extract tool calls from model output
 |   |-- prompt/             Multi-part prompt builder
 |   |-- api/                HTTP server (Ollama + OpenAI endpoints)
 |   |-- config/             Viper-based YAML config
```

Built on [yzma](https://github.com/hybridgroup/yzma) (pure Go FFI bindings to llama.cpp). Single binary, no CGo, no header files. **GPU acceleration is automatic** -- CUDA, Metal, and Vulkan backends are auto-detected and the right libraries are downloaded at first run.

## Configuration

guff uses XDG directories:
- **Config**: `~/.config/guff/config.yaml`
- **Data**: `~/.local/share/guff/` (models, libraries)
- **Cache**: `~/.cache/guff/`

All config values can be overridden with `GUFF_` environment variables:
```bash
GUFF_HUGGINGFACE_TOKEN=hf_xxx    # Required for model downloads
GUFF_SERVER_PORT=9090             # Override server port
GUFF_MODEL_DEFAULT_MODEL=granite  # Default model
```

## Development

```bash
make build          # Build binary
make test           # Run all tests
make fmt            # Format code
make lint           # Lint with golangci-lint
go test ./internal/tools -run TestRegistry   # Single test
```

## Author

Created by **John Soprych**, Chief Scientist at [Elko.AI](https://elko.ai), with assistance from The Dark Software Factory.

## License

Apache 2.0
