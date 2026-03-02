# Providers

guff uses a unified `Provider` interface to route requests to local llama.cpp inference or remote APIs. The `Router` dispatches requests based on model name syntax.

## Provider Interface

All providers implement:

```go
type Provider interface {
    Name() string
    ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
    ListModels(ctx context.Context) ([]ModelInfo, error)
}
```

## Model Resolution

The Router resolves model names in this order:

1. **Explicit route table** -- configured aliases (e.g., `sonnet` -> `anthropic/claude-sonnet-4-5-20250929`)
2. **Provider prefix** -- `provider/model` syntax (e.g., `openai/gpt-4o`)
3. **Fallback** -- local llama.cpp provider (always registered)

```bash
# These all work:
guff chat --model granite-3b              # local (fallback)
guff chat --model openai/gpt-4o          # prefix routing
guff chat --model deepseek/deepseek-chat  # prefix routing
guff chat --model sonnet                  # route alias (if configured)
```

## Built-in Providers

### Local (`local`)

Wraps the llama.cpp model manager and generator. Always registered as the fallback provider.

- Messages formatted as `System: ...\nUser: ...\nAssistant:` prompt
- Supports all generation parameters (temperature, top-p/k, min-p, penalties, seed)
- Model loaded on each request, unloaded after completion

**Source:** `internal/provider/local.go`

### OpenAI (`openai`)

Proxies to the OpenAI API (`https://api.openai.com/v1`).

```yaml
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
    # base_url: https://api.openai.com/v1  # default
```

- Supports streaming (SSE) and non-streaming
- Handles `[DONE]` sentinel in stream
- Lists models via `GET /v1/models`

**Source:** `internal/provider/openai.go`

### Anthropic (`anthropic`)

Proxies to the Anthropic Messages API (`https://api.anthropic.com/v1/messages`).

```yaml
providers:
  anthropic:
    type: anthropic
    api_key: ${ANTHROPIC_API_KEY}
```

**Key differences from OpenAI:**
- System messages are extracted to a top-level `system` field (not in messages array)
- Only `user` and `assistant` roles are allowed in messages
- Messages must start with a `user` message (auto-injected if needed)
- Stream events use `content_block_delta` and `message_stop` types
- Models listed as hardcoded set (Anthropic has no list endpoint)

**Source:** `internal/provider/anthropic.go`

### DeepSeek (`deepseek`)

Uses the OpenAI-compatible provider with DeepSeek's base URL (`https://api.deepseek.com/v1`).

```yaml
providers:
  deepseek:
    type: deepseek
    api_key: ${DEEPSEEK_API_KEY}
    # base_url: https://api.deepseek.com/v1  # default
```

**Source:** Reuses `OpenAIProvider` via `NewDeepSeekProvider()`.

### OpenAI-Compatible (`openai-compatible`)

For any API that speaks the OpenAI protocol (Together, Groq, Mistral, local servers, etc.).

```yaml
providers:
  together:
    type: openai-compatible
    api_key: ${TOGETHER_API_KEY}
    base_url: https://api.together.xyz/v1
  groq:
    type: openai-compatible
    api_key: ${GROQ_API_KEY}
    base_url: https://api.groq.com/openai/v1
  local-server:
    type: openai-compatible
    base_url: http://localhost:11434/v1  # e.g. another Ollama instance
```

The provider name in the config becomes the prefix for routing (e.g., `together/meta-llama/Llama-3-70b`).

**Source:** Reuses `OpenAIProvider` via `NewOpenAICompatibleProvider()`.

## Model Route Aliases

Map short names to specific provider/model combinations:

```yaml
model_routes:
  gpt-4o:
    provider: openai
    model: gpt-4o
  sonnet:
    provider: anthropic
    model: claude-sonnet-4-5-20250929
  deepseek-coder:
    provider: deepseek
    model: deepseek-coder
  fast:
    provider: groq
    model: llama-3.1-70b-versatile
```

Usage:
```bash
guff chat --model sonnet
guff run --model fast "Hello"
```

## Router Setup (serve command)

When `guff serve` starts, the router is configured from the config file:

1. Local provider registered as fallback
2. Remote providers registered from `providers:` config
3. Route aliases registered from `model_routes:` config

All API endpoints (`/v1/chat/completions`, `/api/chat`, etc.) route through the same `Router`.

## API Key Security

API keys support `${ENV_VAR}` expansion:

```yaml
providers:
  openai:
    api_key: ${OPENAI_API_KEY}  # resolved at runtime from environment
```

Never put raw API keys in config files. Use environment variables.

## Adding a New Provider

To add support for a new API:

1. If OpenAI-compatible: use `type: openai-compatible` with `base_url` -- no code changes needed
2. If custom protocol: implement the `Provider` interface in a new file under `internal/provider/`
3. Register the provider type in `cmd/serve.go:setupProviderRouter()`
