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
	"github.com/jsoprych/guff/internal/tools"
	"github.com/spf13/cobra"
)

// chatHistoryMsg holds a single message for in-memory (non-persist) chat history.
type chatHistoryMsg struct {
	role       string
	content    string
	tokenCount int
}

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
		systemFlag, _ := cmd.Flags().GetString("system")
		systemFile, _ := cmd.Flags().GetString("system-file")
		sessionID, _ := cmd.Flags().GetString("session")
		noPersist, _ := cmd.Flags().GetBool("no-persist")
		listSessions, _ := cmd.Flags().GetBool("list-sessions")

		// List sessions if requested
		if listSessions {
			paths, err := config.GetPaths()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting paths: %v\n", err)
				os.Exit(1)
			}
			store, err := chatstorage.NewSQLiteStorage(paths.DataDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error opening chat storage: %v\n", err)
				os.Exit(1)
			}
			defer store.Close()
			ctx := context.Background()
			sessions, err := store.ListSessions(ctx, "", 0, 0)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
				os.Exit(1)
			}
			if len(sessions) == 0 {
				fmt.Println("No saved sessions.")
				return
			}
			fmt.Printf("Found %d saved session(s):\n", len(sessions))
			for _, s := range sessions {
				fmt.Printf("  %s - %s (model: %s, messages: %d, tokens: %d, updated: %s)\n",
					s.ID, s.Title, s.ModelName, s.MessageCount, s.TotalTokens,
					s.UpdatedAt.Format("2006-01-02 15:04:05"))
			}
			return
		}

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

		// Resolve system prompt: --system > --system-file > per-model config > default
		system := appConfig.ResolveSystemPrompt(modelName, systemFlag, systemFile)
		if system != "" {
			if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
				fmt.Fprintf(os.Stderr, "System prompt: %q\n", system)
			}
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
		if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
			fmt.Fprintf(os.Stderr, "Model loaded in %v\n", loadTime)
		}

		// Create generator
		generator := generate.NewGenerator(loaded)
		defer generator.Close()

		// Context window size from model metadata
		contextWindowSize := loaded.NCtxTrain
		if contextWindowSize <= 0 {
			contextWindowSize = 2048
		}
		if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
			fmt.Fprintf(os.Stderr, "Model: %s (ctx=%d, layers=%d, embd=%d)\n",
				loaded.Description, contextWindowSize, loaded.NLayer, loaded.NEmbd)
			if loaded.ChatTemplate != "" {
				fmt.Fprintf(os.Stderr, "Chat template: available (%d chars)\n", len(loaded.ChatTemplate))
			}
		}

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

		// Initialize MCP tools if enabled
		registry := tools.NewRegistry()
		toolsEnabled, _ := cmd.Flags().GetBool("tools")
		if toolsEnabled && len(appConfig.MCP) > 0 {
			var mcpClients []*tools.MCPClient
			for name, mcpCfg := range appConfig.MCP {
				serverCfg := tools.MCPServerConfig{
					Name:    name,
					Command: mcpCfg.Command,
					Args:    mcpCfg.Args,
					Env:     mcpCfg.Env,
				}
				client, err := tools.RegisterMCPTools(ctx, registry, serverCfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: MCP server %q failed: %v\n", name, err)
					continue
				}
				mcpClients = append(mcpClients, client)
			}
			defer func() {
				for _, c := range mcpClients {
					c.Close()
				}
			}()
			if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
				if toolDefs := registry.List(); len(toolDefs) > 0 {
					fmt.Fprintf(os.Stderr, "Discovered %d tool(s) from MCP servers\n", len(toolDefs))
					for _, td := range toolDefs {
						fmt.Fprintf(os.Stderr, "  - %s: %s\n", td.Name, td.Description)
					}
				}
			}
		}

		// Inject tool descriptions into system prompt
		if toolPrompt := registry.FormatForPrompt(); toolPrompt != "" {
			system = system + "\n\n" + toolPrompt
		}

		// Initialize chat storage and session manager if persistence enabled
		var sessionManager *chatsession.SessionManager
		var currentSession *chatstorage.Session
		var tokenizer chatcontext.Tokenizer

		// Non-persist path state
		var history []chatHistoryMsg

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
			tokenizer = chatcontext.NewYzmaTokenizer(loaded.Vocab)

			// Create context manager
			ctxManager := chatcontext.NewDefaultContextManager(store, tokenizer)

			// Create session manager
			sessionManager = chatsession.NewSessionManager(store, ctxManager)
			sessionManager.SetContextSize(contextWindowSize)

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
			// In-memory history with token-aware truncation
			fmt.Fprintln(os.Stderr, "Running without persistence (history will not be saved)")
			tokenizer = chatcontext.NewYzmaTokenizer(loaded.Vocab)
			if system != "" {
				tc := tokenizer.CountTokens(system)
				history = append(history, chatHistoryMsg{role: "system", content: system, tokenCount: tc})
			}
		}

		// Initial prompt
		var initialPrompt string
		if len(args) > 0 {
			initialPrompt = args[0]
		}

		scanner := bufio.NewScanner(os.Stdin)
		fmt.Println("Chat with " + modelName + ". Type '/exit' to quit, '/clear' to clear history, '/status' for context info.")
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

			trimmed := strings.TrimSpace(userInput)

			// Process commands
			if trimmed == "/exit" {
				break
			}
			if trimmed == "/clear" {
				if !noPersist && sessionManager != nil {
					if err := sessionManager.ClearContext(ctx, sessionID); err != nil {
						fmt.Fprintf(os.Stderr, "Error clearing history: %v\n", err)
					} else {
						fmt.Println("History cleared.")
					}
				} else {
					history = nil
					if system != "" {
						tc := tokenizer.CountTokens(system)
						history = append(history, chatHistoryMsg{role: "system", content: system, tokenCount: tc})
					}
					fmt.Println("History cleared (not persisted).")
				}
				continue
			}
			if trimmed == "/status" {
				if !noPersist && sessionManager != nil {
					printDetailedStatus(ctx, sessionManager, sessionID, modelName, contextWindowSize, genOpts.MaxTokens)
				} else {
					printNoPersistStatus(history, contextWindowSize, genOpts.MaxTokens)
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

				// Tool call loop (max 5 iterations to prevent runaway)
				for i := 0; i < 5; i++ {
					call, prefixText, found := tools.ParseToolCall(response)
					if !found {
						break
					}
					if prefixText != "" {
						fmt.Print(prefixText)
					}
					fmt.Fprintf(os.Stderr, "\n[calling tool: %s]\n", call.Name)

					result := registry.Execute(ctx, *call)
					if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
						fmt.Fprintf(os.Stderr, "[tool result: %s]\n", truncateStr(result.Content, 200))
					}

					if err := sessionManager.AddMessage(ctx, sessionID, "tool", result.Content); err != nil {
						fmt.Fprintf(os.Stderr, "Error saving tool result: %v\n", err)
						break
					}
					resp, err = sessionManager.GenerateResponse(ctx, sessionID, generator, genOpts, false)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Generation error: %v\n", err)
						break
					}
					response = resp
				}
			} else {
				// Token-aware in-memory generation
				tc := tokenizer.CountTokens(userInput)
				history = append(history, chatHistoryMsg{role: "user", content: userInput, tokenCount: tc})

				// Context budget = context window - generation reserve
				contextBudget := contextWindowSize - maxTokens
				if contextBudget < 256 {
					contextBudget = 256
				}

				// Sliding window: keep system msgs + newest non-system msgs that fit
				history = slidingWindowTruncate(history, contextBudget)

				// Build prompt from history
				prompt := formatHistory(history)
				if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
					fmt.Fprintf(os.Stderr, "Prompt: %q\n", prompt)
				}

				// Generate response
				result, err := generator.Generate(ctx, prompt, genOpts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Generation error: %v\n", err)
					os.Exit(1)
				}
				response = strings.TrimSpace(result.Text)

				// Tool call loop (max 5 iterations to prevent runaway)
				for i := 0; i < 5; i++ {
					call, prefixText, found := tools.ParseToolCall(response)
					if !found {
						break
					}
					if prefixText != "" {
						fmt.Print(prefixText)
					}
					fmt.Fprintf(os.Stderr, "\n[calling tool: %s]\n", call.Name)

					toolResult := registry.Execute(ctx, *call)
					if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
						fmt.Fprintf(os.Stderr, "[tool result: %s]\n", truncateStr(toolResult.Content, 200))
					}

					tc := tokenizer.CountTokens(toolResult.Content)
					history = append(history, chatHistoryMsg{role: "tool", content: toolResult.Content, tokenCount: tc})

					prompt = formatHistory(history)
					result, err = generator.Generate(ctx, prompt, genOpts)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Generation error: %v\n", err)
						break
					}
					response = strings.TrimSpace(result.Text)
				}

				// Add assistant response to history
				respTc := tokenizer.CountTokens(response)
				history = append(history, chatHistoryMsg{role: "assistant", content: response, tokenCount: respTc})
			}
			genTime := time.Since(startGen)

			fmt.Printf("Assistant: %s\n", response)

			// Show compact status line
			if !noPersist && sessionManager != nil {
				printCompactStatus(ctx, sessionManager, sessionID)
			} else {
				printNoPersistCompactStatus(history, contextWindowSize, maxTokens)
			}

			if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
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

