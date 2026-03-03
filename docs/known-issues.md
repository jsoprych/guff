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

### ~~Mirostat Not Implemented~~ (REMOVED)

~~The `Mirostat` field exists in `GenerationOptions`.~~ Removed. Yzma does not expose an individual `SamplerInitMirostat` function. Mirostat is not available as a standalone sampler in the current yzma API.

### ~~Grammar Constraints Not Exposed~~ (FIXED)

~~Yzma provides `SamplerInitGrammar()` for BNF/GBNF grammar-constrained generation, but guff doesn't expose it yet.~~ Grammar constraints are now wired via `generate.grammar` config and the `Grammar` field in `GenerationOptions`. Applied as the first sampler in the 12-stage chain.

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

### ~~`/api/pull` Not Implemented~~ (FIXED)

`POST /api/pull` is now implemented. It streams NDJSON download progress and maps to the same HuggingFace downloader used by `guff pull`.

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

### ~~No Model Caching Across Requests~~ (FIXED)

~~Each API request loads and unloads the model.~~ `guff serve` now enables `keepLoaded` mode -- models stay in memory across requests. `Load()` is idempotent (returns the cached model if already loaded). Model switches still incur a full load/unload cycle.

## Storage

### SQLite Concurrency

SQLite storage is not optimized for concurrent access from multiple processes. Running multiple `guff chat` sessions on the same database may cause locking errors.

**Workaround:** Use `--no-persist` for concurrent sessions, or use different session IDs.

## Yzma Integration

### ~33% API Coverage

guff uses approximately 66 of yzma's ~199 exported functions (~33%). Major capabilities:

| Feature | Yzma Function | Status |
|---------|---------------|--------|
| Model metadata | `NCtxTrain`, `NEmbd`, `NLayer`, `ModelDesc` | **Used** (v0.1.1) |
| Chat templates | `ModelChatTemplate`, `ChatApplyTemplate` | **Used** (v0.1.1) |
| GGUF key-value metadata | `ModelMetaValStr`, `ModelHasEncoder`, `ModelIsRecurrent` | **Used** (v0.1.3) |
| Embeddings | `SetEmbeddings`, `Decode`, `GetEmbeddings` | **Used** (v0.1.3) |
| Grammar constraints | `SamplerInitGrammar` | **Used** (v0.1.3) |
| LoRA adapters | `AdapterLoraInit`, `SetAdaptersLora`, `AdapterLoraFree` | **Used** (v0.1.3) |
| Extended samplers | `SamplerInitTypical`, `SamplerInitTopNSigma`, `SamplerInitDry`, `SamplerInitXTC`, `SamplerInitLogitBias` | **Used** (v0.1.3) |
| Model warmup | `Warmup` | **Used** (v0.1.3) |
| Vocab utilities | `VocabNTokens`, `VocabType`, `TokenBOS` | **Used** (v0.1.3) |
| Performance metrics | `PerfContextReset` | **Used** (v0.1.3) |
| KV cache management | `KvCacheSeqRm`, `KvCacheSeqCp` | Not used |
| Batch processing | `BatchInit`, `BatchAdd` | Partially used |
