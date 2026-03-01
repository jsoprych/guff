package context

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jsoprych/guff/internal/chat/storage"
)

// Strategy defines how to handle context‑window overflows.
type Strategy string

const (
	StrategyTruncateOldest Strategy = "truncate_oldest" // discard oldest messages
	StrategyTruncateNewest Strategy = "truncate_newest" // discard newest messages (except system)
	StrategyFail           Strategy = "fail"            // return error
	// Future: StrategySummarize, StrategySlide
)

var (
	ErrContextTooLong  = errors.New("context window exceeded")
	ErrSessionNotFound = errors.New("session not found")
	ErrInvalidStrategy = errors.New("invalid strategy")
)

// ContextManager manages the token‑count and formatting of a session’s messages.
type ContextManager interface {
	// Add a message to the session, updating token counts.
	AddMessage(ctx context.Context, sessionID, role, content string) (tokenCount int, err error)

	// Retrieve formatted prompt for generation, respecting maxTokens.
	// If the session’s current token count exceeds maxTokens, apply the configured strategy.
	GetContext(ctx context.Context, sessionID string, maxTokens int) (formatted string, err error)

	// Explicitly truncate the session’s messages using the given strategy.
	Truncate(ctx context.Context, sessionID string, strategy Strategy) error

	// Clear all messages (but keep the session record).
	ClearContext(ctx context.Context, sessionID string) error

	// Return the current token count for the session.
	TokenCount(ctx context.Context, sessionID string) (int, error)

	// Set/change the context‑window strategy for a session.
	SetStrategy(ctx context.Context, sessionID string, strategy Strategy) error
}

// Tokenizer defines the interface for token counting.
type Tokenizer interface {
	CountTokens(text string) int
}

// DefaultContextManager implements ContextManager with storage and tokenizer.
type DefaultContextManager struct {
	store      storage.Storage
	tokenizer  Tokenizer
	strategies map[string]Strategy // sessionID -> strategy
}

// NewDefaultContextManager creates a new context manager.
func NewDefaultContextManager(store storage.Storage, tokenizer Tokenizer) *DefaultContextManager {
	return &DefaultContextManager{
		store:      store,
		tokenizer:  tokenizer,
		strategies: make(map[string]Strategy),
	}
}

// AddMessage implements ContextManager.AddMessage.
func (cm *DefaultContextManager) AddMessage(ctx context.Context, sessionID, role, content string) (int, error) {
	// Get session to ensure it exists
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if session == nil {
		return 0, ErrSessionNotFound
	}

	// Count tokens for this message
	tokenCount := cm.tokenizer.CountTokens(content)

	// Create message record
	msg := &storage.Message{
		ID:         generateID(),
		SessionID:  sessionID,
		Role:       storage.MessageRole(role),
		Content:    content,
		CreatedAt:  time.Now(),
		TokenCount: tokenCount,
	}

	// Save message
	if err := cm.store.AddMessage(ctx, msg); err != nil {
		return 0, err
	}

	// Update session token count
	session.TotalTokens += tokenCount
	session.MessageCount++
	session.UpdatedAt = time.Now()

	if err := cm.store.UpdateSession(ctx, session); err != nil {
		// Attempt to rollback message?
		return 0, err
	}

	return tokenCount, nil
}

// GetContext implements ContextManager.GetContext.
func (cm *DefaultContextManager) GetContext(ctx context.Context, sessionID string, maxTokens int) (string, error) {
	// Get session
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", ErrSessionNotFound
	}

	// Get all messages
	messages, err := cm.store.GetMessages(ctx, sessionID, 0, 0) // 0,0 means all messages
	if err != nil {
		return "", err
	}

	// Calculate total tokens
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += msg.TokenCount
	}

	// Apply strategy if needed
	if totalTokens > maxTokens {
		strategy := cm.getStrategy(sessionID)
		if err := cm.applyTruncation(ctx, sessionID, messages, maxTokens, strategy); err != nil {
			return "", err
		}
		// Re-fetch messages after truncation
		messages, err = cm.store.GetMessages(ctx, sessionID, 0, 0)
		if err != nil {
			return "", err
		}
	}

	// Format messages into prompt
	return cm.formatMessages(messages), nil
}

