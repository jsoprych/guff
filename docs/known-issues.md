# Known Issues & Limitations

## Current Issues

### Model Loading

**Single model at a time.** The `ModelManager` holds one loaded model. API requests that specify different models cause load/unload cycles. This adds latency and prevents concurrent serving of multiple models.

**Impact:** High latency on model switches (~3.5s for granite-3b on CPU).
**Workaround:** Use a single model per `guff serve` instance, or pin a default model.
**Future fix:** Model pool with LRU eviction.

### Code-Instruct Models

**Granite-3b-code-instruct is optimized for code, not chat.** Natural language prompts may produce code-formatted output. This is expected behavior for the model, not a guff bug.

**Workaround:** Use a chat-tuned model, or set a system prompt that guides output format.

### ~~Context Window Detection~~ (FIXED)

~~Context window size was hardcoded to 2048.~~ Now reads `n_ctx_train` from model metadata via yzma at load time. Models with larger context windows are automatically detected and used.

### ~~Chat Template~~ (FIXED)

~~No model-specific chat templates.~~ Now reads the chat template from GGUF metadata and applies it via `llama.ChatApplyTemplate()`. Falls back to simple `Role: content` format only if no template is found in the model.

## Sampling

### Mirostat Not Implemented

The `Mirostat` field exists in `GenerationOptions` but is not wired to any sampler. Yzma supports mirostat sampling.

### Grammar Constraints Not Exposed

Yzma provides `SamplerInitGrammar()` for BNF/GBNF grammar-constrained generation, but guff doesn't expose it yet. This would enable reliable structured output (JSON, function calls).

## MCP / Tools

### Stdio Transport Only

The MCP client only supports stdio transport (launching a child process). HTTP/SSE transport is not implemented.

### No Parallel Tool Calls

Tool calls are executed sequentially. If a model requests multiple tools in one turn, they run one at a time.

### Local Model Tool Calling Reliability

Local models produce tool calls via text output (JSON in markdown blocks). Without grammar constraints, models may produce malformed JSON or tool calls in unexpected formats.

**Workaround:** Use frontier models (OpenAI, Anthropic) for tool-heavy workloads.
**Future fix:** Grammar-constrained generation for tool call output.

## API Server

### `/api/pull` Not Implemented

The Ollama-compatible pull endpoint returns 501. Model downloads must be done via `guff pull` CLI.

### No Authentication

The API server has no authentication. Anyone who can reach the port can make requests.

**Workaround:** Bind to `127.0.0.1` (default) and use a reverse proxy for remote access.

### No Rate Limiting

No built-in rate limiting on API endpoints.

## Provider Routing

### No Health Checks

Provider availability is not checked proactively. If a remote API is down, you get an error on the first request.

### No Retry Logic

Failed remote API calls are not retried. Network blips cause immediate errors.

### Anthropic Model List

The Anthropic provider returns a hardcoded model list since Anthropic's API has no list models endpoint. New models require a code update.

## Performance

### CPU-Only Default

While GPU acceleration is automatic when drivers are present, the default configuration doesn't verify GPU availability. Users may unknowingly run on CPU.

**Workaround:** Check `guff serve --verbose` output for GPU detection messages.
**Future fix:** Report GPU status in `guff --version` and `/health` endpoint.

### No Model Caching Across Requests

Each API request loads and unloads the model (unless kept in memory by the manager). This is the primary latency bottleneck for the API server.

## Storage

### SQLite Concurrency

SQLite storage is not optimized for concurrent access from multiple processes. Running multiple `guff chat` sessions on the same database may cause locking errors.

**Workaround:** Use `--no-persist` for concurrent sessions, or use different session IDs.

## Yzma Integration

### ~20% API Coverage

guff uses approximately 43 of yzma's 180+ exported functions. Major unused capabilities:

| Feature | Yzma Function | Status |
|---------|---------------|--------|
| Model metadata | `NCtxTrain`, `NEmbd`, `NLayer`, `ModelDesc` | **Used** (v0.1.1) |
| Chat templates | `ModelChatTemplate`, `ChatApplyTemplate` | **Used** (v0.1.1) |
| GGUF key-value metadata | `ModelMetaValStr`, `ModelMetaCount` | Not used |
| Embeddings | `Encode`, `GetEmbeddings` | Not used |
| Grammar constraints | `SamplerInitGrammar` | Not used |
| LoRA adapters | `LoraAdapterInit`, `SetLoraAdapter` | Not used |
| KV cache management | `KvCacheSeqRm`, `KvCacheSeqCp` | Not used |
| Performance metrics | `GetTimings`, `PerfContextReset` | Not used |
| Batch processing | `BatchInit`, `BatchAdd` | Partially used |
| Vocab utilities | `VocabType`, `NVocab`, `TokenBOS` | Partially used |
