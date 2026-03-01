package config

import (
	"os"
	"path/filepath"
)

// Paths manages all directory locations following XDG spec
type Paths struct {
	// Data directory: ~/.local/share/guff/
	DataDir string

	// Config directory: ~/.config/guff/
	ConfigDir string

	// Cache directory: ~/.cache/guff/
	CacheDir string
}

func GetPaths() (*Paths, error) {
	paths := &Paths{}

	// Data directory (models)
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		paths.DataDir = filepath.Join(xdgData, "guff")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		paths.DataDir = filepath.Join(home, ".local", "share", "guff")
	}

	// Config directory
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		paths.ConfigDir = filepath.Join(xdgConfig, "guff")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		paths.ConfigDir = filepath.Join(home, ".config", "guff")
	}

	// Cache directory
	if xdgCache := os.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
		paths.CacheDir = filepath.Join(xdgCache, "guff")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		paths.CacheDir = filepath.Join(home, ".cache", "guff")
	}

	// Create directories if they don't exist
	for _, dir := range []string{paths.DataDir, paths.ConfigDir, paths.CacheDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	return paths, nil
}

// ModelsDir returns the directory where models are stored
func (p *Paths) ModelsDir() string {
	return filepath.Join(p.DataDir, "models")
}

// ModelPath returns the path for a specific model
func (p *Paths) ModelPath(name, quantization string) string {
	return filepath.Join(p.ModelsDir(), name, "model."+quantization+".gguf")
}

// LibDir returns the directory for llama.cpp libraries
func (p *Paths) LibDir() string {
	return filepath.Join(p.DataDir, "lib")
}
