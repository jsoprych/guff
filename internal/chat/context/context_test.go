package context

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jsoprych/guff/internal/chat/storage"
)

// mockTokenizer returns token count as number of characters.
type mockTokenizer struct{}

func (m mockTokenizer) CountTokens(text string) int {
	return len(text)
}

func TestDefaultContextManager(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	tokenizer := mockTokenizer{}
	cm := NewDefaultContextManager(store, tokenizer)

	// Create a session
	session := &storage.Session{
		ID:           "test-session",
		UserID:       "",
		ModelName:    "test-model",
		Title:        "Test",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		MessageCount: 0,
		TotalTokens:  0,
	}
	err = store.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Test AddMessage
	tokenCount, err := cm.AddMessage(ctx, session.ID, "user", "Hello")
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}
	if tokenCount != 5 {
		t.Errorf("Expected token count 5, got %d", tokenCount)
	}

	// Verify session updated
	updatedSession, _ := store.GetSession(ctx, session.ID)
	if updatedSession.MessageCount != 1 {
		t.Errorf("Expected MessageCount 1, got %d", updatedSession.MessageCount)
	}
	if updatedSession.TotalTokens != 5 {
		t.Errorf("Expected TotalTokens 5, got %d", updatedSession.TotalTokens)
	}

	// Add another message
	_, err = cm.AddMessage(ctx, session.ID, "assistant", "Hi there!")
	if err != nil {
		t.Fatalf("AddMessage assistant failed: %v", err)
	}

	// Test TokenCount
	count, err := cm.TokenCount(ctx, session.ID)
	if err != nil {
		t.Fatalf("TokenCount failed: %v", err)
	}
	// "Hello" (5) + "Hi there!" (9) = 14
	if count != 14 {
		t.Errorf("Expected token count 14, got %d", count)
	}

	// Test GetContext with enough tokens
	formatted, err := cm.GetContext(ctx, session.ID, 100)
	if err != nil {
		t.Fatalf("GetContext failed: %v", err)
	}
	expected := "User: Hello\nAssistant: Hi there!\nAssistant:"
	if formatted != expected {
		t.Errorf("GetContext mismatch:\nExpected: %q\nGot: %q", expected, formatted)
	}

	// Test GetContext with token limit (sliding window truncation)
	// Set maxTokens to allow only the assistant message (9 tokens).
	// Default strategy is SlidingWindowStrategy, so the oldest non-system message (user) should be deleted.
	formatted, err = cm.GetContext(ctx, session.ID, 9)
	if err != nil {
		t.Fatalf("GetContext with truncation failed: %v", err)
	}
	expected = "Assistant: Hi there!\nAssistant:"
	if formatted != expected {
		t.Errorf("Truncation mismatch:\nExpected: %q\nGot: %q", expected, formatted)
	}
	// Verify that user message was deleted
	messages, _ := store.GetMessages(ctx, session.ID, 0, 0)
	if len(messages) != 1 {
		t.Errorf("Expected 1 message after truncation, got %d", len(messages))
	}
	if messages[0].Content != "Hi there!" {
		t.Errorf("Remaining message content mismatch: %s", messages[0].Content)
	}

	// Test ClearContext
	err = cm.ClearContext(ctx, session.ID)
	if err != nil {
		t.Fatalf("ClearContext failed: %v", err)
	}
	messages, _ = store.GetMessages(ctx, session.ID, 0, 0)
	if len(messages) != 0 {
		t.Errorf("ClearContext didn't delete messages, got %d", len(messages))
	}
	sess, _ := store.GetSession(ctx, session.ID)
	if sess.MessageCount != 0 || sess.TotalTokens != 0 {
		t.Errorf("Session counts not reset: %+v", sess)
	}

	// Test SetStrategy with FailStrategy
	err = cm.SetStrategy(ctx, session.ID, &FailStrategy{})
	if err != nil {
		t.Fatalf("SetStrategy failed: %v", err)
	}
	// Add messages again
	cm.AddMessage(ctx, session.ID, "user", "Hello")
	cm.AddMessage(ctx, session.ID, "assistant", "Hi")
	// Should fail when exceeding maxTokens
	_, err = cm.GetContext(ctx, session.ID, 1)
	if err != ErrContextTooLong {
		t.Errorf("Expected ErrContextTooLong, got %v", err)
	}
}

