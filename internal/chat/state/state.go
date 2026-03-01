package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/jsoprych/guff/internal/chat/storage"
)

var (
	ErrStateNotFound = errors.New("state file not found")
)

// StateManager handles yzma‑specific state persistence.
type StateManager interface {
	// Save the current KV‑cache state for the given session and sequence.
	// Returns the path where the state file was written.
	SaveState(ctx context.Context, sessionID string, ctxLLama llama.Context, seqID llama.SeqId, tokens []llama.Token) (string, error)

	// Load a previously saved state into the context.
	// Returns the tokens that were stored with the state.
	LoadState(ctx context.Context, sessionID string, ctxLLama llama.Context, seqID llama.SeqId) ([]llama.Token, error)

	// Delete all state files associated with a session.
	CleanupSession(ctx context.Context, sessionID string) error
}

// YzmaStateManager implements StateManager using yzma's state persistence functions.
type YzmaStateManager struct {
	store    storage.Storage
	stateDir string
}

// NewYzmaStateManager creates a new state manager.
func NewYzmaStateManager(store storage.Storage, stateDir string) *YzmaStateManager {
	return &YzmaStateManager{
		store:    store,
		stateDir: stateDir,
	}
}

// SaveState implements StateManager.SaveState.
func (sm *YzmaStateManager) SaveState(ctx context.Context, sessionID string, ctxLLama llama.Context, seqID llama.SeqId, tokens []llama.Token) (string, error) {
	// Generate unique filename
	filename := fmt.Sprintf("%s_seq%d_%d.state", sessionID, seqID, time.Now().UnixNano())
	path := filepath.Join(sm.stateDir, filename)

	// Save state using yzma
	bytesWritten := llama.StateSeqSaveFile(ctxLLama, path, seqID, tokens)
	if bytesWritten == 0 {
		return "", fmt.Errorf("failed to save state file")
	}

	// Record in storage
	stateFile := &storage.StateFile{
		ID:         generateID(),
		SessionID:  sessionID,
		Path:       path,
		CreatedAt:  time.Now(),
		TokenCount: len(tokens),
	}

	if err := sm.store.AddStateFile(ctx, stateFile); err != nil {
		// Attempt to clean up the file
		os.Remove(path)
		return "", fmt.Errorf("failed to record state file: %w", err)
	}

	return path, nil
}

// LoadState implements StateManager.LoadState.
func (sm *YzmaStateManager) LoadState(ctx context.Context, sessionID string, ctxLLama llama.Context, seqID llama.SeqId) ([]llama.Token, error) {
	// Get latest state file for this session
	files, err := sm.store.GetStateFiles(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get state files: %w", err)
	}
	if len(files) == 0 {
		return nil, ErrStateNotFound
	}

	// Use the most recent state file
	latest := files[0]

	// Load state using yzma
	// Allocate slice with capacity from stored token count
	tokenCapacity := uint64(latest.TokenCount)
	tokensOut := make([]llama.Token, tokenCapacity)
	var nTokenCount uint64

	bytesRead := llama.StateSeqLoadFile(ctxLLama, latest.Path, seqID, tokensOut, tokenCapacity, &nTokenCount)
	if bytesRead == 0 {
		return nil, fmt.Errorf("failed to load state file")
	}

	// Return only the actual tokens loaded
	return tokensOut[:nTokenCount], nil
}

// CleanupSession implements StateManager.CleanupSession.
func (sm *YzmaStateManager) CleanupSession(ctx context.Context, sessionID string) error {
	// Get all state files for this session
	files, err := sm.store.GetStateFiles(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get state files: %w", err)
	}

	// Delete files from filesystem
	for _, file := range files {
		if err := os.Remove(file.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove state file %s: %w", file.Path, err)
		}
	}

	// Delete from storage
	if err := sm.store.DeleteStateFiles(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to delete state files from storage: %w", err)
	}

	return nil
}

// Helper functions

func generateID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
