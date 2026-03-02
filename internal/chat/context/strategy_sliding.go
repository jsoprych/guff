package context

import (
	"context"

	"github.com/jsoprych/guff/internal/chat/storage"
)

// SlidingWindowStrategy keeps the newest messages that fit within the token budget.
// System messages are always preserved.
type SlidingWindowStrategy struct{}

func (s *SlidingWindowStrategy) Name() string {
	return "sliding_window"
}

func (s *SlidingWindowStrategy) Truncate(ctx context.Context, store storage.Storage, sessionID string, messages []*storage.Message, tokenBudget int) ([]*storage.Message, error) {
	// Separate system messages from the rest
	var systemMsgs []*storage.Message
	var nonSystemMsgs []*storage.Message
	systemTokens := 0

	for _, msg := range messages {
		if msg.Role == storage.RoleSystem {
			systemMsgs = append(systemMsgs, msg)
			systemTokens += msg.TokenCount
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	// Budget remaining after system messages
	remaining := tokenBudget - systemTokens
	if remaining < 0 {
		remaining = 0
	}

	// Walk backwards through non-system messages, keeping newest that fit
	keepFrom := len(nonSystemMsgs)
	used := 0
	for i := len(nonSystemMsgs) - 1; i >= 0; i-- {
		used += nonSystemMsgs[i].TokenCount
		if used > remaining {
			keepFrom = i + 1
			break
		}
		if i == 0 {
			keepFrom = 0
		}
	}

	// Delete discarded messages from storage
	for i := 0; i < keepFrom; i++ {
		if err := store.DeleteMessage(ctx, nonSystemMsgs[i].ID); err != nil {
			return nil, err
		}
	}

	// Build result: system messages first, then kept non-system messages
	result := make([]*storage.Message, 0, len(systemMsgs)+len(nonSystemMsgs)-keepFrom)
	result = append(result, systemMsgs...)
	result = append(result, nonSystemMsgs[keepFrom:]...)

	return result, nil
}