func TestDefaultContextManagerSystemMessages(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	tokenizer := mockTokenizer{}
	cm := NewDefaultContextManager(store, tokenizer)

	session := &storage.Session{
		ID:           "test-session-system",
		UserID:       "",
		ModelName:    "test-model",
		Title:        "Test",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		MessageCount: 0,
		TotalTokens:  0,
	}
	err = store.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Add system message
	cm.AddMessage(ctx, session.ID, "system", "You are a helpful assistant.")
	// Add user and assistant messages
	cm.AddMessage(ctx, session.ID, "user", "Hello")
	cm.AddMessage(ctx, session.ID, "assistant", "Hi")
	cm.AddMessage(ctx, session.ID, "user", "What's up?")

	// Default strategy is SlidingWindowStrategy which preserves system messages.
	// Total tokens: "You are a helpful assistant." (28) + "Hello" (5) + "Hi" (2) + "What's up?" (10) = 45
	// Set maxTokens = 30: system (28) + should keep only newest non-system msg that fits (2 remaining)
	formatted, err := cm.GetContext(ctx, session.ID, 30)
	if err != nil {
		t.Fatalf("GetContext failed: %v", err)
	}
	// System message must be preserved
	if !strings.Contains(formatted, "You are a helpful assistant.") {
		t.Errorf("System message not preserved: %s", formatted)
	}
	// Verify that system message exists in storage
	messages, _ := store.GetMessages(ctx, session.ID, 0, 0)
	var foundSystem bool
	for _, msg := range messages {
		if msg.Role == storage.RoleSystem {
			foundSystem = true
			break
		}
	}
	if !foundSystem {
		t.Error("System message not found in storage after truncation")
	}
}

func TestGetStatus(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	tokenizer := mockTokenizer{}
	cm := NewDefaultContextManager(store, tokenizer)

	session := &storage.Session{
		ID:           "test-status",
		UserID:       "",
		ModelName:    "test-model",
		Title:        "Test",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		MessageCount: 0,
		TotalTokens:  0,
	}
	err = store.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	cm.AddMessage(ctx, session.ID, "user", "Hello")
	cm.AddMessage(ctx, session.ID, "assistant", "Hi there!")

	// Call GetContext to populate status
	_, err = cm.GetContext(ctx, session.ID, 100)
	if err != nil {
		t.Fatalf("GetContext failed: %v", err)
	}

	status, err := cm.GetStatus(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.MessageCount != 2 {
		t.Errorf("Expected 2 messages, got %d", status.MessageCount)
	}
	if status.TotalTokens != 14 {
		t.Errorf("Expected 14 tokens, got %d", status.TotalTokens)
	}
	if status.TokenBudget != 100 {
		t.Errorf("Expected budget 100, got %d", status.TokenBudget)
	}
	if status.StrategyName != "sliding_window" {
		t.Errorf("Expected strategy sliding_window, got %s", status.StrategyName)
	}
	if status.Truncated {
		t.Error("Expected Truncated=false")
	}

	// Now trigger truncation
	_, err = cm.GetContext(ctx, session.ID, 9)
	if err != nil {
		t.Fatalf("GetContext truncation failed: %v", err)
	}
	status, err = cm.GetStatus(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetStatus after truncation failed: %v", err)
	}
	if !status.Truncated {
		t.Error("Expected Truncated=true after truncation")
	}
}

func TestNewStrategy(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"sliding_window", "sliding_window", false},
		{"fail", "fail", false},
		{"unknown", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewStrategy(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewStrategy(%q) unexpected error: %v", tt.name, err)
			}
			if s.Name() != tt.want {
				t.Errorf("Expected name %q, got %q", tt.want, s.Name())
			}
		})
	}
}
