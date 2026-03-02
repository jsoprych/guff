package context

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jsoprych/guff/internal/chat/storage"
)

// Strategy name constants for convenience.
const (
	StrategySlidingWindow = "sliding_window"
	StrategyFail          = "fail"
)

var (
	ErrContextTooLong  = errors.New("context window exceeded")
	ErrSessionNotFound = errors.New("session not found")
	ErrInvalidStrategy = errors.New("invalid strategy")
)

// ContextStrategy defines how to handle context window overflow.
type ContextStrategy interface {
	Name() string
	Truncate(ctx context.Context, store storage.Storage, sessionID string,
		messages []*storage.Message, tokenBudget int) ([]*storage.Message, error)
}

// NewStrategy returns a ContextStrategy by name.
func NewStrategy(name string) (ContextStrategy, error) {
	switch name {
	case StrategySlidingWindow:
		return &SlidingWindowStrategy{}, nil
	case StrategyFail:
		return &FailStrategy{}, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidStrategy, name)
	}
}

// ContextStatus holds a snapshot of the context state for display.
type ContextStatus struct {
	MessageCount int
	TotalTokens  int
	TokenBudget  int
	StrategyName string
	Truncated    bool
}

// ContextManager manages the token-count and formatting of a session's messages.
type ContextManager interface {
	AddMessage(ctx context.Context, sessionID, role, content string) (tokenCount int, err error)
	GetContext(ctx context.Context, sessionID string, maxTokens int) (formatted string, err error)
	ClearContext(ctx context.Context, sessionID string) error
	TokenCount(ctx context.Context, sessionID string) (int, error)
	SetStrategy(ctx context.Context, sessionID string, strategy ContextStrategy) error
	GetStatus(ctx context.Context, sessionID string) (*ContextStatus, error)
}

// Tokenizer defines the interface for token counting.
type Tokenizer interface {
	CountTokens(text string) int
}

// DefaultContextManager implements ContextManager with storage and tokenizer.
type DefaultContextManager struct {
	store       storage.Storage
	tokenizer   Tokenizer
	mu          sync.RWMutex
	strategies  map[string]ContextStrategy // sessionID -> strategy
	lastBudget  map[string]int             // sessionID -> last token budget used
	truncatedAt map[string]bool            // sessionID -> whether truncation happened last GetContext
}

// NewDefaultContextManager creates a new context manager.
func NewDefaultContextManager(store storage.Storage, tokenizer Tokenizer) *DefaultContextManager {
	return &DefaultContextManager{
		store:       store,
		tokenizer:   tokenizer,
		strategies:  make(map[string]ContextStrategy),
		lastBudget:  make(map[string]int),
		truncatedAt: make(map[string]bool),
	}
}

// AddMessage implements ContextManager.AddMessage.
func (cm *DefaultContextManager) AddMessage(ctx context.Context, sessionID, role, content string) (int, error) {
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if session == nil {
		return 0, ErrSessionNotFound
	}

	tokenCount := cm.tokenizer.CountTokens(content)

	msg := &storage.Message{
		ID:         storage.GenerateID(),
		SessionID:  sessionID,
		Role:       storage.MessageRole(role),
		Content:    content,
		CreatedAt:  time.Now(),
		TokenCount: tokenCount,
	}

	if err := cm.store.AddMessage(ctx, msg); err != nil {
		return 0, err
	}

	session.TotalTokens += tokenCount
	session.MessageCount++
	session.UpdatedAt = time.Now()

	if err := cm.store.UpdateSession(ctx, session); err != nil {
		return 0, err
	}

	return tokenCount, nil
}

// GetContext implements ContextManager.GetContext.
func (cm *DefaultContextManager) GetContext(ctx context.Context, sessionID string, maxTokens int) (string, error) {
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", ErrSessionNotFound
	}

	messages, err := cm.store.GetMessages(ctx, sessionID, 0, 0)
	if err != nil {
		return "", err
	}

	totalTokens := 0
	for _, msg := range messages {
		totalTokens += msg.TokenCount
	}

	// Track status
	cm.mu.Lock()
	cm.lastBudget[sessionID] = maxTokens
	cm.mu.Unlock()

	truncated := false
	if totalTokens > maxTokens {
		truncated = true
		strategy := cm.getStrategy(sessionID)
		remaining, err := strategy.Truncate(ctx, cm.store, sessionID, messages, maxTokens)
		if err != nil {
			return "", err
		}
		// Update session counts after truncation
		if err := cm.updateSessionAfterTruncation(ctx, sessionID, remaining); err != nil {
			return "", err
		}
		// Re-fetch to get canonical order from storage
		messages, err = cm.store.GetMessages(ctx, sessionID, 0, 0)
		if err != nil {
			return "", err
		}
	}

	cm.mu.Lock()
	cm.truncatedAt[sessionID] = truncated
	cm.mu.Unlock()

	return cm.formatMessages(messages), nil
}

