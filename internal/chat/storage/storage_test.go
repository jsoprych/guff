package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSQLiteStorage(t *testing.T) {
	ctx := context.Background()

	// Use temporary directory for SQLite database
	tmpDir := t.TempDir()
	store, err := NewSQLiteStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}
	defer store.Close()

	// Generate unique IDs for testing
	sessionID := uuid.New().String()
	userID := "test-user"
	modelName := "test-model"
	title := "Test Session"

	now := time.Now()

	// Test CreateSession
	session := &Session{
		ID:           sessionID,
		UserID:       userID,
		ModelName:    modelName,
		Title:        title,
		CreatedAt:    now,
		UpdatedAt:    now,
		MessageCount: 0,
		TotalTokens:  0,
	}
	err = store.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Test GetSession
	retrieved, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetSession returned nil")
	}
	if retrieved.ID != sessionID || retrieved.UserID != userID || retrieved.ModelName != modelName || retrieved.Title != title {
		t.Errorf("GetSession returned mismatched data: got %+v", retrieved)
	}

	// Test UpdateSession
	retrieved.MessageCount = 2
	retrieved.TotalTokens = 100
	retrieved.UpdatedAt = now.Add(time.Hour)
	err = store.UpdateSession(ctx, retrieved)
	if err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}
	updated, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession after update failed: %v", err)
	}
	if updated.MessageCount != 2 || updated.TotalTokens != 100 {
		t.Errorf("UpdateSession didn't persist changes: got %+v", updated)
	}

	// Test AddMessage
	msgID := uuid.New().String()
	message := &Message{
		ID:         msgID,
		SessionID:  sessionID,
		Role:       RoleUser,
		Content:    "Hello, world!",
		CreatedAt:  now,
		TokenCount: 5,
	}
	err = store.AddMessage(ctx, message)
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	// Test GetMessage
	msg, err := store.GetMessage(ctx, msgID)
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}
	if msg == nil {
		t.Fatal("GetMessage returned nil")
	}
	if msg.Content != "Hello, world!" || msg.Role != RoleUser {
		t.Errorf("GetMessage returned mismatched data: got %+v", msg)
	}

	// Test GetMessages
	messages, err := store.GetMessages(ctx, sessionID, 10, 0)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if messages[0].ID != msgID {
		t.Errorf("GetMessages returned wrong message: got %+v", messages[0])
	}

	// Test CountMessages
	count, err := store.CountMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("CountMessages failed: %v", err)
	}
	if count != 1 {
		t.Errorf("CountMessages expected 1, got %d", count)
	}

	// Test UpdateMessage
	msg.Content = "Updated content"
	msg.TokenCount = 10
	err = store.UpdateMessage(ctx, msg)
	if err != nil {
		t.Fatalf("UpdateMessage failed: %v", err)
	}
	updatedMsg, _ := store.GetMessage(ctx, msgID)
	if updatedMsg.Content != "Updated content" || updatedMsg.TokenCount != 10 {
		t.Errorf("UpdateMessage didn't persist changes: got %+v", updatedMsg)
	}

	// Test DeleteMessage
	err = store.DeleteMessage(ctx, msgID)
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}
	count, _ = store.CountMessages(ctx, sessionID)
	if count != 0 {
		t.Errorf("DeleteMessage didn't delete, count = %d", count)
	}

	// Test DeleteMessages (empty)
	err = store.DeleteMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("DeleteMessages failed: %v", err)
	}

	// Test ListSessions
	sessions, err := store.ListSessions(ctx, userID, 10, 0)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}

	// Test DeleteSession
	err = store.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	deleted, _ := store.GetSession(ctx, sessionID)
	if deleted != nil {
		t.Errorf("DeleteSession didn't delete session")
	}

	// Test that messages are cascade-deleted (should be empty)
	messages, _ = store.GetMessages(ctx, sessionID, 10, 0)
	if len(messages) != 0 {
		t.Errorf("Cascade delete failed, messages left: %d", len(messages))
	}
}
