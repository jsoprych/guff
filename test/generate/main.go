package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jsoprych/guff/internal/config"
	"github.com/jsoprych/guff/internal/generate"
	"github.com/jsoprych/guff/internal/model"
)

func main() {
	// Set environment variable for yzma library auto-detection
	os.Setenv("YZMA_LIB", "")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create model manager
	mgr := model.NewManager(cfg)

	// Scan for models
	fmt.Println("Scanning for models...")
	if err := mgr.ScanModels(); err != nil {
		log.Fatalf("Failed to scan models: %v", err)
	}

	// List models
	models := mgr.List()
	if len(models) == 0 {
		log.Fatal("No models found. Please download a model first.")
	}

	fmt.Printf("Found %d model(s):\n", len(models))
	for _, m := range models {
		fmt.Printf("  - %s (%s, %s, %d bytes)\n", m.Name, m.Quantization, m.Architecture, m.Size)
	}

	// Try to load the first model (granite-3b)
	modelName := models[0].Name
	fmt.Printf("\nLoading model '%s'...\n", modelName)

	opts := model.LoadOptions{
		NumGpuLayers: 0, // CPU only for now
		UseMmap:      true,
		UseMlock:     false,
	}

	start := time.Now()
	loaded, err := mgr.Load(modelName, opts)
	if err != nil {
		log.Fatalf("Failed to load model: %v", err)
	}
	elapsed := time.Since(start)

	fmt.Printf("Model loaded successfully in %v\n", elapsed)
	fmt.Printf("  Path: %s\n", loaded.Info.Path)
	fmt.Printf("  Context size: %d\n", loaded.Info.ContextLen)

	// Create generator
	fmt.Println("\nCreating generator...")
	generator := generate.NewGenerator(loaded)
	defer generator.Close()

	// Generate text
	prompt := "Hello, how are you?"
	fmt.Printf("Prompt: %s\n", prompt)

	genOpts := generate.GenerationOptions{
		Temperature:   0.8,
		TopP:          0.95,
		TopK:          40,
		MaxTokens:     10,
		Stop:          []string{},
		Seed:          42,
		RepeatPenalty: 1.1,
		Stream:        false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Generating...")
	startGen := time.Now()
	result, err := generator.Generate(ctx, prompt, genOpts)
	if err != nil {
		log.Fatalf("Generation failed: %v", err)
	}
	genElapsed := time.Since(startGen)

	fmt.Printf("Generation completed in %v\n", genElapsed)
	fmt.Printf("Prompt tokens: %d\n", result.PromptTokens)
	fmt.Printf("Generated tokens: %d\n", result.GenTokens)
	fmt.Printf("Output:\n%s\n", result.Text)
	fmt.Printf("Tokens: %v\n", result.Tokens)

	// Unload model
	fmt.Println("\nUnloading model...")
	if err := mgr.Unload(); err != nil {
		log.Fatalf("Failed to unload model: %v", err)
	}
	fmt.Println("Model unloaded. Test passed.")
}
