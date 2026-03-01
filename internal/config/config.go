package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	// Server settings
	Server struct {
		Host        string `mapstructure:"host"`
		Port        int    `mapstructure:"port"`
		ReadTimeout int    `mapstructure:"read_timeout"`
	} `mapstructure:"server"`

	// Model settings
	Model struct {
		DefaultModel string `mapstructure:"default_model"`
		DefaultQuant string `mapstructure:"default_quant"`
		NumGpuLayers int    `mapstructure:"num_gpu_layers"`
		UseMmap      bool   `mapstructure:"use_mmap"`
		UseMlock     bool   `mapstructure:"use_mlock"`
	} `mapstructure:"model"`

	// Generation defaults
	Generate struct {
		Temperature   float32 `mapstructure:"temperature"`
		TopP          float32 `mapstructure:"top_p"`
		TopK          int     `mapstructure:"top_k"`
		MaxTokens     int     `mapstructure:"max_tokens"`
		RepeatPenalty float32 `mapstructure:"repeat_penalty"`
	} `mapstructure:"generate"`

	// Hugging Face settings
	HuggingFace struct {
		Token string `mapstructure:"token"`
	} `mapstructure:"huggingface"`

	// Paths
	paths *Paths
}

func Load() (*Config, error) {
	paths, err := GetPaths()
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(paths.ConfigDir)
	v.AddConfigPath(".")

	// Enable environment variable binding
	v.AutomaticEnv()
	v.SetEnvPrefix("GUFF")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Set defaults
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", 60)

	v.SetDefault("model.default_quant", "q4_K_M")
	v.SetDefault("model.num_gpu_layers", 35)
	v.SetDefault("model.use_mmap", true)
	v.SetDefault("model.use_mlock", false)

	v.SetDefault("generate.temperature", 0.8)
	v.SetDefault("generate.top_p", 0.9)
	v.SetDefault("generate.top_k", 40)
	v.SetDefault("generate.max_tokens", 2048)
	v.SetDefault("generate.repeat_penalty", 1.1)

	// Read config
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// Config file not found - use defaults
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	// Debug: log token presence (masked)
	if config.HuggingFace.Token != "" {
		masked := config.HuggingFace.Token
		if len(masked) > 8 {
			masked = masked[:4] + "..." + masked[len(masked)-4:]
		}
		fmt.Printf("[config] Hugging Face token present: %s\n", masked)
	}

	config.paths = paths
	return &config, nil
}

// Paths returns the configuration paths
func (c *Config) Paths() *Paths {
	return c.paths
}
