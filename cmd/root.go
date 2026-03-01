package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jsoprych/guff/internal/config"
)

var (
	cfgFile   string
	appConfig *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "guff",
	Short: "Run GGUF models locally",
	Long: `guff - A local LLM runtime for GGUF models

Single binary that can run any GGUF model with features like:
  • Interactive chat
  • Modelfile support for custom models
  • OpenAI-compatible API server
  • Tool calling and MCP integration
  • Optimized for IBM Granite models`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		var err error
		appConfig, err = config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/guff/config.yaml)")
	rootCmd.PersistentFlags().StringP("model", "m", "", "Model name to use")
	rootCmd.PersistentFlags().IntP("ctx-size", "c", 0, "Context size")
	rootCmd.PersistentFlags().Float32P("temperature", "t", 0, "Temperature")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose output")

	viper.BindPFlag("model.default_model", rootCmd.PersistentFlags().Lookup("model"))
	viper.BindPFlag("model.context_size", rootCmd.PersistentFlags().Lookup("ctx-size"))
	viper.BindPFlag("generate.temperature", rootCmd.PersistentFlags().Lookup("temperature"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		paths, err := config.GetPaths()
		if err == nil {
			viper.AddConfigPath(paths.ConfigDir)
		}
		viper.AddConfigPath(".")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix("GUFF")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}
