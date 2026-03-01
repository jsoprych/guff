package generate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/jsoprych/guff/internal/model"
)

type GenerationOptions struct {
	Temperature      float32
	TopP             float32
	TopK             int
	MaxTokens        int
	Stop             []string
	Seed             uint32
	RepeatPenalty    float32
	FrequencyPenalty float32
	PresencePenalty  float32
	Mirostat         int
	Grammar          string
	Stream           bool
}

// createSamplerChain creates a sampler chain based on generation options.
func createSamplerChain(opts GenerationOptions) llama.Sampler {
	chain := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
	// Add greedy sampler as base
	llama.SamplerChainAdd(chain, llama.SamplerInitGreedy())
	// Add temperature if > 0
	if opts.Temperature > 0 {
		// Using TempExt with delta=0, exponent=1 for standard temperature
		llama.SamplerChainAdd(chain, llama.SamplerInitTempExt(opts.Temperature, 0.0, 1.0))
	}
	// Add top-p if > 0
	if opts.TopP > 0 && opts.TopP < 1.0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitTopP(opts.TopP, 1))
	}
	// Add top-k if > 0
	if opts.TopK > 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitTopK(int32(opts.TopK)))
	}
	// Add repeat penalty if > 1.0
	if opts.RepeatPenalty > 1.0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitPenalties(-1, opts.RepeatPenalty, opts.FrequencyPenalty, opts.PresencePenalty))
	}
	return chain
}

type GenerationResult struct {
	Text         string
	Tokens       []llama.Token
	PromptTokens int
	GenTokens    int
	Duration     time.Duration
	Done         bool
}

type StreamChunk struct {
	Token string
	Text  string
	Done  bool
	Error error
}

type Generator struct {
	model *model.LoadedModel
}

func NewGenerator(m *model.LoadedModel) *Generator {
	return &Generator{
		model: m,
	}
}

// Generate completes a prompt with the given options.
func (g *Generator) Generate(ctx context.Context, prompt string, opts GenerationOptions) (*GenerationResult, error) {
	start := time.Now()

	// Create sampler chain for this generation
	sampler := createSamplerChain(opts)
	defer llama.SamplerFree(sampler)

	// Tokenize prompt
	tokens := llama.Tokenize(g.model.Vocab, prompt, true, false)

	var generated []llama.Token
	var result strings.Builder

	batch := llama.BatchGetOne(tokens)

	for i := 0; i < opts.MaxTokens; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Decode
		if _, err := llama.Decode(g.model.Ctx, batch); err != nil {
			return nil, fmt.Errorf("decode error: %w", err)
		}

		// Sample next token
		token := llama.SamplerSample(sampler, g.model.Ctx, -1)

		// Check for end of generation
		if llama.VocabIsEOG(g.model.Vocab, token) {
			break
		}

		// Convert token to piece
		buf := make([]byte, 36)
		length := llama.TokenToPiece(g.model.Vocab, token, buf, 0, true)
		if length > 0 {
			result.Write(buf[:length])
		}

		generated = append(generated, token)

		// Check stop strings
		if shouldStop(result.String(), opts.Stop) {
			break
		}

		// Prepare next batch with single token
		batch = llama.BatchGetOne([]llama.Token{token})
	}

	duration := time.Since(start)

	return &GenerationResult{
		Text:         result.String(),
		Tokens:       generated,
		PromptTokens: len(tokens),
		GenTokens:    len(generated),
		Duration:     duration,
		Done:         true,
	}, nil
}

// GenerateStream generates tokens asynchronously, sending each token through a channel.
func (g *Generator) GenerateStream(ctx context.Context, prompt string, opts GenerationOptions) <-chan StreamChunk {
	ch := make(chan StreamChunk, 32) // buffered channel to avoid blocking

	go func() {
		defer close(ch)

		// Create sampler chain for this generation
		sampler := createSamplerChain(opts)
		defer llama.SamplerFree(sampler)

		// Tokenize prompt
		tokens := llama.Tokenize(g.model.Vocab, prompt, true, false)

		var generated []llama.Token
		var result strings.Builder

		batch := llama.BatchGetOne(tokens)

		for i := 0; i < opts.MaxTokens; i++ {
			select {
			case <-ctx.Done():
				ch <- StreamChunk{Error: ctx.Err()}
				return
			default:
			}

			// Decode
			if _, err := llama.Decode(g.model.Ctx, batch); err != nil {
				ch <- StreamChunk{Error: fmt.Errorf("decode error: %w", err)}
				return
			}

			// Sample next token
			token := llama.SamplerSample(sampler, g.model.Ctx, -1)

			// Check for end of generation
			if llama.VocabIsEOG(g.model.Vocab, token) {
				break
			}

			// Convert token to piece
			buf := make([]byte, 36)
			length := llama.TokenToPiece(g.model.Vocab, token, buf, 0, true)
			tokenText := ""
			if length > 0 {
				tokenText = string(buf[:length])
				result.Write(buf[:length])
			}

			generated = append(generated, token)

			// Send token chunk
			ch <- StreamChunk{
				Token: tokenText,
				Text:  result.String(),
				Done:  false,
			}

			// Check stop strings
			if shouldStop(result.String(), opts.Stop) {
				break
			}

			// Prepare next batch with single token
			batch = llama.BatchGetOne([]llama.Token{token})
		}

		// Send final chunk
		ch <- StreamChunk{
			Text:  result.String(),
			Done:  true,
			Token: "",
		}
	}()

	return ch
}

func shouldStop(text string, stop []string) bool {
	for _, s := range stop {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// Close releases resources associated with the generator.
func (g *Generator) Close() {
	// No resources to release currently
}
