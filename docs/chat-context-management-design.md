# Chat Context Management System Design

## 1. Overview

The **Chat Context Management System** provides persistent storage, session management, and context‑window handling for guff’s chat interfaces (CLI and HTTP API). It is designed to be **modular** and **pluggable**, with SQLite as the default storage backend, and integrates with yzma’s state‑saving and template‑application features to avoid redundant work.

### Relationship with yzma

- **State persistence** – uses `llama_state_seq_save_file` / `llama_state_seq_load_file` to store/restore KV‑cache for a conversation sequence, eliminating re‑computation of previous tokens.
- **Chat templates** – delegates template rendering to `llama_chat_apply_template` and uses the model’s built‑in template (via `llama_model_chat_template`) when available.
- **Sampler configuration** – relies on yzma’s `DefaultSamplerParams` and `NewSampler` for consistent sampling across sessions.

What this system **adds**:
- **Session & message storage** – SQLite‑backed retention of conversations, metadata, and model references.
- **Context‑window management** – token counting, truncation, and configurable strategies for long conversations.
- **Model switching** – ability to continue a conversation with a different model (with appropriate warnings/limitations).
- **Pluggable backends** – interfaces that allow swapping SQLite for in‑memory, file‑based, or remote storage.

## 2. Goals & Non‑Goals

### Goals

1. **Persistent chat history** – store messages, metadata (timestamps, model used, token counts) and optional KV‑cache state files.
2. **Session isolation** – separate conversations with unique IDs, labels, and configurable retention.
3. **Context‑window awareness** – track token counts, automatically truncate or summarize when the model’s context limit is approached.
4. **Model‑agnostic sessions** – allow switching the underlying model mid‑conversation (with clear caveats).
5. **Performance** – leverage yzma’s state‑saving to avoid re‑processing previous turns.
6. **Pluggable architecture** – define clean interfaces so storage, context strategies, and state managers can be replaced.

### Non‑Goals

1. **Real‑time collaboration** – not designed for multiple concurrent writers to the same session.
2. **Advanced search** – semantic search via embeddings is out of scope for the minimal version (can be added later).
3. **Multi‑user authentication** – session ownership and access control are left to higher layers (e.g., the HTTP API layer).
4. **Automatic summarization** – summarization of old messages requires a separate summarization model; only trivial truncation is provided initially.

## 3. Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         guff CLI / API                       │
└─────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                  ChatSessionManager (facade)                 │
│                                                             │
│  • createSession(model, label) → Session                    │
│  • getSession(sessionID) → Session                         │
│  • listSessions(filter) → []SessionInfo                    │
│  • deleteSession(sessionID)                                 │
└─────────────────────────────────────────────────────────────┘
                               │
                               ├──────────────────────────────┐
                               ▼                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    ContextManager                           │
│                                                             │
│  • addMessage(sessionID, role, content) → (tokenCount, ok) │
│  • getContext(sessionID, maxTokens) → formattedPrompt      │
│  • truncate(sessionID, strategy)                           │
│  • clearContext(sessionID)                                 │
└─────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                       Storage (interface)                    │
│                                                             │
│  • SaveMessage(sessionID, msg) → messageID                 │
│  • LoadMessages(sessionID, limit) → []Message              │
│  • SaveSession(session)                                    │
│  • LoadSession(sessionID) → Session                        │
│  • DeleteSession(sessionID)                                │
└─────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                     SQLiteStorage (default)                 │
│                                                             │
│  • SQL schema: sessions, messages, state_files             │
│  • Connection pooling, WAL journal                         │
└─────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    yzma Integration                         │
│                                                             │
│  • llama_state_seq_save_file(...) → state file path        │
│  • llama_chat_apply_template(...) → formatted prompt       │
│  • llama_model_chat_template(...) → template string        │
└─────────────────────────────────────────────────────────────┘
```

## 4. Core Interfaces

### 4.1. Storage

```go
package storage

// Message represents a single chat message.
type Message struct {
    ID        string    // unique message ID (UUID)
    SessionID string    // owning session ID
    Role      string    // "system", "user", "assistant"
    Content   string    // raw text
    Tokens    int       // token count (after tokenization)
    CreatedAt time.Time
    Model     string    // model used for generation (if assistant)
}

// Session represents a chat session.
type Session struct {
    ID          string    // unique session ID (UUID)
    Label       string    // human‑readable label (optional)
    Model       string    // current model name (e.g., "granite‑3b")
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Meta        map[string]string // arbitrary metadata
}

// StateFile references a saved KV‑cache state file.
type StateFile struct {
    SessionID   string
    SeqID       int32     // llama.cpp sequence ID
    Path        string    // filesystem path to the state file
    TokenCount  int       // number of tokens represented by this state
    CreatedAt   time.Time
}