// ClearContext implements ContextManager.ClearContext.
func (cm *DefaultContextManager) ClearContext(ctx context.Context, sessionID string) error {
	if err := cm.store.DeleteMessages(ctx, sessionID); err != nil {
		return err
	}

	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return ErrSessionNotFound
	}

	session.MessageCount = 0
	session.TotalTokens = 0
	session.UpdatedAt = time.Now()

	return cm.store.UpdateSession(ctx, session)
}

// TokenCount implements ContextManager.TokenCount.
func (cm *DefaultContextManager) TokenCount(ctx context.Context, sessionID string) (int, error) {
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if session == nil {
		return 0, ErrSessionNotFound
	}
	return session.TotalTokens, nil
}

// SetStrategy implements ContextManager.SetStrategy.
func (cm *DefaultContextManager) SetStrategy(ctx context.Context, sessionID string, strategy ContextStrategy) error {
	if strategy == nil {
		return ErrInvalidStrategy
	}
	cm.mu.Lock()
	cm.strategies[sessionID] = strategy
	cm.mu.Unlock()
	return nil
}

// GetStatus implements ContextManager.GetStatus.
func (cm *DefaultContextManager) GetStatus(ctx context.Context, sessionID string) (*ContextStatus, error) {
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}

	cm.mu.RLock()
	budget := cm.lastBudget[sessionID]
	truncated := cm.truncatedAt[sessionID]
	strategy := cm.getStrategyLocked(sessionID)
	cm.mu.RUnlock()

	return &ContextStatus{
		MessageCount: session.MessageCount,
		TotalTokens:  session.TotalTokens,
		TokenBudget:  budget,
		StrategyName: strategy.Name(),
		Truncated:    truncated,
	}, nil
}

// Helper methods

func (cm *DefaultContextManager) getStrategy(sessionID string) ContextStrategy {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.getStrategyLocked(sessionID)
}

// getStrategyLocked returns the strategy for a session. Caller must hold cm.mu (read or write).
func (cm *DefaultContextManager) getStrategyLocked(sessionID string) ContextStrategy {
	if strategy, ok := cm.strategies[sessionID]; ok {
		return strategy
	}
	return &SlidingWindowStrategy{} // default
}

func (cm *DefaultContextManager) updateSessionAfterTruncation(ctx context.Context, sessionID string, remaining []*storage.Message) error {
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return ErrSessionNotFound
	}

	totalTokens := 0
	for _, msg := range remaining {
		totalTokens += msg.TokenCount
	}

	session.MessageCount = len(remaining)
	session.TotalTokens = totalTokens
	session.UpdatedAt = time.Now()

	return cm.store.UpdateSession(ctx, session)
}

// GetContextMessages returns the session's messages after applying truncation
// (same logic as GetContext), but as structured data instead of a formatted string.
func (cm *DefaultContextManager) GetContextMessages(ctx context.Context, sessionID string, maxTokens int) ([]*storage.Message, error) {
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}

	messages, err := cm.store.GetMessages(ctx, sessionID, 0, 0)
	if err != nil {
		return nil, err
	}

	totalTokens := 0
	for _, msg := range messages {
		totalTokens += msg.TokenCount
	}

	cm.mu.Lock()
	cm.lastBudget[sessionID] = maxTokens
	cm.mu.Unlock()

	truncated := false
	if totalTokens > maxTokens {
		truncated = true
		strategy := cm.getStrategy(sessionID)
		remaining, err := strategy.Truncate(ctx, cm.store, sessionID, messages, maxTokens)
		if err != nil {
			return nil, err
		}
		if err := cm.updateSessionAfterTruncation(ctx, sessionID, remaining); err != nil {
			return nil, err
		}
		messages, err = cm.store.GetMessages(ctx, sessionID, 0, 0)
		if err != nil {
			return nil, err
		}
	}

	cm.mu.Lock()
	cm.truncatedAt[sessionID] = truncated
	cm.mu.Unlock()

	return messages, nil
}

func (cm *DefaultContextManager) formatMessages(messages []*storage.Message) string {
	var formatted strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case storage.RoleSystem:
			formatted.WriteString("System: ")
		case storage.RoleUser:
			formatted.WriteString("User: ")
		case storage.RoleAssistant:
			formatted.WriteString("Assistant: ")
		case storage.RoleTool:
			formatted.WriteString("Tool: ")
		}
		formatted.WriteString(msg.Content)
		formatted.WriteString("\n")
	}
	formatted.WriteString("Assistant:")
	return formatted.String()
}
