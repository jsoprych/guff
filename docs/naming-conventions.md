# Naming Conventions

Go interfaces are the canonical contract. MCP tools, HTTP routes, and CLI commands are mechanical projections of the same underlying operations.

## The Projection Rule

Every Go interface method maps deterministically to four transports:

| Transport | Pattern | Case | Example |
|-----------|---------|------|---------|
| **Go method** | `{Interface}.{Verb}{Noun}` | PascalCase | `MemoryProvider.Search` |
| **MCP tool** | `{ns}_{verb}` | snake_case | `memory_search` |
| **HTTP route** | `/{ns}/{verb}` | REST | `POST /memory/search` |
| **CLI command** | `guff {ns} {verb}` | kebab-case | `guff memory search` |

The Go method is always the source of truth. Everything else is derived.

## Namespace Mapping

Each Go interface maps to a namespace used across all transports:

| Go Interface | Namespace | Package | Notes |
|---|---|---|---|
| `MemoryProvider` | `memory` | `internal/chat/storage` (future) | Primary MCP target |
| `Storage` | `session` | `internal/chat/storage` | Session/message CRUD |
| `ContextManager` | `context` | `internal/chat/context` | Context window ops |
| `StateManager` | `state` | `internal/chat/state` | KV-cache persistence |
| `Provider` | `provider` | `internal/provider` | LLM provider ops |
| `Embedder` | `embed` | `internal/generate` | Embedding generation |

Namespace constants live in `internal/adapter/namespace.go`.

## Complete Projection Table

### `memory` namespace (MemoryProvider)

| Go Method | MCP Tool | HTTP Route | CLI |
|-----------|----------|------------|-----|
| `Store()` | `memory_store` | `POST /memory/store` | `guff memory store` |
| `Search()` | `memory_search` | `POST /memory/search` | `guff memory search` |
| `Recall()` | `memory_recall` | `POST /memory/recall` | `guff memory recall` |
| `Forget()` | `memory_forget` | `POST /memory/forget` | `guff memory forget` |

### `session` namespace (Storage)

| Go Method | MCP Tool | HTTP Route | CLI |
|-----------|----------|------------|-----|
| `CreateSession()` | `session_create` | `POST /session/create` | `guff session create` |
| `GetSession()` | `session_get` | `GET /session/get` | `guff session get` |
| `ListSessions()` | `session_list` | `GET /session/list` | `guff session list` |
| `DeleteSession()` | `session_delete` | `POST /session/delete` | `guff session delete` |
| `AddMessage()` | `session_add_message` | `POST /session/add-message` | `guff session add-message` |
| `GetMessages()` | `session_get_messages` | `GET /session/get-messages` | `guff session get-messages` |
| `DeleteMessages()` | `session_delete_messages` | `POST /session/delete-messages` | `guff session delete-messages` |
| `CountMessages()` | `session_count_messages` | `GET /session/count-messages` | `guff session count-messages` |

### `context` namespace (ContextManager)

| Go Method | MCP Tool | HTTP Route | CLI |
|-----------|----------|------------|-----|
| `AddMessage()` | `context_add_message` | `POST /context/add-message` | `guff context add-message` |
| `GetContext()` | `context_get` | `GET /context/get` | `guff context get` |
| `ClearContext()` | `context_clear` | `POST /context/clear` | `guff context clear` |
| `TokenCount()` | `context_token_count` | `GET /context/token-count` | `guff context token-count` |
| `SetStrategy()` | `context_set_strategy` | `POST /context/set-strategy` | `guff context set-strategy` |
| `GetStatus()` | `context_status` | `GET /context/status` | `guff context status` |

### `state` namespace (StateManager)

| Go Method | MCP Tool | HTTP Route | CLI |
|-----------|----------|------------|-----|
| `SaveState()` | `state_save` | `POST /state/save` | `guff state save` |
| `LoadState()` | `state_load` | `POST /state/load` | `guff state load` |
| `CleanupSession()` | `state_cleanup` | `POST /state/cleanup` | `guff state cleanup` |

### `provider` namespace (Provider)

