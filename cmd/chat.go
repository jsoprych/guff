package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	chatcontext "github.com/jsoprych/guff/internal/chat/context"
	chatsession "github.com/jsoprych/guff/internal/chat/session"
	chatstorage "github.com/jsoprych/guff/internal/chat/storage"
	"github.com/jsoprych/guff/internal/config"
	"github.com/jsoprych/guff/internal/generate"
	"github.com/jsoprych/guff/internal/model"
	"github.com/spf13/cobra"
)

var chatCmd = &cobra.Command{
	Use:   "chat [initial prompt]",
	Short: "Interactive chat with a model",
	Long: `Start an interactive chat session.

If an initial prompt is provided, the model will respond to it and then enter interactive mode.
Otherwise, you will be prompted for input.

Press Ctrl+D or type '/exit' to quit.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Parse flags
		temperature, _ := cmd.Flags().GetFloat32("temperature")
		maxTokens, _ := cmd.Flags().GetInt("max-tokens")
		topP, _ := cmd.Flags().GetFloat32("top-p")
		topK, _ := cmd.Flags().GetInt("top-k")
		seed, _ := cmd.Flags().GetUint32("seed")
		repeatPenalty, _ := cmd.Flags().GetFloat32("repeat-penalty")
		system, _ := cmd.Flags().GetString("system")
		sessionID, _ := cmd.Flags().GetString("session")
		noPersist, _ := cmd.Flags().GetBool("no-persist")

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
			modelName = appConfig.Model.DefaultModel
		}
		if modelName == "" {
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
			NumGpuLayers: 0,
			UseMmap:      true,
			UseMlock:     false,
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
		if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
			fmt.Fprintf(os.Stderr, "Model loaded in %v\n", loadTime)
		}

		// Create generator
		generator := generate.NewGenerator(loaded)
		defer generator.Close()

		// Prepare base generation options
		genOpts := generate.GenerationOptions{
			Temperature:      temperature,
			TopP:             topP,
			TopK:             topK,
			MaxTokens:        maxTokens,
			Stop:             []string{"\n", "User:", "user:"},
			Seed:             seed,
			RepeatPenalty:    repeatPenalty,
			FrequencyPenalty: 0.0,
			PresencePenalty:  0.0,
			Mirostat:         0,
			Grammar:          "",
			Stream:           false,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Initialize chat storage and session manager if persistence enabled
		var sessionManager *chatsession.SessionManager
		var currentSession *chatstorage.Session
		var history []string // used only when noPersist is true

		if !noPersist {
			// Get paths
			paths, err := config.GetPaths()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting paths: %v\n", err)
				os.Exit(1)
			}

			// Create storage
			store, err := chatstorage.NewSQLiteStorage(paths.DataDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating chat storage: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()

			// Create tokenizer using loaded model's vocabulary
			tokenizer := chatcontext.NewYzmaTokenizer(loaded.Vocab)

			// Create context manager
			ctxManager := chatcontext.NewDefaultContextManager(store, tokenizer)

			// Create session manager
			sessionManager = chatsession.NewSessionManager(store, ctxManager)

			// Get or create session
			if sessionID == "" {
				// Create new session with model name as label
				session, err := sessionManager.CreateSession(ctx, modelName, fmt.Sprintf("Chat with %s", modelName))
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
					os.Exit(1)
				}
				currentSession = session
				sessionID = session.ID
				fmt.Fprintf(os.Stderr, "Created new session: %s\n", sessionID)
			} else {
				// Load existing session
				session, err := sessionManager.GetSession(ctx, sessionID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error loading session: %v\n", err)
					os.Exit(1)
				}
				if session == nil {
					fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
					os.Exit(1)
				}
				currentSession = session
				// Check if session model matches current model
				if session.ModelName != modelName {
					fmt.Fprintf(os.Stderr, "Warning: session model (%s) differs from current model (%s)\n",
						session.ModelName, modelName)
				}
				fmt.Fprintf(os.Stderr, "Resumed session: %s (%d messages)\n", sessionID, session.MessageCount)
			}

			// Add system prompt if provided
			if system != "" {
				if err := sessionManager.AddMessage(ctx, sessionID, "system", system); err != nil {
					fmt.Fprintf(os.Stderr, "Error adding system message: %v\n", err)
					os.Exit(1)
				}
			}
		} else {
			// In-memory history (legacy behavior)
			fmt.Fprintln(os.Stderr, "Running without persistence (history will not be saved)")
			// Initialize in-memory history with system prompt
			if system != "" {
				history = append(history, "System: "+system)
			}
		}

		// Initial prompt
		var initialPrompt string
		if len(args) > 0 {
			initialPrompt = args[0]
		}

		scanner := bufio.NewScanner(os.Stdin)
		fmt.Println("Chat with " + modelName + ". Type '/exit' to quit, '/clear' to clear history.")
		if !noPersist {
			fmt.Printf("Session: %s\n", sessionID)
		}

		for {
			var userInput string
			if initialPrompt != "" {
				userInput = initialPrompt
				initialPrompt = ""
				fmt.Printf("User: %s\n", userInput)
			} else {
				fmt.Print("User: ")
				if !scanner.Scan() {
					break // EOF
				}
				userInput = scanner.Text()
				if err := scanner.Err(); err != nil {
					fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
					break
				}
			}

			// Process commands
			if strings.TrimSpace(userInput) == "/exit" {
				break
			}
			if strings.TrimSpace(userInput) == "/clear" {
				if !noPersist && sessionManager != nil {
					// Clear session messages
					if err := sessionManager.ClearContext(ctx, sessionID); err != nil {
						fmt.Fprintf(os.Stderr, "Error clearing history: %v\n", err)
					} else {
						fmt.Println("History cleared.")
					}
				} else {
					history = nil
					// Re-add system prompt if it existed
					if system != "" {
						history = append(history, "System: "+system)
					}
					fmt.Println("History cleared (not persisted).")
				}
				continue
			}

			// Add user message
			if !noPersist && sessionManager != nil {
				if err := sessionManager.AddMessage(ctx, sessionID, "user", userInput); err != nil {
					fmt.Fprintf(os.Stderr, "Error saving user message: %v\n", err)
				}
			}

			// Generate response
			startGen := time.Now()
			var response string
			if !noPersist && sessionManager != nil {
				// Use session manager for generation
				resp, err := sessionManager.GenerateResponse(ctx, sessionID, generator, genOpts, false)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Generation error: %v\n", err)
					os.Exit(1)
				}
				response = resp
			} else {
				// Legacy generation (in-memory history)
				// Add user message to history
				history = append(history, "User: "+userInput)

				// Build prompt from history
				prompt := strings.Join(history, "\n") + "\nAssistant:"

				// Generate response
				result, err := generator.Generate(ctx, prompt, genOpts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Generation error: %v\n", err)
					os.Exit(1)
				}
				response = strings.TrimSpace(result.Text)

				// Add assistant response to history
				history = append(history, "Assistant: "+response)
			}
			genTime := time.Since(startGen)

			fmt.Printf("Assistant: %s\n", response)

			if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
				// TODO: get token count from generation result
				fmt.Fprintf(os.Stderr, "Generated in %v\n", genTime)
			}

			// If we had an initial prompt, exit after one turn unless interactive flag
			if len(args) > 0 {
				interactive, _ := cmd.Flags().GetBool("interactive")
				if !interactive {
					break
				}
			}
		}

		if !noPersist && currentSession != nil {
			fmt.Fprintf(os.Stderr, "Session saved: %s (%d messages)\n",
				currentSession.ID, currentSession.MessageCount)
		}
	},
}

func init() {
	chatCmd.Flags().Int("max-tokens", 1024, "Maximum number of tokens to generate per response")
	chatCmd.Flags().Float32("top-p", 0.95, "Top-p sampling parameter")
	chatCmd.Flags().Int("top-k", 40, "Top-k sampling parameter")
	chatCmd.Flags().Uint32("seed", 0, "Random seed (0 = random)")
	chatCmd.Flags().Float32("repeat-penalty", 1.1, "Penalty for repeated tokens")
	chatCmd.Flags().String("system", "", "System prompt to set behavior")
	chatCmd.Flags().Bool("interactive", false, "Stay in interactive mode even with initial prompt")
	chatCmd.Flags().String("session", "", "Session ID to resume (creates new if empty)")
	chatCmd.Flags().Bool("no-persist", false, "Do not save chat history")

	rootCmd.AddCommand(chatCmd)
}