type Storage interface {
    // Session operations
    SaveSession(session *Session) error
    LoadSession(sessionID string) (*Session, error)
    ListSessions(limit, offset int) ([]*Session, error)
    DeleteSession(sessionID string) error
    
    // Message operations
    SaveMessage(msg *Message) error
    LoadMessages(sessionID string, limit, offset int) ([]*Message, error)
    DeleteMessages(sessionID string) error
    
    // State‑file operations
    SaveStateFile(state *StateFile) error
    LoadStateFiles(sessionID string) ([]*StateFile, error)
    DeleteStateFile(sessionID string, seqID int32) error
    
    // Maintenance
    Close() error
}
```

### 4.2. ContextManager

```go
package context

// Strategy defines how to handle context‑window overflows.
type Strategy string

const (
    StrategyTruncateOldest Strategy = "truncate_oldest" // discard oldest messages
    StrategyTruncateNewest Strategy = "truncate_newest" // discard newest messages (except system)
    StrategyFail           Strategy = "fail"            // return error
    // Future: StrategySummarize, StrategySlide
)

// ContextManager manages the token‑count and formatting of a session’s messages.
type ContextManager interface {
    // Add a message to the session, updating token counts.
    AddMessage(sessionID, role, content string) (tokenCount int, err error)
    
    // Retrieve formatted prompt for generation, respecting maxTokens.
    // If the session’s current token count exceeds maxTokens, apply the configured strategy.
    GetContext(sessionID string, maxTokens int) (formatted string, err error)
    
    // Explicitly truncate the session’s messages using the given strategy.
    Truncate(sessionID string, strategy Strategy) error
    
    // Clear all messages (but keep the session record).
    ClearContext(sessionID string) error
    
    // Return the current token count for the session.
    TokenCount(sessionID string) (int, error)
    
    // Set/change the context‑window strategy for a session.
    SetStrategy(sessionID string, strategy Strategy) error
}
```

### 4.3. SessionManager (Facade)

```go
package session

import (
    "github.com/jsoprych/guff/internal/storage"
    "github.com/jsoprych/guff/internal/context"
)

// SessionInfo is a lightweight view of a session for listing.
type SessionInfo struct {
    ID        string
    Label     string
    Model     string
    CreatedAt time.Time
    UpdatedAt time.Time
    MessageCount int
    TotalTokens  int
}

// SessionManager orchestrates storage, context, and state management.
type SessionManager struct {
    storage storage.Storage
    context context.ContextManager
    state   StateManager // see below
}

func NewSessionManager(storage storage.Storage, ctx context.ContextManager, state StateManager) *SessionManager {
    return &SessionManager{storage, ctx, state}
}

// Create a new session with the given model and optional label.
func (sm *SessionManager) CreateSession(model, label string) (*storage.Session, error)

// Retrieve a session by ID.
func (sm *SessionManager) GetSession(sessionID string) (*storage.Session, error)

// List sessions with optional filtering (by model, date, etc.).
func (sm *SessionManager) ListSessions(filter map[string]interface{}) ([]SessionInfo, error)

// Delete a session and all associated messages and state files.
func (sm *SessionManager) DeleteSession(sessionID string) error

// Add a user or assistant message to a session.
func (sm *SessionManager) AddMessage(sessionID, role, content string) error

// Generate the next assistant response using the current model.
// If useState is true, attempts to load the latest KV‑cache state before generation.
func (sm *SessionManager) GenerateResponse(sessionID string, opts generate.GenerationOptions) (string, error)

// Switch the model used for a session (creates a new state sequence).
func (sm *SessionManager) SwitchModel(sessionID, newModel string) error
```

### 4.4. StateManager

```go
package state

// StateManager handles yzma‑specific state persistence.
type StateManager interface {
    // Save the current KV‑cache state for the given session and sequence.
    // Returns the path where the state file was written.
    SaveState(sessionID string, ctx llama.Context, seqID llama.SeqId, tokens []llama.Token) (string, error)
    
    // Load a previously saved state into the context.
    // Returns the tokens that were stored with the state.
    LoadState(sessionID string, ctx llama.Context, seqID llama.SeqId) ([]llama.Token, error)
    
    // Delete all state files associated with a session.
    CleanupSession(sessionID string) error
}
```

## 5. SQLite Implementation

### 5.1. Schema

```sql
-- sessions table
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    label TEXT,
    model TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    meta TEXT -- JSON blob
);

-- messages table
CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('system', 'user', 'assistant')),
    content TEXT NOT NULL,
    tokens INTEGER NOT NULL DEFAULT 0,
    model TEXT, -- model that generated this message (for assistant messages)
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

-- state_files table
CREATE TABLE state_files (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq_id INTEGER NOT NULL,
    path TEXT NOT NULL,
    token_count INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, seq_id)
);

