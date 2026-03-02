package session

import (
	"context"
	"testing"

	chatcontext "github.com/jsoprych/guff/internal/chat/context"
	"github.com/jsoprych/guff/internal/chat/storage"
)

// mockTokenizer returns token count as number of characters.
type mockTokenizer struct{}

func (m mockTokenizer) CountTokens(text string) int {
	return len(text)
}

func TestSessionManager(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	tokenizer := mockTokenizer{}
	ctxManager := chatcontext.NewDefaultContextManager(store, tokenizer)
	sm := NewSessionManager(store, ctxManager)

	// Test CreateSession
	session, err := sm.CreateSession(ctx, "test-model", "Test Session")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if session.ModelName != "test-model" || session.Title != "Test Session" {
		t.Errorf("CreateSession returned mismatched data: %+v", session)
	}

	// Test GetSession
	retrieved, err := sm.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.ID != session.ID {
		t.Errorf("GetSession mismatch: got %+v", retrieved)
	}

	// Test AddMessage
	err = sm.AddMessage(ctx, session.ID, "user", "Hello")
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}
	err = sm.AddMessage(ctx, session.ID, "assistant", "Hi")
	if err != nil {
		t.Fatalf("AddMessage assistant failed: %v", err)
	}

	// Verify messages stored
	messages, _ := store.GetMessages(ctx, session.ID, 0, 0)
	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}

	// Test ListSessions
	infos, err := sm.ListSessions(ctx, "", 10, 0)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(infos) != 1 {
		t.Errorf("Expected 1 session info, got %d", len(infos))
	}
	if infos[0].ID != session.ID {
		t.Errorf("ListSessions mismatch: %+v", infos[0])
	}

	// Test ClearContext
	err = sm.ClearContext(ctx, session.ID)
	if err != nil {
		t.Fatalf("ClearContext failed: %v", err)
	}
	messages, _ = store.GetMessages(ctx, session.ID, 0, 0)
	if len(messages) != 0 {
		t.Errorf("ClearContext didn't delete messages, got %d", len(messages))
	}

	// Test SwitchModel
	err = sm.SwitchModel(ctx, session.ID, "new-model")
	if err != nil {
		t.Fatalf("SwitchModel failed: %v", err)
	}
	updated, _ := sm.GetSession(ctx, session.ID)
	if updated.ModelName != "new-model" {
		t.Errorf("SwitchModel didn't update model: %s", updated.ModelName)
	}
	// Ensure updated_at changed
	if updated.UpdatedAt.Equal(session.UpdatedAt) {
		t.Errorf("UpdatedAt not changed after SwitchModel")
	}

	// Test DeleteSession
	err = sm.DeleteSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	deleted, _ := sm.GetSession(ctx, session.ID)
	if deleted != nil {
		t.Errorf("DeleteSession didn't delete session")
	}
}

func TestSessionManagerContextSize(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	tokenizer := mockTokenizer{}
	ctxManager := chatcontext.NewDefaultContextManager(store, tokenizer)
	sm := NewSessionManager(store, ctxManager)

	// Default context size should be 2048
	if sm.ContextSize() != 2048 {
		t.Errorf("Expected default context size 2048, got %d", sm.ContextSize())
	}

	// Test SetContextSize
	sm.SetContextSize(4096)
	if sm.ContextSize() != 4096 {
		t.Errorf("Expected context size 4096, got %d", sm.ContextSize())
	}

	// Test ContextManager accessor
	if sm.ContextManager() != ctxManager {
		t.Error("ContextManager() returned wrong instance")
	}
}