// Truncate implements ContextManager.Truncate.
func (cm *DefaultContextManager) Truncate(ctx context.Context, sessionID string, strategy Strategy) error {
	// Validate strategy
	if !isValidStrategy(strategy) {
		return ErrInvalidStrategy
	}

	// Get messages
	messages, err := cm.store.GetMessages(ctx, sessionID, 0, 0)
	if err != nil {
		return err
	}

	// Calculate current tokens
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += msg.TokenCount
	}

	// Apply truncation to fit within current token count (i.e., no max reduction)
	return cm.applyTruncation(ctx, sessionID, messages, totalTokens, strategy)
}

// ClearContext implements ContextManager.ClearContext.
func (cm *DefaultContextManager) ClearContext(ctx context.Context, sessionID string) error {
	// Delete all messages for session
	if err := cm.store.DeleteMessages(ctx, sessionID); err != nil {
		return err
	}

	// Update session counts
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
func (cm *DefaultContextManager) SetStrategy(ctx context.Context, sessionID string, strategy Strategy) error {
	if !isValidStrategy(strategy) {
		return ErrInvalidStrategy
	}
	cm.strategies[sessionID] = strategy
	return nil
}

// Helper methods

func (cm *DefaultContextManager) getStrategy(sessionID string) Strategy {
	if strategy, ok := cm.strategies[sessionID]; ok {
		return strategy
	}
	return StrategyTruncateOldest // default
}

func (cm *DefaultContextManager) applyTruncation(ctx context.Context, sessionID string, messages []*storage.Message, maxTokens int, strategy Strategy) error {
	switch strategy {
	case StrategyTruncateOldest:
		return cm.truncateOldest(ctx, sessionID, messages, maxTokens)
	case StrategyTruncateNewest:
		return cm.truncateNewest(ctx, sessionID, messages, maxTokens)
	case StrategyFail:
		return ErrContextTooLong
	default:
		return ErrInvalidStrategy
	}
}

func (cm *DefaultContextManager) truncateOldest(ctx context.Context, sessionID string, messages []*storage.Message, maxTokens int) error {
	// Keep newest messages that fit within maxTokens
	totalTokens := 0
	keepFrom := len(messages)

	// Start from newest (last) message and work backwards
	for i := len(messages) - 1; i >= 0; i-- {
		totalTokens += messages[i].TokenCount
		if totalTokens > maxTokens {
			keepFrom = i + 1
			break
		}
	}

	// Delete messages before keepFrom
	for i := 0; i < keepFrom; i++ {
		if err := cm.store.DeleteMessage(ctx, messages[i].ID); err != nil {
			return err
		}
	}

	// Update session counts
	return cm.updateSessionAfterTruncation(ctx, sessionID, messages[keepFrom:])
}

func (cm *DefaultContextManager) truncateNewest(ctx context.Context, sessionID string, messages []*storage.Message, maxTokens int) error {
	// Keep oldest messages, preserving system messages if possible
	totalTokens := 0
	keepUntil := 0

	// First pass: count system messages (they stay)
	for i, msg := range messages {
		if msg.Role == storage.RoleSystem {
			totalTokens += msg.TokenCount
			keepUntil = i + 1
		}
	}

	// Second pass: add oldest non-system messages until maxTokens
	for i, msg := range messages {
		if msg.Role == storage.RoleSystem {
			continue // already counted
		}
		totalTokens += msg.TokenCount
		if totalTokens > maxTokens {
			break
		}
		keepUntil = i + 1
	}

	// Delete messages after keepUntil
	for i := keepUntil; i < len(messages); i++ {
		if err := cm.store.DeleteMessage(ctx, messages[i].ID); err != nil {
			return err
		}
	}

	// Update session counts
	return cm.updateSessionAfterTruncation(ctx, sessionID, messages[:keepUntil])
}

func (cm *DefaultContextManager) updateSessionAfterTruncation(ctx context.Context, sessionID string, remaining []*storage.Message) error {
	session, err := cm.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	// Recalculate totals
	totalTokens := 0
	for _, msg := range remaining {
		totalTokens += msg.TokenCount
	}

	session.MessageCount = len(remaining)
	session.TotalTokens = totalTokens
	session.UpdatedAt = time.Now()

	return cm.store.UpdateSession(ctx, session)
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
	// Add final "Assistant:" prefix for next response
	formatted.WriteString("Assistant:")
	return formatted.String()
}

func generateID() string {
	// TODO: implement proper UUID generation
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func isValidStrategy(s Strategy) bool {
	return s == StrategyTruncateOldest || s == StrategyTruncateNewest || s == StrategyFail
}
