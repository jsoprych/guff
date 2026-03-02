package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// PromptSection configures a single section of the multi-part system prompt.
type PromptSection struct {
	Type    string `mapstructure:"type"`              // "base", "project", "tools", "user"
	Content string `mapstructure:"content,omitempty"` // inline text
	File    string `mapstructure:"file,omitempty"`    // load from file
	Auto    bool   `mapstructure:"auto,omitempty"`    // auto-discover
}

// PromptModelConf holds per-model prompt section overrides.
type PromptModelConf struct {
	Sections []PromptSection `mapstructure:"sections"`
}

// PromptConfig holds the multi-part prompt configuration.
type PromptConfigBlock struct {
	Sections []PromptSection            `mapstructure:"sections"`
	Models   map[string]PromptModelConf `mapstructure:"models,omitempty"`
}

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
		Temperature    float32 `mapstructure:"temperature"`
		TopP           float32 `mapstructure:"top_p"`
		TopK           int     `mapstructure:"top_k"`
		MinP           float32 `mapstructure:"min_p"`
		MaxTokens      int     `mapstructure:"max_tokens"`
		RepeatPenalty  float32 `mapstructure:"repeat_penalty"`
		TypicalP       float32 `mapstructure:"typical_p"`
		TopNSigma      float32 `mapstructure:"top_n_sigma"`
		DryMultiplier  float32 `mapstructure:"dry_multiplier"`
		DryBase        float32 `mapstructure:"dry_base"`
		DryAllowedLen  int32   `mapstructure:"dry_allowed_len"`
		DryPenaltyLast int32   `mapstructure:"dry_penalty_last"`
		Grammar        string  `mapstructure:"grammar"`
	} `mapstructure:"generate"`

	// LoRA adapter
	LoRA struct {
		Path  string  `mapstructure:"path"`
		Scale float32 `mapstructure:"scale"`
	} `mapstructure:"lora"`

	// System prompt settings (simple mode — see also Prompt for multi-part)
	SystemPrompt struct {
		Default string            `mapstructure:"default"` // inline default system prompt
		File    string            `mapstructure:"file"`    // path to default system prompt file
		Models  map[string]string `mapstructure:"models"`  // model name -> system prompt text or @filepath
	} `mapstructure:"system_prompt"`

	// Multi-part prompt configuration (advanced mode)
	Prompt PromptConfigBlock `mapstructure:"prompt"`

	// Provider settings for remote API passthrough
	Providers map[string]ProviderConfig `mapstructure:"providers"`

	// Model routing: model alias -> provider/model mapping
	ModelRoutes map[string]ModelRoute `mapstructure:"model_routes"`

	// MCP server configurations
	MCP map[string]MCPConfig `mapstructure:"mcp"`

	// Hugging Face settings
	HuggingFace struct {
		Token string `mapstructure:"token"`
	} `mapstructure:"huggingface"`

	// Paths
	paths *Paths
}

// MCPConfig describes an MCP server to connect to.
type MCPConfig struct {
	Command string            `mapstructure:"command"`
	Args    []string          `mapstructure:"args"`
	Env     map[string]string `mapstructure:"env,omitempty"`
}

// ProviderConfig configures a remote API provider.
type ProviderConfig struct {
	Type    string `mapstructure:"type"`     // "openai" or "anthropic"
	APIKey  string `mapstructure:"api_key"`  // API key (supports ${ENV_VAR} syntax)
	BaseURL string `mapstructure:"base_url"` // custom base URL
}

// ModelRoute maps a model alias to a provider and actual model name.
type ModelRoute struct {
	Provider string `mapstructure:"provider"`
	Model    string `mapstructure:"model"` // actual model name at the provider
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
	v.SetDefault("generate.dry_base", 1.75)
	v.SetDefault("lora.scale", 1.0)

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

	config.paths = paths
	return &config, nil
}

// Paths returns the configuration paths
func (c *Config) Paths() *Paths {
	return c.paths
}

// ResolveSystemPrompt resolves the system prompt using the priority chain:
// cliPrompt (--system) > cliFile (--system-file) > per-model config > config default/file > ~/.config/guff/system-prompt.txt
// Returns empty string if no system prompt is configured.
func (c *Config) ResolveSystemPrompt(modelName, cliPrompt, cliFile string) string {
	// 1. Explicit --system flag
	if cliPrompt != "" {
		return cliPrompt
	}

	// 2. Explicit --system-file flag
	if cliFile != "" {
		if content, err := os.ReadFile(cliFile); err == nil {
			return strings.TrimSpace(string(content))
		}
		return ""
	}

	// 3. Per-model config
	if c.SystemPrompt.Models != nil {
		if prompt, ok := c.SystemPrompt.Models[modelName]; ok {
			return resolvePromptValue(prompt)
		}
	}

	// 4. Config default (inline or file)
	if c.SystemPrompt.Default != "" {
		return c.SystemPrompt.Default
	}
	if c.SystemPrompt.File != "" {
		if content, err := os.ReadFile(c.SystemPrompt.File); err == nil {
			return strings.TrimSpace(string(content))
		}
	}

	// 5. Default file at ~/.config/guff/system-prompt.txt
	if c.paths != nil {
		defaultFile := filepath.Join(c.paths.ConfigDir, "system-prompt.txt")
		if content, err := os.ReadFile(defaultFile); err == nil {
			return strings.TrimSpace(string(content))
		}
	}

	return ""
}

// ResolveEnv expands ${ENV_VAR} references in a string.
func ResolveEnv(s string) string {
	return os.Expand(s, os.Getenv)
}

// resolvePromptValue handles a prompt value that may be inline text or a @filepath reference.
func resolvePromptValue(val string) string {
	if strings.HasPrefix(val, "@") {
		path := val[1:]
		if content, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(content))
		}
		return ""
	}
	return val
}
