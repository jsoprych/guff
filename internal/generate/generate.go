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
	MinP             float32
	MaxTokens        int
	Stop             []string
	Seed             uint32
	RepeatPenalty    float32
	FrequencyPenalty float32
	PresencePenalty  float32
	Grammar          string
	Stream           bool

	// Extended samplers
	TypicalP      float32            // SamplerInitTypical — 0 = disabled
	TopNSigma     float32            // SamplerInitTopNSigma — 0 = disabled
	DryMultiplier float32            // SamplerInitDry — 0 = disabled
	DryBase       float32            // DRY base (default 1.75)
	DryAllowedLen int32              // DRY allowed length
	DryPenaltyLast int32             // DRY penalty lookback
	XtcP          float32            // SamplerInitXTC probability — 0 = disabled
	XtcT          float32            // SamplerInitXTC threshold
	LogitBias     map[int32]float32  // SamplerInitLogitBias — nil = disabled
}

// createSamplerChain creates a sampler chain based on generation options.
// Chain order: grammar (constrains candidates) → logit bias → temp → top-k →
// top-p → typical-p → min-p → top-n-sigma → penalties → DRY → XTC →
// terminal (dist or greedy). Filters first, terminal sampler MUST be last.
func createSamplerChain(opts GenerationOptions, vocab llama.Vocab) llama.Sampler {
	chain := llama.SamplerChainInit(llama.SamplerChainDefaultParams())

	// Grammar-constrained generation — first, constrains candidate set
	if opts.Grammar != "" {
		llama.SamplerChainAdd(chain, llama.SamplerInitGrammar(vocab, opts.Grammar, "root"))
	}

	// Logit bias — direct token probability manipulation
	if len(opts.LogitBias) > 0 {
		biases := make([]llama.LogitBias, 0, len(opts.LogitBias))
		for token, bias := range opts.LogitBias {
			biases = append(biases, llama.LogitBias{Token: llama.Token(token), Bias: bias})
		}
		llama.SamplerChainAdd(chain, llama.SamplerInitLogitBias(
			llama.VocabNTokens(vocab),
			int32(len(biases)),
			&biases[0],
		))
	}

	// Temperature — must come before other probabilistic filters
	if opts.Temperature > 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitTempExt(opts.Temperature, 0.0, 1.0))
	}

	// Top-K: keep only top K candidates
	if opts.TopK > 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitTopK(int32(opts.TopK)))
	}

	// Top-P (nucleus sampling): keep smallest set of tokens whose cumulative prob >= p
	if opts.TopP > 0 && opts.TopP < 1.0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitTopP(opts.TopP, 1))
	}

	// Typical-P: information-theoretic filtering
	if opts.TypicalP > 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitTypical(opts.TypicalP, 1))
	}

	// Min-P: discard tokens with prob < p * prob_of_top_token
	if opts.MinP > 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitMinP(opts.MinP, 1))
	}

	// Top-N Sigma: keep tokens within N standard deviations
	if opts.TopNSigma > 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitTopNSigma(opts.TopNSigma))
	}

	// Repeat/frequency/presence penalties
	if opts.RepeatPenalty > 1.0 || opts.FrequencyPenalty != 0 || opts.PresencePenalty != 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitPenalties(-1, opts.RepeatPenalty, opts.FrequencyPenalty, opts.PresencePenalty))
	}

	// DRY (Don't Repeat Yourself): penalizes repeated n-grams
	if opts.DryMultiplier > 0 {
		dryBase := opts.DryBase
		if dryBase == 0 {
			dryBase = 1.75
		}
		llama.SamplerChainAdd(chain, llama.SamplerInitDry(
			vocab,
			0, // nCtxTrain (0 = use model default)
			opts.DryMultiplier,
			dryBase,
			opts.DryAllowedLen,
			opts.DryPenaltyLast,
			nil, // seq breakers
		))
	}

	// XTC: cross-token consistency
	if opts.XtcP > 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitXTC(opts.XtcP, opts.XtcT, 1, opts.Seed))
	}

	// Terminal sampler — MUST be last in the chain
	// Use distribution sampling when temperature > 0 (probabilistic),
	// greedy when temperature == 0 (deterministic / argmax)
	if opts.Temperature > 0 {
		llama.SamplerChainAdd(chain, llama.SamplerInitDist(opts.Seed))
	} else {
		llama.SamplerChainAdd(chain, llama.SamplerInitGreedy())
	}

	return chain
}

type GenerationResult struct {
	Text            string
	Tokens          []llama.Token
	PromptTokens    int
	GenTokens       int
	Duration        time.Duration
	PromptEvalTime  time.Duration
	TokenGenTime    time.Duration
	Done            bool
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

// clearKVCache clears the KV cache so the next decode starts fresh.
func (g *Generator) clearKVCache() {
	mem, err := llama.GetMemory(g.model.Ctx)
	if err == nil && mem != 0 {
		llama.MemoryClear(mem, true)
	}
}

// trimStopStrings removes any trailing stop string from text.
func trimStopStrings(text string, stop []string) string {
	for _, s := range stop {
		if s != "" && strings.HasSuffix(text, s) {
			return strings.TrimSuffix(text, s)
		}
	}
	return text
}

// Generate completes a prompt with the given options.
func (g *Generator) Generate(ctx context.Context, prompt string, opts GenerationOptions) (*GenerationResult, error) {
	start := time.Now()

	// Clear KV cache from any previous generation
	g.clearKVCache()

	// Reset perf counters for timing
	llama.PerfContextReset(g.model.Ctx)

	// Create sampler chain for this generation
	sampler := createSamplerChain(opts, g.model.Vocab)
	defer llama.SamplerFree(sampler)

	// Tokenize prompt
	tokens := llama.Tokenize(g.model.Vocab, prompt, true, false)

	var generated []llama.Token
	var result strings.Builder

	promptStart := time.Now()
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

		// Track prompt eval time (after first decode = prompt processing)
		var promptEvalTime time.Duration
		if i == 0 {
			promptEvalTime = time.Since(promptStart)
		}
		_ = promptEvalTime // used in result below

		// Sample next token
		token := llama.SamplerSample(sampler, g.model.Ctx, -1)

		// Check for end of generation
		if llama.VocabIsEOG(g.model.Vocab, token) {
			break
		}

		// Convert token to piece
		buf := make([]byte, 256)
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
		Text:         trimStopStrings(result.String(), opts.Stop),
		Tokens:       generated,
		PromptTokens: len(tokens),
		GenTokens:    len(generated),
		Duration:     duration,
		Done:         true,
	}, nil
}

// GenerateStream generates tokens asynchronously, sending each token through a channel.
func (g *Generator) GenerateStream(ctx context.Context, prompt string, opts GenerationOptions) <-chan StreamChunk {
	// Clear KV cache from any previous generation
	g.clearKVCache()

	ch := make(chan StreamChunk, 32) // buffered channel to avoid blocking

	go func() {
		defer close(ch)

		// Create sampler chain for this generation
		sampler := createSamplerChain(opts, g.model.Vocab)
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
			buf := make([]byte, 256)
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

		// Send final chunk with stop strings trimmed
		ch <- StreamChunk{
			Text:  trimStopStrings(result.String(), opts.Stop),
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