| Go Method | MCP Tool | HTTP Route | CLI |
|-----------|----------|------------|-----|
| `ChatCompletion()` | `provider_chat` | `POST /provider/chat` | `guff provider chat` |
| `ChatCompletionStream()` | `provider_chat_stream` | `POST /provider/chat-stream` | `guff provider chat-stream` |
| `ListModels()` | `provider_list_models` | `GET /provider/list-models` | `guff provider list-models` |

### `embed` namespace (Embedder)

| Go Method | MCP Tool | HTTP Route | CLI |
|-----------|----------|------------|-----|
| `Embed()` | `embed_generate` | `POST /embed/generate` | `guff embed generate` |

## Compound Verb Simplification

When projecting Go method names to snake_case, apply these rules:

1. **Drop the noun if the namespace already implies it.** `ContextManager.GetContext()` becomes `context_get`, not `context_get_context`.
2. **Drop `Get` prefix for status/info methods.** `GetStatus()` becomes `context_status`.
3. **Preserve compound nouns that add meaning.** `AddMessage()` keeps the noun: `session_add_message`, because "session add" alone is ambiguous.
4. **Use `_` to separate multi-word verbs.** `ChatCompletion()` becomes `provider_chat`, `ChatCompletionStream()` becomes `provider_chat_stream`.

## What Gets Exposed

Not every Go method needs a tool/route/command. The rule:

| Criteria | Exposed? | Example |
|----------|----------|---------|
| User-facing operation | Yes | `memory_search`, `session_list` |
| Internal lifecycle | No | `Close()`, `init()`, `Name()` |
| Maintenance/admin | CLI only | `state_cleanup` |
| Read-only query | All transports | `context_status` |

Methods like `Close()`, `Name()`, and `CleanupOldStateFiles()` are internal plumbing -- they don't project to any transport.

## External Tools Keep Upstream Names

Tools from external MCP servers (filesystem, GitHub, SQLite, etc.) keep the names their servers define. The naming convention applies only to **guff-native tools** -- tools that project Go interface methods.

```
# External tools — keep upstream names
filesystem_list, filesystem_read, filesystem_write
github_create_issue, github_search_repos

# Guff-native tools — follow the convention
memory_store, memory_search, session_list, context_status
```

This avoids confusion when reading MCP server documentation and ensures compatibility with other MCP clients.

## Compatibility: Grandfathered Routes

Existing routes and commands predate this convention and are **grandfathered**:

### HTTP Routes (grandfathered)

| Route | Origin | Status |
|-------|--------|--------|
| `/v1/chat/completions` | OpenAI compatibility | Permanent |
| `/v1/completions` | OpenAI compatibility | Permanent |
| `/v1/models` | OpenAI compatibility | Permanent |
| `/v1/embeddings` | OpenAI compatibility | Permanent |
| `/api/tags` | Ollama compatibility | Permanent |
| `/api/generate` | Ollama compatibility | Permanent |
| `/api/chat` | Ollama compatibility | Permanent |
| `/api/pull` | Ollama compatibility | Permanent |
| `/api/status` | Dashboard | Permanent |
| `/api/tools` | Dashboard | Permanent |
| `/ui` | Dashboard SPA | Permanent |

### CLI Commands (grandfathered)

| Command | Status |
|---------|--------|
| `guff pull` | Permanent |
| `guff run` | Permanent |
| `guff chat` | Permanent |
| `guff serve` | Permanent |

New endpoints and commands follow the convention. Grandfathered routes are never removed.

## The Adapter Package

`internal/adapter/` provides generic Go→MCP bridging:

- **`Wrap[A, R]()`** -- Typed input/output function → MCP tool handler
- **`WrapVoid[A]()`** -- Typed input, no output → MCP tool handler (returns `"ok"`)
- **`ToolName()`** -- Builds conventional `{ns}_{verb}` or `{ns}_{verb}_{noun}` names
- **`RegisterAll()`** -- Batch-registers tool specs into `tools.Registry`

See [Architecture](architecture.md) for how the adapter fits into the system.
