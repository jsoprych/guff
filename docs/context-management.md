# Context Management

guff tracks token usage across conversation turns and automatically manages the context window to prevent overflow.

## Architecture

```
SessionManager
  |-- contextSize (e.g., 2048)
  |-- ContextManager
        |-- Tokenizer (yzma vocabulary-based)
        |-- Storage (SQLite)
        |-- strategies map[sessionID]ContextStrategy
        |-- status tracking (lastBudget, truncatedAt)
```

## Context Budget Calculation

The context window is split between prompt tokens and generation tokens:

```
contextBudget = contextSize - maxGenTokens
```

For example, with a 2048-token context window and 1024 max generation tokens:
- Context budget: 1024 tokens for prompt (system + history)
- Generation budget: 1024 tokens for model output

The minimum context budget is clamped to 256 tokens.

## ContextManager Interface

```go
type ContextManager interface {
    AddMessage(ctx, sessionID, role, content string) (tokenCount int, err error)
    GetContext(ctx, sessionID string, maxTokens int) (formatted string, err error)
    ClearContext(ctx, sessionID string) error
    TokenCount(ctx, sessionID string) (int, error)
    SetStrategy(ctx, sessionID string, strategy ContextStrategy) error
    GetStatus(ctx, sessionID string) (*ContextStatus, error)
}
```

### AddMessage

Stores a message in SQLite with its token count (computed via the yzma tokenizer). Returns the token count.

### GetContext

1. Loads all messages for the session from storage
2. Sums token counts; if over budget, calls `strategy.Truncate()`
3. Formats surviving messages into a prompt string:
   ```
   System: You are helpful
   User: Hello
   Assistant: Hi there
   User: How are you?
   Assistant:
   ```
4. Records status (budget, whether truncation occurred)

### GetStatus

Returns a `ContextStatus` snapshot:

```go
type ContextStatus struct {
    MessageCount int     // total messages in session
    TotalTokens  int     // total tokens across all messages
    TokenBudget  int     // max tokens allowed for context
    StrategyName string  // active strategy name
    Truncated    bool    // whether truncation happened on last GetContext
}
```

## Context Strategies

Strategies implement the `ContextStrategy` interface:

```go
type ContextStrategy interface {
    Name() string
    Truncate(ctx context.Context, store storage.Storage, sessionID string,
        messages []*storage.Message, tokenBudget int) ([]*storage.Message, error)
}
```

### Sliding Window (default)

**Name:** `sliding_window`

Keeps the newest messages that fit within the token budget. System messages are always preserved.

Algorithm:
1. Separate system messages from non-system messages
2. Calculate remaining budget after system messages
3. Walk backwards through non-system messages, keeping those that fit
4. Delete discarded messages from storage
5. Return: system messages + kept non-system messages

**Source:** `internal/chat/context/strategy_sliding.go`

### Fail

**Name:** `fail`

Returns `ErrContextTooLong` without any truncation. Useful for testing or strict-budget scenarios where you want to be notified rather than lose history.

**Source:** `internal/chat/context/strategy_fail.go`

### Future Strategies (Planned)

- **Summarization** -- Summarize older messages into a condensed form using the LLM
- **Hybrid** -- Keep recent messages verbatim + summary of older messages
- **Selective** -- Let the user choose which messages to keep/discard

## Token Counting

The `Tokenizer` interface wraps yzma's vocabulary-based token counting:

```go
type Tokenizer interface {
    CountTokens(text string) int
}
```

`NewYzmaTokenizer(vocab)` creates a tokenizer from a loaded model's vocabulary. This gives exact token counts matching what llama.cpp will see during inference.

For testing, a mock tokenizer (`mockTokenizer`) uses 1 character = 1 token.

## Status Display

### Compact Status (after each response)

Printed to stderr in dim gray after every assistant response:

```
[12 msgs | 847/1024 tokens | sliding_window]
```

If truncation occurred:
```
[8 msgs | 980/1024 tokens | sliding_window truncated]
```

### Detailed Status (`/status` command)

```
  Session: d2215a3e53cc8db02fd2ced47967ce1a
  Model: granite-3b
  Messages: 12
  Tokens: 847 / 1024 (context budget)
  Context window: 2048 (generation reserve: 1024)
  Strategy: sliding_window
```

## Non-Persist Mode

When running with `--no-persist`, context management works in-memory:

- Uses the same token-aware sliding window algorithm
- Same compact status display
- Same `/status` command (shows in-memory stats)
- History is lost when the session ends

The in-memory path uses `slidingWindowTruncate()` in `cmd/chat.go` which mirrors the `SlidingWindowStrategy` logic without SQLite.

## Chat Commands

| Command | Description |
|---------|-------------|
| `/status` | Show detailed context info |
| `/clear` | Clear conversation history |
| `/exit` | Exit chat session |
