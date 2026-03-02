package generate

import (
	"context"
	"fmt"

	"github.com/hybridgroup/yzma/pkg/llama"
)

// EmbeddingResult holds the output of an embedding operation.
type EmbeddingResult struct {
	Embedding  []float32
	Dimensions int
	TokenCount int
}

// Embed generates an embedding vector for the given text.
// The model must support encoding (HasEncoder) for best results,
// but will attempt to use decoder-only models as well.
func (g *Generator) Embed(ctx context.Context, text string) (*EmbeddingResult, error) {
	// Enable embedding output mode
	llama.SetEmbeddings(g.model.Ctx, true)
	defer llama.SetEmbeddings(g.model.Ctx, false)

	// Clear KV cache
	g.clearKVCache()

	// Tokenize
	tokens := llama.Tokenize(g.model.Vocab, text, true, false)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("tokenization produced no tokens")
	}

	// Create batch and decode
	batch := llama.BatchGetOne(tokens)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if _, err := llama.Decode(g.model.Ctx, batch); err != nil {
		return nil, fmt.Errorf("decode error: %w", err)
	}

	// Extract embeddings
	nEmbd := g.model.NEmbd
	embeddings, err := llama.GetEmbeddings(g.model.Ctx, len(tokens), nEmbd)
	if err != nil {
		return nil, fmt.Errorf("get embeddings: %w", err)
	}

	// Average pool across tokens if multiple embeddings returned
	if len(embeddings) > nEmbd {
		nTokens := len(embeddings) / nEmbd
		pooled := make([]float32, nEmbd)
		for t := 0; t < nTokens; t++ {
			for d := 0; d < nEmbd; d++ {
				pooled[d] += embeddings[t*nEmbd+d]
			}
		}
		for d := 0; d < nEmbd; d++ {
			pooled[d] /= float32(nTokens)
		}
		embeddings = pooled
	}

	return &EmbeddingResult{
		Embedding:  embeddings,
		Dimensions: nEmbd,
		TokenCount: len(tokens),
	}, nil
}
