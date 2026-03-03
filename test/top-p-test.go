package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hybridgroup/yzma/pkg/llama"
	llamaensure "github.com/jsoprych/guff/internal/llama"
)

func main() {
	// Ensure libraries
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		log.Fatalf("Failed to get home directory: %v", homeErr)
	}
	libDir := filepath.Join(home, ".local", "share", "guff", "lib")
	if err := llamaensure.EnsureLibraries(libDir); err != nil {
		log.Fatalf("EnsureLibraries: %v", err)
	}
	fmt.Println("Libraries ensured")
	fmt.Printf("YZMA_LIB=%s\n", os.Getenv("YZMA_LIB"))

	// Load llama.cpp library
	if err := llama.Load(libDir); err != nil {
		log.Fatalf("llama.Load: %v", err)
	}
	fmt.Println("llama.cpp loaded")

	// Initialize backends
	llama.BackendInit()
	if err := llama.GGMLBackendLoadAllFromPath(libDir); err != nil {
		log.Fatalf("GGMLBackendLoadAllFromPath: %v", err)
	}
	// Set logging and initialize
	llama.LogSet(llama.LogNormal)
	llama.Init()

	// Try to load model
	modelPath := filepath.Join(home, ".local", "share", "guff", "models", "granite-3b", "model.Q4_K_M.gguf")
	if _, err := os.Stat(modelPath); err != nil {
		log.Fatalf("Model file not found at %s: %v", modelPath, err)
	}
	fmt.Printf("Loading model: %s\n", modelPath)

	params := llama.ModelDefaultParams()
	params.NGpuLayers = 0
	params.UseMmap = 1
	model, err := llama.ModelLoadFromFile(modelPath, params)
	if err != nil {
		log.Fatalf("ModelLoadFromFile: %v", err)
	}
	defer llama.ModelFree(model)
	fmt.Println("Model loaded successfully!")

	// Create context
	ctxParams := llama.ContextDefaultParams()
	ctxParams.NCtx = 2048
	ctx, err := llama.InitFromModel(model, ctxParams)
	if err != nil {
		log.Fatalf("InitFromModel: %v", err)
	}
	defer llama.Free(ctx)
	fmt.Println("Context created")

	vocab := llama.ModelGetVocab(model)

	// Test tokenization
	prompt := "func add(a, b int) int {"
	fmt.Printf("Prompt: '%s'\n", prompt)
	tokens := llama.Tokenize(vocab, prompt, true, false)
	fmt.Printf("Tokens (%d): %v\n", len(tokens), tokens)

	// Detokenize back to verify
	detok := llama.Detokenize(vocab, tokens, false, false)
	fmt.Printf("Detokenized: '%s'\n", detok)

	// Check BOS/EOS tokens
	bos := llama.VocabBOS(vocab)
	eos := llama.VocabEOS(vocab)
	eot := llama.VocabEOT(vocab)
	fmt.Printf("BOS token: %d, EOS: %d, EOT: %d\n", bos, eos, eot)

	// Create sampler with greedy + top-p
	sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
	llama.SamplerChainAdd(sampler, llama.SamplerInitGreedy())
	// Add top-p sampler with p=0.95
	llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))
	defer llama.SamplerFree(sampler)

	// Prepare batch with prompt tokens
	batch := llama.BatchGetOne(tokens)

	// Decode prompt
	fmt.Println("Decoding prompt...")
	if _, err := llama.Decode(ctx, batch); err != nil {
		log.Fatalf("Decode error: %v", err)
	}

	// Generate a few tokens
	fmt.Println("Generating 10 tokens:")
	for i := 0; i < 10; i++ {
		token := llama.SamplerSample(sampler, ctx, -1)
		fmt.Printf("  Token %d: %d\n", i, token)

		// Check if EOG
		if llama.VocabIsEOG(vocab, token) {
			fmt.Println("  (EOG token)")
			break
		}

		// Convert token to text
		buf := make([]byte, 36)
		length := llama.TokenToPiece(vocab, token, buf, 0, true)
		if length > 0 {
			fmt.Printf("  Text: '%s'\n", string(buf[:length]))
		}

		// Prepare next batch
		batch = llama.BatchGetOne([]llama.Token{token})
		if _, err := llama.Decode(ctx, batch); err != nil {
			log.Fatalf("Decode error: %v", err)
		}
	}

	fmt.Println("Debug completed")
}
