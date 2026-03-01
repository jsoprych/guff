package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	chatcontext "github.com/jsoprych/guff/internal/chat/context"
	"github.com/jsoprych/guff/internal/chat/storage"
	"github.com/jsoprych/guff/internal/generate"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrInvalidRole     = errors.New("invalid role")
)

// SessionInfo is a lightweight view of a session for listing.
type SessionInfo struct {
	ID           string    `json:"id"`
	Label        string    `json:"label"`
	Model        string    `json:"model"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	TotalTokens  int       `json:"total_tokens"`
}

// SessionManager orchestrates storage, context, and state management.
type SessionManager struct {
	storage storage.Storage
	context chatcontext.ContextManager
	// state   state.StateManager // TODO: implement state manager
}

// NewSessionManager creates a new session manager.
func NewSessionManager(storage storage.Storage, ctx chatcontext.ContextManager) *SessionManager {
	return &SessionManager{storage: storage, context: ctx}
}

// CreateSession creates a new session with the given model and optional label.
func (sm *SessionManager) CreateSession(ctx context.Context, model, label string) (*storage.Session, error) {
	session := &storage.Session{
		ID:           generateID(),
		UserID:       "", // TODO: support user authentication
		ModelName:    model,
		Title:        label,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		MessageCount: 0,
		TotalTokens:  0,
	}

	if err := sm.storage.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return session, nil
}

// GetSession retrieves a session by ID.
func (sm *SessionManager) GetSession(ctx context.Context, sessionID string) (*storage.Session, error) {
	session, err := sm.storage.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

// ListSessions lists sessions with optional filtering (by model, date, etc.).
func (sm *SessionManager) ListSessions(ctx context.Context, userID string, limit, offset int) ([]SessionInfo, error) {
	sessions, err := sm.storage.ListSessions(ctx, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	infos := make([]SessionInfo, len(sessions))
	for i, s := range sessions {
		infos[i] = SessionInfo{
			ID:           s.ID,
			Label:        s.Title,
			Model:        s.ModelName,
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
			MessageCount: s.MessageCount,
			TotalTokens:  s.TotalTokens,
		}
	}
	return infos, nil
}

// DeleteSession deletes a session and all associated messages and state files.
func (sm *SessionManager) DeleteSession(ctx context.Context, sessionID string) error {
	// TODO: delete state files when state manager is implemented
	// if err := sm.state.CleanupSession(ctx, sessionID); err != nil {
	// 	return fmt.Errorf("failed to cleanup state files: %w", err)
	// }

	// Delete session (cascading to messages)
	if err := sm.storage.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}

// ClearContext clears all messages from a session (but keeps the session record).
func (sm *SessionManager) ClearContext(ctx context.Context, sessionID string) error {
	return sm.context.ClearContext(ctx, sessionID)
}

// AddMessage adds a user or assistant message to a session.
func (sm *SessionManager) AddMessage(ctx context.Context, sessionID, role, content string) error {
	// Validate role
	if !isValidRole(role) {
		return ErrInvalidRole
	}

	// Add message via context manager (which updates storage and token counts)
	_, err := sm.context.AddMessage(ctx, sessionID, role, content)
	if err != nil {
		return fmt.Errorf("failed to add message: %w", err)
	}

	return nil
}

// GenerateResponse generates the next assistant response using the current model.
// If useState is true, attempts to load the latest KV‑cache state before generation.
func (sm *SessionManager) GenerateResponse(ctx context.Context, sessionID string, generator *generate.Generator, opts generate.GenerationOptions, useState bool) (string, error) {
	// Get session
	session, err := sm.storage.GetSession(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return "", ErrSessionNotFound
	}

	// Get formatted context
	formatted, err := sm.context.GetContext(ctx, sessionID, opts.MaxTokens)
	if err != nil {
		return "", fmt.Errorf("failed to get context: %w", err)
	}

	// TODO: Implement state loading if useState is true
	// if useState {
	//     tokens, err := sm.state.LoadState(ctx, sessionID, generator.Model.Ctx, seqID)
	//     ...
	// }

	// Generate response
	result, err := generator.Generate(ctx, formatted, opts)
	if err != nil {
		return "", fmt.Errorf("generation failed: %w", err)
	}

	// Add assistant message to session
	if err := sm.AddMessage(ctx, sessionID, "assistant", result.Text); err != nil {
		return "", fmt.Errorf("failed to save assistant message: %w", err)
	}

	// TODO: Save state if useState is true
	// if useState {
	//     _, err := sm.state.SaveState(ctx, sessionID, generator.Model.Ctx, seqID, result.Tokens)
	//     ...
	// }

	return result.Text, nil
}

// SwitchModel switches the model used for a session (creates a new state sequence).
func (sm *SessionManager) SwitchModel(ctx context.Context, sessionID, newModel string) error {
	session, err := sm.storage.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return ErrSessionNotFound
	}

	// Update session model
	session.ModelName = newModel
	session.UpdatedAt = time.Now()

	if err := sm.storage.UpdateSession(ctx, session); err != nil {
		return fmt.Errorf("failed to update session model: %w", err)
	}

	// TODO: clear state files when state manager is implemented
	// if err := sm.state.CleanupSession(ctx, sessionID); err != nil {
	// 	return fmt.Errorf("failed to cleanup old state files: %w", err)
	// }

	return nil
}

// Helper functions

func generateID() string {
	// TODO: implement proper UUID generation
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func isValidRole(role string) bool {
	switch role {
	case "system", "user", "assistant", "tool":
		return true
	default:
		return false
	}
}