-- indices
CREATE INDEX idx_messages_session ON messages(session_id);
CREATE INDEX idx_messages_created ON messages(created_at);
CREATE INDEX idx_sessions_updated ON sessions(updated_at);
```

### 5.2. Queries & Operations

- **Insert message**: parameterized insert with token count (obtained via `llama_tokenize`).
- **List sessions**: `SELECT id, label, model, created_at, updated_at, (SELECT COUNT(*) FROM messages WHERE session_id = sessions.id) AS message_count, (SELECT SUM(tokens) FROM messages WHERE session_id = sessions.id) AS total_tokens FROM sessions ORDER BY updated_at DESC`
- **Load messages for context**: `SELECT role, content, tokens FROM messages WHERE session_id = ? ORDER BY created_at ASC`
- **State file cleanup**: when a session is deleted, the SQLite cascade delete removes state‑file records; the actual files are removed by a background goroutine.

### 5.3. Migration

Use a simple versioned migration system (e.g., a `schema_version` table) to allow future schema changes.

## 6. Integration with guff

### 6.1. CLI (`guff chat`)

- **New flag**: `--session` (or `-s`) to specify a session ID; if omitted, a new session is created.
- **Commands**:
  - `/list` – show recent sessions.
  - `/switch <session‑id>` – switch to another session.
  - `/model <model‑name>` – change model for current session.
  - `/clear` – clear context (keep session).
  - `/delete` – delete current session.
- **Persistence**: after each exchange, the session’s messages are saved to SQLite; the KV‑cache state is optionally saved (configurable).

### 6.2. HTTP API (`/api/chat`)

- **Session ID header**: `X‑Session‑ID` (or as a query parameter).
- **New endpoints**:
  - `GET /api/sessions` – list sessions.
  - `POST /api/sessions` – create a new session.
  - `DELETE /api/sessions/{id}` – delete a session.
  - `POST /api/sessions/{id}/model` – change model.
- **Streaming**: the existing `/api/chat` endpoint will use the session’s context and state.

### 6.3. Configuration

```yaml
chat:
  storage:
    driver: "sqlite"           # or "memory", "file"
    dsn: "~/.local/share/guff/chat.db"
  context:
    default_strategy: "truncate_oldest"
    max_tokens_per_message: 4096
  state:
    enable_persistence: true   # save KV‑cache state files
    state_dir: "~/.local/share/guff/state/"
```

## 7. Context Management Strategies

The default strategy is `truncate_oldest`. When the token count exceeds the model’s context window:

1. System messages are preserved (if possible).
2. Oldest user/assistant pairs are removed until the token count fits.
3. The KV‑cache state is invalidated (since the token sequence changes); a new state must be generated.

Future strategies:
- **Sliding window** – keep only the last N tokens, preserving continuity of the KV‑cache where possible.
- **Summarization** – use a separate summarization model to condense old messages (requires additional model loading).
- **Hierarchical** – keep detailed recent messages, store only embeddings or summaries of older turns.

## 8. Model Switching

Switching the model within a session is allowed but has caveats:

- **KV‑cache state** cannot be reused across different models; the state is discarded.
- **Tokenization differences** may cause misalignment; the context is re‑tokenized with the new model’s tokenizer.
- **Chat template** changes according to the new model’s built‑in template (if any).

Implementation steps:
1. Validate the new model exists (in the model registry).
2. Delete any existing state files for the session.
3. Update the session’s `model` field.
4. Re‑tokenize all messages with the new model’s tokenizer (optional but recommended for accurate token counts).
5. Continue generation.

## 9. Performance Considerations

- **State file size**: KV‑cache state files can be large (proportional to context size). They should be stored in a separate directory (configurable) and cleaned up when a session is deleted.
- **Token counting**: Tokenization is performed twice (once for storage, once for generation). Consider caching tokenized sequences in memory for the active session.
- **SQLite concurrency**: Use Write‑Ahead Logging (WAL) and a connection pool to handle concurrent HTTP requests.

## 10. Extension Points

### 10.1. Pluggable Storage

Implement the `storage.Storage` interface for:
- **In‑memory** – for testing or ephemeral chats.
- **File‑based** – simple JSON‑per‑session (no SQL dependency).
- **PostgreSQL/MySQL** – for multi‑instance deployments.

### 10.2. Custom Context Strategies

Plugins can implement the `context.ContextManager` interface to provide advanced truncation, summarization, or hierarchical context management.

### 10.3. State Manager Alternatives

Replace the default `StateManager` with a version that:
- Compresses state files.
- Stores them in object storage (S3, GCS).
- Shares state across multiple guff instances (requires careful coordination).

## 11. Future Enhancements

1. **Embeddings & semantic search** – store message embeddings (using yzma’s embedding extraction) and enable “find similar past conversations”.
2. **Export/import** – export a session to a shareable format (Markdown, JSON).
3. **Multi‑modal context** – extend the message schema to support images, documents, and other media (building on yzma’s VLM capabilities).
4. **Fine‑grained retention policies** – automatically archive or delete old sessions based on configurable rules.
5. **Web UI** – a simple browser‑based chat interface that consumes the HTTP API.

---

*This document serves as a blueprint for implementing chat context management in guff. The design prioritizes modularity, leverages yzma where possible, and provides a solid foundation for future enhancements.*