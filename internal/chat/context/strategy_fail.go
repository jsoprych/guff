package context

import (
	"context"

	"github.com/jsoprych/guff/internal/chat/storage"
)

// FailStrategy returns ErrContextTooLong when the context exceeds the token budget.
// No truncation is performed.
type FailStrategy struct{}

func (s *FailStrategy) Name() string {
	return "fail"
}

func (s *FailStrategy) Truncate(_ context.Context, _ storage.Storage, _ string, _ []*storage.Message, _ int) ([]*storage.Message, error) {
	return nil, ErrContextTooLong
}
