package provider

import (
	"context"
	"strings"
	"unsafe"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/jsoprych/guff/internal/generate"
	"github.com/jsoprych/guff/internal/model"
)

// LocalProvider wraps the existing llama.cpp-based model manager and generator.
type LocalProvider struct {
	manager  *model.ModelManager
	loadOpts model.LoadOptions
}

// NewLocalProvider creates a provider backed by local llama.cpp inference.
func NewLocalProvider(mm *model.ModelManager, opts model.LoadOptions) *LocalProvider {
	return &LocalProvider{manager: mm, loadOpts: opts}
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	loaded, err := p.manager.Load(req.Model, p.loadOpts)
	if err != nil {
		return nil, err
	}
	defer p.manager.Unload()

	gen := generate.NewGenerator(loaded)
	defer gen.Close()

	prompt := formatMessagesWithTemplate(loaded, req.Messages)
	opts := toGenOpts(req)

	result, err := gen.Generate(ctx, prompt, opts)
	if err != nil {
		return nil, err
	}

	return &ChatResponse{
		Model:        req.Model,
		Message:      Message{Role: "assistant", Content: strings.TrimSpace(result.Text)},
		PromptTokens: result.PromptTokens,
		GenTokens:    result.GenTokens,
		Done:         true,
	}, nil
}

func (p *LocalProvider) ChatCompletionStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	loaded, err := p.manager.Load(req.Model, p.loadOpts)
	if err != nil {
		return nil, err
	}

	gen := generate.NewGenerator(loaded)
	prompt := formatMessagesWithTemplate(loaded, req.Messages)
	opts := toGenOpts(req)

	srcCh := gen.GenerateStream(ctx, prompt, opts)
	outCh := make(chan StreamChunk, 32)

	go func() {
		defer close(outCh)
		defer gen.Close()
		defer p.manager.Unload()

		var prev string
		for chunk := range srcCh {
			if chunk.Error != nil {
				outCh <- StreamChunk{Error: chunk.Error}
				return
			}
			// Compute delta (incremental text)
			delta := ""
			if len(chunk.Text) > len(prev) {
				delta = chunk.Text[len(prev):]
			}
			prev = chunk.Text
			outCh <- StreamChunk{Delta: delta, Done: chunk.Done}
		}
	}()

	return outCh, nil
}

func (p *LocalProvider) ListModels(_ context.Context) ([]ModelInfo, error) {
	models := p.manager.List()
	infos := make([]ModelInfo, len(models))
	for i, m := range models {
		infos[i] = ModelInfo{
			ID:       m.Name,
			Provider: "local",
			OwnedBy:  "local",
		}
	}
	return infos, nil
}

// formatMessagesWithTemplate uses the model's GGUF chat template if available,
// falling back to the simple Role: content format.
func formatMessagesWithTemplate(loaded *model.LoadedModel, msgs []Message) string {
	if loaded.ChatTemplate == "" {
		return formatMessages(msgs)
	}

	// Build llama.ChatMessage slice for the template engine
	chatMsgs := make([]llama.ChatMessage, len(msgs))
	// Keep Go strings alive for the duration of the call
	roles := make([][]byte, len(msgs))
	contents := make([][]byte, len(msgs))

	for i, m := range msgs {
		roles[i] = append([]byte(m.Role), 0)    // null-terminated
		contents[i] = append([]byte(m.Content), 0)
		chatMsgs[i] = llama.ChatMessage{
			Role:    &roles[i][0],
			Content: &contents[i][0],
		}
	}

	// First call to get required buffer size
	needed := llama.ChatApplyTemplate(loaded.ChatTemplate, chatMsgs, true, nil)
	if needed <= 0 {
		// Template failed, fall back to simple format
		return formatMessages(msgs)
	}

	// Allocate buffer and apply template
	buf := make([]byte, needed+1)
	n := llama.ChatApplyTemplate(loaded.ChatTemplate, chatMsgs, true, buf)
	if n <= 0 {
		return formatMessages(msgs)
	}

	// Find the actual string (up to null terminator or n bytes)
	end := int(n)
	if end > len(buf) {
		end = len(buf)
	}
	// Trim any null bytes
	result := buf[:end]
	if idx := indexOf(result, 0); idx >= 0 {
		result = result[:idx]
	}

	return string(result)
}

func indexOf(b []byte, val byte) int {
	for i, v := range b {
		if v == val {
			return i
		}
	}
	return -1
}

// Ensure unsafe is used (for ChatMessage pointer fields)
var _ = unsafe.Pointer(nil)

// formatMessages converts chat messages to the simple prompt format used by local models.
func formatMessages(msgs []Message) string {
	var b strings.Builder
	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			b.WriteString("System: ")
		case "user":
			b.WriteString("User: ")
		case "assistant":
			b.WriteString("Assistant: ")
		default:
			b.WriteString("User: ")
		}
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	// Add final assistant prompt if last message isn't from assistant
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "assistant" {
		b.WriteString("Assistant:")
	}
	return b.String()
}

// toGenOpts converts a ChatRequest to generate.GenerationOptions.
func toGenOpts(req ChatRequest) generate.GenerationOptions {
	opts := generate.GenerationOptions{
		MaxTokens:    req.MaxTokens,
		Stop:         req.Stop,
		Stream:       req.Stream,
		RepeatPenalty: 1.1,
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 1024
	}
	if opts.Stop == nil {
		opts.Stop = []string{"\n", "User:", "user:"}
	}
	if req.Temperature != nil {
		opts.Temperature = *req.Temperature
	} else {
		opts.Temperature = 0.8
	}
	if req.TopP != nil {
		opts.TopP = *req.TopP
	}
	if req.TopK != nil {
		opts.TopK = *req.TopK
	} else {
		opts.TopK = 40
	}
	if req.MinP != nil {
		opts.MinP = *req.MinP
	}
	if req.Seed != nil {
		opts.Seed = *req.Seed
	}

	// Extended sampler parameters
	if req.TypicalP != nil {
		opts.TypicalP = *req.TypicalP
	}
	if req.TopNSigma != nil {
		opts.TopNSigma = *req.TopNSigma
	}
	if req.DryMultiplier != nil {
		opts.DryMultiplier = *req.DryMultiplier
		opts.DryBase = 1.75 // sensible default
	}
	if req.Grammar != "" {
		opts.Grammar = req.Grammar
	}
	if req.LogitBias != nil {
		opts.LogitBias = req.LogitBias
	}

	return opts
}
