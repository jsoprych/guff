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
	"github.com/jsoprych/guff/internal/model"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server",
	Long: `Start the HTTP API server for remote model access.

The server provides REST endpoints similar to Ollama:
  • POST /api/generate - Generate text
  • POST /api/chat     - Interactive chat
  • GET  /api/tags     - List available models
  • POST /api/pull     - Download a model

By default, the server listens on localhost:8080.`,
	Run: func(cmd *cobra.Command, args []string) {
		host, _ := cmd.Flags().GetString("host")
		port, _ := cmd.Flags().GetInt("port")
		verbose, _ := cmd.Flags().GetBool("verbose")

		// Create model manager
		mm := model.NewManager(appConfig)

		// Scan for models
		if err := mm.ScanModels(); err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning models: %v\n", err)
			os.Exit(1)
		}

		// Create API server
		server := api.NewServer(mm, appConfig)
		addr := fmt.Sprintf("%s:%d", host, port)

		if verbose {
			fmt.Printf("Starting server on %s\n", addr)
			models := mm.List()
			fmt.Printf("Available models: %d\n", len(models))
			for _, m := range models {
				fmt.Printf("  • %s (%s)\n", m.Name, m.Quantization)
			}
		}

		// Start server in goroutine
		srv := &http.Server{
			Addr:    addr,
			Handler: server,
		}

		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
				os.Exit(1)
			}
		}()

		// Wait for interrupt signal
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop

		fmt.Println("\nShutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Server shutdown error: %v\n", err)
		}
		fmt.Println("Server stopped")
	},
}

func init() {
	serveCmd.Flags().String("host", "127.0.0.1", "Host to bind to")
	serveCmd.Flags().Int("port", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
}
