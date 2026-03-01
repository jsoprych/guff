package storage

import (
	"context"
	"time"
)

// MessageRole represents the role of a chat message.
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// Message represents a single message in a chat session.
type Message struct {
	ID         string      `json:"id"`
	SessionID  string      `json:"session_id"`
	Role       MessageRole `json:"role"`
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	TokenCount int         `json:"token_count,omitempty"`
}

// ToolCall represents a tool call made by the assistant.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction represents the function being called.
type ToolFunction struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// Session represents a chat session.
type Session struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id,omitempty"`
	ModelName    string    `json:"model_name"`
	Title        string    `json:"title,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	TotalTokens  int       `json:"total_tokens"`
}

// StateFile represents a saved KV‑cache state.
type StateFile struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	Path       string    `json:"path"`
	CreatedAt  time.Time `json:"created_at"`
	TokenCount int       `json:"token_count"`
}

// Storage defines the interface for persistent storage of chat data.
type Storage interface {
	// Session operations
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	UpdateSession(ctx context.Context, session *Session) error
	DeleteSession(ctx context.Context, id string) error
	ListSessions(ctx context.Context, userID string, limit, offset int) ([]*Session, error)

	// Message operations
	AddMessage(ctx context.Context, message *Message) error
	GetMessage(ctx context.Context, id string) (*Message, error)
	GetMessages(ctx context.Context, sessionID string, limit, offset int) ([]*Message, error)
	UpdateMessage(ctx context.Context, message *Message) error
	DeleteMessage(ctx context.Context, id string) error
	DeleteMessages(ctx context.Context, sessionID string) error
	CountMessages(ctx context.Context, sessionID string) (int, error)

	// State file operations
	AddStateFile(ctx context.Context, state *StateFile) error
	GetStateFile(ctx context.Context, id string) (*StateFile, error)
	GetStateFiles(ctx context.Context, sessionID string) ([]*StateFile, error)
	DeleteStateFile(ctx context.Context, id string) error
	DeleteStateFiles(ctx context.Context, sessionID string) error

	// Maintenance
	Close() error
	CleanupOldStateFiles(ctx context.Context, olderThan time.Time) error
}
