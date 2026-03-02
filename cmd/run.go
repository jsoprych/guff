package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jsoprych/guff/internal/generate"
	"github.com/jsoprych/guff/internal/model"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Run a model with a prompt",
	Long: `Generate text using a loaded model.

If no prompt is provided as an argument, reads from stdin.
The model must already be downloaded (use 'guff pull' first).`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		verbose, _ := cmd.Flags().GetBool("verbose")
		if verbose {
			fmt.Fprintf(os.Stderr, "[DEBUG] Run command started\n")
		}
		// Determine prompt
		var prompt string
		if len(args) > 0 {
			prompt = args[0]
		} else {
			// Read from stdin
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
				os.Exit(1)
			}
			prompt = string(data)
		}
		if prompt == "" {
			fmt.Fprintln(os.Stderr, "Error: empty prompt")
			os.Exit(1)
		}

		// Parse flags
		temperature, _ := cmd.Flags().GetFloat32("temperature")
		maxTokens, _ := cmd.Flags().GetInt("max-tokens")
		topP, _ := cmd.Flags().GetFloat32("top-p")
		topK, _ := cmd.Flags().GetInt("top-k")
		seed, _ := cmd.Flags().GetUint32("seed")
		repeatPenalty, _ := cmd.Flags().GetFloat32("repeat-penalty")
		stopStrings, _ := cmd.Flags().GetStringSlice("stop")
		stream, _ := cmd.Flags().GetBool("stream")

		// Create model manager
		mm := model.NewManager(appConfig)

		// Scan for models
		if err := mm.ScanModels(); err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning models: %v\n", err)
			os.Exit(1)
		}

		// Determine model name
		modelName, _ := cmd.Flags().GetString("model")
		if modelName == "" {
			// Use default from config
			modelName = appConfig.Model.DefaultModel
		}
		if modelName == "" {
			// Pick first available model
			models := mm.List()
			if len(models) == 0 {
				fmt.Fprintln(os.Stderr, "No models found. Use 'guff pull' to download a model.")
				os.Exit(1)
			}
			modelName = models[0].Name
			fmt.Fprintf(os.Stderr, "Using model: %s\n", modelName)
		}

		// Load options
		loadOpts := model.LoadOptions{
			NumGpuLayers: appConfig.Model.NumGpuLayers,
			UseMmap:      appConfig.Model.UseMmap,
			UseMlock:     appConfig.Model.UseMlock,
		}

		// Load model
		startLoad := time.Now()
		loaded, err := mm.Load(modelName, loadOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading model: %v\n", err)
			os.Exit(1)
		}
		defer mm.Unload()
		loadTime := time.Since(startLoad)
		if verbose {
			fmt.Fprintf(os.Stderr, "Model loaded in %v\n", loadTime)
		}

		// Create generator
		generator := generate.NewGenerator(loaded)
		defer generator.Close()

		// Prepare generation options
		genOpts := generate.GenerationOptions{
			Temperature:      temperature,
			TopP:             topP,
			TopK:             topK,
			MaxTokens:        maxTokens,
			Stop:             stopStrings,
			Seed:             seed,
			RepeatPenalty:    repeatPenalty,
			FrequencyPenalty: 0.0,
			PresencePenalty:  0.0,
			Mirostat:         0,
			Grammar:          "",
			Stream:           stream,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if stream {
			// Streaming generation
			ch := generator.GenerateStream(ctx, prompt, genOpts)
			var fullText strings.Builder
			var genTokens int

			for chunk := range ch {
				if chunk.Error != nil {
					fmt.Fprintf(os.Stderr, "Generation error: %v\n", chunk.Error)
					os.Exit(1)
				}

				if chunk.Token != "" {
					fmt.Print(chunk.Token)
					fullText.WriteString(chunk.Token)
					genTokens++
				}

				if chunk.Done {
					break
				}
			}

			if verbose {
				fmt.Fprintf(os.Stderr, "\nGenerated %d tokens\n", genTokens)
			}
		} else {
			// Non-streaming generation
			startGen := time.Now()
			if verbose {
				fmt.Fprintf(os.Stderr, "[DEBUG] Calling generator.Generate with prompt: %q\n", prompt)
			}
			result, err := generator.Generate(ctx, prompt, genOpts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Generation error: %v\n", err)
				os.Exit(1)
			}
			genTime := time.Since(startGen)

			// Output result
			fmt.Print(result.Text)
			if verbose {
				fmt.Fprintf(os.Stderr, "\nGenerated %d tokens in %v (%.1f tokens/sec)\n",
					result.GenTokens, genTime, float64(result.GenTokens)/genTime.Seconds())
			}
		}
	},
}

func init() {
	runCmd.Flags().Int("max-tokens", 512, "Maximum number of tokens to generate")
	runCmd.Flags().Float32("top-p", 0, "Top-p (nucleus) sampling threshold (0 = disabled)")
	runCmd.Flags().Int("top-k", 40, "Top-k sampling parameter")
	runCmd.Flags().Uint32("seed", 0, "Random seed (0 = random)")
	runCmd.Flags().Float32("repeat-penalty", 1.1, "Penalty for repeated tokens")
	runCmd.Flags().StringSlice("stop", []string{"\n"}, "Stop strings (default newline)")
	runCmd.Flags().Bool("stream", false, "Stream output token by token")
	// Note: temperature flag already exists in root command

	rootCmd.AddCommand(runCmd)
}
