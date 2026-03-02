# API Reference

guff serves two API dialects simultaneously when running `guff serve`.

## OpenAI-Compatible API

### POST /v1/chat/completions

Chat completion with support for streaming.

**Request:**
```json
{
  "model": "granite-3b",
  "messages": [
    {"role": "system", "content": "You are helpful."},
    {"role": "user", "content": "Hello"}
  ],
  "temperature": 0.8,
  "top_p": 0.9,
  "max_tokens": 1024,
  "max_completion_tokens": 1024,
  "stop": ["\n"],
  "seed": 42,
  "stream": false
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | Yes | Model name (supports provider routing) |
| `messages` | array | Yes | Array of `{role, content}` objects |
| `temperature` | float | No | Sampling temperature |
| `top_p` | float | No | Nucleus sampling threshold |
| `max_tokens` | int | No | Max tokens to generate (default 1024) |
| `max_completion_tokens` | int | No | Alias for max_tokens |
| `stop` | string or array | No | Stop sequences |
| `seed` | int | No | Random seed |
| `stream` | bool | No | Enable SSE streaming |
| `n` | int | No | Number of completions (only n=1 supported) |

**Response (non-streaming):**
```json
{
  "id": "chatcmpl-1234567890",
  "object": "chat.completion",
  "created": 1709312345,
  "model": "granite-3b",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "Hello! How can I help?"},
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 8,
    "total_tokens": 18
  }
}
```

**Response (streaming):**

SSE events with `data: ` prefix:
```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1709312345,"model":"granite-3b","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1709312345,"model":"granite-3b","choices":[{"index":0,"delta":{"role":"assistant","content":"!"},"finish_reason":"stop"}]}

data: [DONE]
```

### POST /v1/completions

Legacy text completion endpoint.

**Request:**
```json
{
  "model": "granite-3b",
  "prompt": "Once upon a time",
  "max_tokens": 256,
  "temperature": 0.8,
  "stream": false
}
```

**Response:**
```json
{
  "id": "cmpl-1234567890",
  "object": "text_completion",
  "created": 1709312345,
  "model": "granite-3b",
  "choices": [
    {
      "index": 0,
      "text": " there was a programmer...",
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 5,
    "completion_tokens": 10,
    "total_tokens": 15
  }
}
```

### GET /v1/models

List available models from all providers.

**Response:**
```json
{
  "object": "list",
  "data": [
    {"id": "granite-3b", "object": "model", "created": 1709312345, "owned_by": "local"},
    {"id": "gpt-4o", "object": "model", "created": 1709312345, "owned_by": "openai"}
  ]
}
```

Models are aggregated from all registered providers plus local models. Duplicates are deduplicated by ID.

## Ollama-Compatible API

### GET /api/tags

List available local models.

**Response:**
```json
{
  "models": [
    {"name": "granite-3b", "size": 2147483648, "digest": "abc123"}
  ]
}
```

### POST /api/generate

Text generation from a prompt.

**Request:**
```json
{
  "model": "granite-3b",
  "prompt": "Hello, world",
  "stream": true,
  "temperature": 0.8,
  "top_p": 0.9,
  "top_k": 40,
  "max_tokens": 512,
  "stop": ["\n"],
  "seed": 42
}
```

**Response (non-streaming):**
```json
{
  "model": "granite-3b",
  "created_at": "2024-01-01T00:00:00Z",
  "response": "Generated text here",
  "done": true,
  "total_tokens": 15
}
```

**Response (streaming):**

SSE events:
```
data: {"model":"granite-3b","created_at":"...","response":"Hello","done":false}
data: {"model":"granite-3b","created_at":"...","response":"Hello world","done":true}
```

### POST /api/chat

Chat completion with message history.

**Request:**
```json
{
  "model": "granite-3b",
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "stream": false,
  "options": {
    "temperature": 0.8,
    "top_p": 0.9,
    "max_tokens": 1024
  }
}
```

**Response:**
```json
{
  "model": "granite-3b",
  "created_at": "2024-01-01T00:00:00Z",
  "message": {"role": "assistant", "content": "Hi there!"},
  "done": true,
  "total_tokens": 12
}
```

### POST /api/pull

Download a model. **Not yet implemented** (returns 501).

## Health Endpoints

### GET /

Returns `guff API server` (plain text).

### GET /health

```json
{"status": "ok"}
```

## Error Responses

### OpenAI format

```json
{
  "error": {
    "message": "model is required",
    "type": "invalid_request_error",
    "code": ""
  }
}
```

### Ollama format

HTTP error with plain text body.

## Provider Routing in API

The model field in all endpoints supports provider routing:

```bash
# Local model
curl -d '{"model": "granite-3b", ...}' http://localhost:8080/v1/chat/completions

# Route to OpenAI
curl -d '{"model": "openai/gpt-4o", ...}' http://localhost:8080/v1/chat/completions

# Route via alias
curl -d '{"model": "sonnet", ...}' http://localhost:8080/v1/chat/completions
```

## Middleware

The API server uses Chi middleware:
- **RequestID** -- Unique ID per request
- **RealIP** -- Extract client IP from proxy headers
- **Logger** -- Request logging
- **Recoverer** -- Panic recovery
- **Timeout** -- 60-second request timeout

CORS header `Access-Control-Allow-Origin: *` is set on streaming responses.