// slidingWindowTruncate keeps system messages and newest non-system messages within budget.
func slidingWindowTruncate(history []chatHistoryMsg, tokenBudget int) []chatHistoryMsg {
	var systemMsgs []chatHistoryMsg
	var nonSystemMsgs []chatHistoryMsg
	systemTokens := 0

	for _, msg := range history {
		if msg.role == "system" {
			systemMsgs = append(systemMsgs, msg)
			systemTokens += msg.tokenCount
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	remaining := tokenBudget - systemTokens
	if remaining < 0 {
		remaining = 0
	}

	// Walk backwards, keep newest that fit
	keepFrom := len(nonSystemMsgs)
	used := 0
	for i := len(nonSystemMsgs) - 1; i >= 0; i-- {
		used += nonSystemMsgs[i].tokenCount
		if used > remaining {
			keepFrom = i + 1
			break
		}
		if i == 0 {
			keepFrom = 0
		}
	}

	result := make([]chatHistoryMsg, 0, len(systemMsgs)+len(nonSystemMsgs)-keepFrom)
	result = append(result, systemMsgs...)
	result = append(result, nonSystemMsgs[keepFrom:]...)
	return result
}

// formatHistory formats in-memory history into a prompt string.
func formatHistory(history []chatHistoryMsg) string {
	var b strings.Builder
	for _, msg := range history {
		switch msg.role {
		case "system":
			b.WriteString("System: ")
		case "user":
			b.WriteString("User: ")
		case "assistant":
			b.WriteString("Assistant: ")
		case "tool":
			b.WriteString("Tool: ")
		}
		b.WriteString(msg.content)
		b.WriteString("\n")
	}
	b.WriteString("Assistant:")
	return b.String()
}

// printCompactStatus prints a dim one-line context status after each response.
func printCompactStatus(ctx context.Context, sm *chatsession.SessionManager, sessionID string) {
	cm := sm.ContextManager()
	status, err := cm.GetStatus(ctx, sessionID)
	if err != nil {
		return
	}
	truncMarker := ""
	if status.Truncated {
		truncMarker = " truncated"
	}
	// Dim gray ANSI: \033[2m ... \033[0m
	fmt.Fprintf(os.Stderr, "\033[2m[%d msgs | %d/%d tokens | %s%s]\033[0m\n",
		status.MessageCount, status.TotalTokens, status.TokenBudget, status.StrategyName, truncMarker)
}

// printNoPersistCompactStatus prints status for the non-persist path.
func printNoPersistCompactStatus(history []chatHistoryMsg, contextWindow, maxGenTokens int) {
	totalTokens := 0
	for _, msg := range history {
		totalTokens += msg.tokenCount
	}
	budget := contextWindow - maxGenTokens
	if budget < 256 {
		budget = 256
	}
	fmt.Fprintf(os.Stderr, "\033[2m[%d msgs | %d/%d tokens | sliding_window]\033[0m\n",
		len(history), totalTokens, budget)
}

// printDetailedStatus prints full context info for /status command.
func printDetailedStatus(ctx context.Context, sm *chatsession.SessionManager, sessionID, modelName string, contextWindow, maxGenTokens int) {
	session, err := sm.GetSession(ctx, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting session: %v\n", err)
		return
	}

	cm := sm.ContextManager()
	status, err := cm.GetStatus(ctx, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting status: %v\n", err)
		return
	}

	budget := contextWindow - maxGenTokens
	if budget < 256 {
		budget = 256
	}

	fmt.Printf("  Session: %s\n", session.ID)
	fmt.Printf("  Model: %s\n", modelName)
	fmt.Printf("  Messages: %d\n", status.MessageCount)
	fmt.Printf("  Tokens: %d / %d (context budget)\n", status.TotalTokens, budget)
	fmt.Printf("  Context window: %d (generation reserve: %d)\n", contextWindow, maxGenTokens)
	fmt.Printf("  Strategy: %s\n", status.StrategyName)
}

// printNoPersistStatus prints status for non-persist /status command.
func printNoPersistStatus(history []chatHistoryMsg, contextWindow, maxGenTokens int) {
	totalTokens := 0
	systemCount := 0
	userCount := 0
	assistantCount := 0
	for _, msg := range history {
		totalTokens += msg.tokenCount
		switch msg.role {
		case "system":
			systemCount++
		case "user":
			userCount++
		case "assistant":
			assistantCount++
		}
	}
	budget := contextWindow - maxGenTokens
	if budget < 256 {
		budget = 256
	}

	fmt.Printf("  Messages: %d (%d system, %d user, %d assistant)\n",
		len(history), systemCount, userCount, assistantCount)
	fmt.Printf("  Tokens: %d / %d (context budget)\n", totalTokens, budget)
	fmt.Printf("  Context window: %d (generation reserve: %d)\n", contextWindow, maxGenTokens)
	fmt.Printf("  Strategy: sliding_window (in-memory)\n")
}

// truncateStr truncates a string to maxLen characters, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func init() {
	chatCmd.Flags().Int("max-tokens", 1024, "Maximum number of tokens to generate per response")
	chatCmd.Flags().Float32("top-p", 0, "Top-p (nucleus) sampling threshold (0 = disabled)")
	chatCmd.Flags().Int("top-k", 40, "Top-k sampling parameter")
	chatCmd.Flags().Uint32("seed", 0, "Random seed (0 = random)")
	chatCmd.Flags().Float32("repeat-penalty", 1.1, "Penalty for repeated tokens")
	chatCmd.Flags().String("system", "", "System prompt to set behavior (highest priority)")
	chatCmd.Flags().String("system-file", "", "Load system prompt from file")
	chatCmd.Flags().Bool("interactive", false, "Stay in interactive mode even with initial prompt")
	chatCmd.Flags().String("session", "", "Session ID to resume (creates new if empty)")
	chatCmd.Flags().Bool("no-persist", false, "Do not save chat history")
	chatCmd.Flags().Bool("list-sessions", false, "List all saved sessions and exit")
	chatCmd.Flags().Bool("tools", true, "Enable MCP tool discovery and execution")

	rootCmd.AddCommand(chatCmd)
}
