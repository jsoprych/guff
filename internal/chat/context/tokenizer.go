package context

import (
	"github.com/hybridgroup/yzma/pkg/llama"
)

// YzmaTokenizer implements Tokenizer using yzma's tokenization.
type YzmaTokenizer struct {
	vocab llama.Vocab
}

// NewYzmaTokenizer creates a tokenizer with the given vocabulary.
func NewYzmaTokenizer(vocab llama.Vocab) *YzmaTokenizer {
	return &YzmaTokenizer{vocab: vocab}
}

// CountTokens returns the number of tokens in the text.
func (t *YzmaTokenizer) CountTokens(text string) int {
	// Check if vocab is nil (zero value)
	var zeroVocab llama.Vocab
	if t.vocab == zeroVocab {
		// Fallback: approximate token count (roughly 4 chars per token)
		return (len(text) + 3) / 4
	}
	tokens := llama.Tokenize(t.vocab, text, true, false)
	return len(tokens)
}

// SimpleTokenizer is a fallback tokenizer that uses a simple approximation.
type SimpleTokenizer struct{}

// CountTokens approximates token count as 4 characters per token.
func (t *SimpleTokenizer) CountTokens(text string) int {
	return (len(text) + 3) / 4
}
