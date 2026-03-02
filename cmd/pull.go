package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/jsoprych/guff/internal/model"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull [model]",
	Short: "Download a model from Hugging Face",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		modelName := args[0]
		quantization, _ := cmd.Flags().GetString("quantization")

		// Create model manager
		mm := model.NewManager(appConfig)

		// Setup progress reporting
		ctx := context.Background()

		fmt.Printf("Pulling %s (%s)...\n", modelName, quantization)

		start := time.Now()
		err := mm.Pull(ctx, modelName, model.PullOptions{
			Quantization: quantization,
			Progress: func(downloaded, total int64) {
				if total <= 0 {
					fmt.Printf("\rDownloaded: %d MB", downloaded/1024/1024)
					return
				}
				percent := float64(downloaded) / float64(total) * 100
				fmt.Printf("\rDownloaded: %.1f%% (%d/%d MB)",
					percent, downloaded/1024/1024, total/1024/1024)
			},
		})

		if err != nil {
			fmt.Printf("\nError: %v\n", err)
			return
		}

		duration := time.Since(start)
		fmt.Printf("\n✅ Pulled %s in %.1fs\n", modelName, duration.Seconds())
	},
}

func init() {
	pullCmd.Flags().StringP("quantization", "q", "Q4_K_M", "Quantization type")
	rootCmd.AddCommand(pullCmd)
}
