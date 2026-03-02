package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jsoprych/guff/internal/api"
	"github.com/jsoprych/guff/internal/config"
	"github.com/jsoprych/guff/internal/model"
	"github.com/jsoprych/guff/internal/provider"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server",
	Long: `Start the HTTP API server for remote model access.

The server provides both Ollama-compatible and OpenAI-compatible endpoints:

  Ollama-compatible:
    POST /api/generate  - Generate text
    POST /api/chat      - Interactive chat
    GET  /api/tags      - List available models
    POST /api/pull      - Download a model

  OpenAI-compatible:
    POST /v1/chat/completions  - Chat completions (streaming + non-streaming)
    POST /v1/completions       - Text completions
    GET  /v1/models            - List available models

Remote providers can be configured to proxy requests to OpenAI, Anthropic, etc.
By default, the server listens on localhost:8080.`,
	Run: func(cmd *cobra.Command, args []string) {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")
		verbose, _ := cmd.Flags().GetBool("verbose")

		// Create model manager with persistence enabled
		mm := model.NewManager(appConfig)
		mm.SetKeepLoaded(true)

		// Scan for models
		if err := mm.ScanModels(); err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning models: %v\n", err)
			os.Exit(1)
		}

		// Pre-load default model if configured
		loadOpts := model.LoadOptions{
			NumGpuLayers: appConfig.Model.NumGpuLayers,
			UseMmap:      appConfig.Model.UseMmap,
			UseMlock:     appConfig.Model.UseMlock,
		}
		if defaultModel := appConfig.Model.DefaultModel; defaultModel != "" {
			fmt.Printf("Pre-loading model: %s\n", defaultModel)
			loaded, err := mm.Load(defaultModel, loadOpts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not pre-load model %q: %v\n", defaultModel, err)
			} else {
				fmt.Printf("Model ready: %s (ctx=%d, layers=%d)\n",
					loaded.Description, loaded.NCtxTrain, loaded.NLayer)
			}
		} else {
			// Pre-load first available model
			models := mm.List()
			if len(models) > 0 {
				fmt.Printf("Pre-loading model: %s\n", models[0].Name)
				loaded, err := mm.Load(models[0].Name, loadOpts)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not pre-load model %q: %v\n", models[0].Name, err)
				} else {
					fmt.Printf("Model ready: %s (ctx=%d, layers=%d)\n",
						loaded.Description, loaded.NCtxTrain, loaded.NLayer)
				}
			}
		}

		// Create API server
		server := api.NewServer(mm, appConfig)

		// Setup provider router
		router := setupProviderRouter(mm, appConfig, verbose)
		server.SetProviderRouter(router)

		addr := fmt.Sprintf("%s:%d", host, port)

		if verbose {
			fmt.Printf("Starting server on %s\n", addr)
			models := mm.List()
			fmt.Printf("Local models: %d\n", len(models))
			for _, m := range models {
				fmt.Printf("  • %s (%s, ctx=%d)\n", m.Name, m.Quantization, m.ContextLen)
			}
			if len(appConfig.Providers) > 0 {
				fmt.Printf("Remote providers: %d\n", len(appConfig.Providers))
				for name, p := range appConfig.Providers {
					fmt.Printf("  • %s (%s)\n", name, p.Type)
				}
			}
		}

		// Start server in goroutine
		srv := &http.Server{
			Addr:         addr,
			Handler:      server,
			ReadTimeout:  time.Duration(appConfig.Server.ReadTimeout) * time.Second,
			WriteTimeout: 5 * time.Minute, // long for streaming responses
			IdleTimeout:  120 * time.Second,
		}

		errCh := make(chan error, 1)
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
		}()

		fmt.Printf("guff server listening on %s\n", addr)
		fmt.Printf("  Ollama API: http://%s/api/\n", addr)
		fmt.Printf("  OpenAI API: http://%s/v1/\n", addr)

		// Wait for interrupt signal or server error
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		select {
		case <-stop:
		case err := <-errCh:
			fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("\nShutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Server shutdown error: %v\n", err)
		}
		mm.ForceUnload()
		fmt.Println("Server stopped")
	},
}

// setupProviderRouter creates and configures the provider router from config.
func setupProviderRouter(mm *model.ModelManager, cfg *config.Config, verbose bool) *provider.Router {
	router := provider.NewRouter()

	// Local provider is always available and is the fallback
	localProvider := provider.NewLocalProvider(mm, model.LoadOptions{
		NumGpuLayers: cfg.Model.NumGpuLayers,
		UseMmap:      cfg.Model.UseMmap,
		UseMlock:     cfg.Model.UseMlock,
	})
	router.RegisterProvider(localProvider)
	router.SetFallback(localProvider)

	// Register configured remote providers
	for name, pc := range cfg.Providers {
		apiKey := config.ResolveEnv(pc.APIKey)
		switch pc.Type {
		case "openai":
			p := provider.NewOpenAIProvider(apiKey, pc.BaseURL)
			router.RegisterProvider(p)
			if verbose {
				fmt.Fprintf(os.Stderr, "Registered OpenAI provider: %s\n", name)
			}
		case "anthropic":
			p := provider.NewAnthropicProvider(apiKey, pc.BaseURL)
			router.RegisterProvider(p)
			if verbose {
				fmt.Fprintf(os.Stderr, "Registered Anthropic provider: %s\n", name)
			}
		case "deepseek":
			p := provider.NewDeepSeekProvider(apiKey, pc.BaseURL)
			router.RegisterProvider(p)
			if verbose {
				fmt.Fprintf(os.Stderr, "Registered DeepSeek provider: %s\n", name)
			}
		case "openai-compatible":
			p := provider.NewOpenAICompatibleProvider(name, apiKey, pc.BaseURL, "")
			router.RegisterProvider(p)
			if verbose {
				fmt.Fprintf(os.Stderr, "Registered OpenAI-compatible provider: %s\n", name)
			}
		default:
			fmt.Fprintf(os.Stderr, "Warning: unknown provider type %q for %q\n", pc.Type, name)
		}
	}

	// Register model routes
	for alias, route := range cfg.ModelRoutes {
		providerName := route.Provider
		modelName := route.Model
		if modelName == "" {
			modelName = alias
		}

		// Find the provider by name
		p, _, err := router.Resolve(providerName + "/dummy")
		if err != nil {
			// Try to find by iterating — the provider name might match
			// We'll rely on the prefix resolution for now
			if verbose {
				fmt.Fprintf(os.Stderr, "Warning: could not resolve provider %q for route %q\n", providerName, alias)
			}
			continue
		}
		router.AddRoute(alias, p, modelName)
		if verbose {
			fmt.Fprintf(os.Stderr, "Route: %s -> %s/%s\n", alias, providerName, modelName)
		}
	}

	return router
}

func init() {
	serveCmd.Flags().String("host", "127.0.0.1", "Host to bind to")
	serveCmd.Flags().Int("port", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
}
