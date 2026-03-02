package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

// SectionType identifies what kind of prompt section this is.
type SectionType string

const (
	SectionBase    SectionType = "base"    // core identity/behavior
	SectionProject SectionType = "project" // project-specific context (.guff/prompt.md)
	SectionTools   SectionType = "tools"   // tool/MCP descriptions (injected when active)
	SectionUser    SectionType = "user"    // user preferences
)

// Section is a single piece of the system prompt.
type Section struct {
	Type    SectionType `yaml:"type" mapstructure:"type"`
	Content string      `yaml:"content,omitempty" mapstructure:"content"` // inline text
	File    string      `yaml:"file,omitempty" mapstructure:"file"`       // load from file
	Auto    bool        `yaml:"auto,omitempty" mapstructure:"auto"`       // auto-discover
}

// PromptConfig holds the prompt configuration from config.yaml.
type PromptConfig struct {
	Sections []Section            `yaml:"sections" mapstructure:"sections"`
	Models   map[string]ModelConf `yaml:"models,omitempty" mapstructure:"models"`
}

// ModelConf holds per-model prompt overrides.
type ModelConf struct {
	Sections []Section `yaml:"sections" mapstructure:"sections"`
}

// Builder assembles a system prompt from multiple sections.
type Builder struct {
	config    PromptConfig
	configDir string // e.g. ~/.config/guff/
	workDir   string // current working directory
}

// NewBuilder creates a prompt builder.
func NewBuilder(cfg PromptConfig, configDir, workDir string) *Builder {
	return &Builder{
		config:    cfg,
		configDir: configDir,
		workDir:   workDir,
	}
}

// Build assembles the full system prompt for the given model.
// Priority: per-model sections (if configured) > global sections.
// Additional sections (e.g. tool descriptions) can be injected via extras.
func (b *Builder) Build(modelName string, extras ...Section) string {
	sections := b.config.Sections

	// Check for per-model override
	if mc, ok := b.config.Models[modelName]; ok && len(mc.Sections) > 0 {
		sections = mc.Sections
	}

	var parts []string

	for _, s := range sections {
		text := b.resolveSection(s)
		if text != "" {
			parts = append(parts, text)
		}
	}

	// Append extras (e.g. tool descriptions injected at runtime)
	for _, s := range extras {
		text := b.resolveSection(s)
		if text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n\n")
}

// resolveSection resolves a section to its text content.
func (b *Builder) resolveSection(s Section) string {
	// Inline content takes priority
	if s.Content != "" {
		return strings.TrimSpace(s.Content)
	}

	// Load from explicit file
	if s.File != "" {
		path := b.expandPath(s.File)
		if content, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(content))
		}
		return ""
	}

	// Auto-discover based on type
	if s.Auto {
		return b.autoDiscover(s.Type)
	}

	return ""
}

// autoDiscover looks for conventional files based on section type.
func (b *Builder) autoDiscover(t SectionType) string {
	switch t {
	case SectionProject:
		// Look for .guff/prompt.md in working directory (and parents)
		return b.findProjectPrompt()
	case SectionUser:
		// Look for user-prompt.txt in config dir
		path := filepath.Join(b.configDir, "user-prompt.txt")
		if content, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(content))
		}
	case SectionBase:
		// Look for system-prompt.txt in config dir (backward compat)
		path := filepath.Join(b.configDir, "system-prompt.txt")
		if content, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(content))
		}
	}
	return ""
}

// findProjectPrompt walks up from workDir looking for .guff/prompt.md.
func (b *Builder) findProjectPrompt() string {
	dir := b.workDir
	for {
		candidate := filepath.Join(dir, ".guff", "prompt.md")
		if content, err := os.ReadFile(candidate); err == nil {
			return strings.TrimSpace(string(content))
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}
	return ""
}

// expandPath resolves ~ and relative paths.
func (b *Builder) expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(b.configDir, path)
	}
	return path
}

// DefaultConfig returns a minimal default prompt configuration.
func DefaultConfig() PromptConfig {
	return PromptConfig{
		Sections: []Section{
			{Type: SectionBase, Auto: true},
			{Type: SectionProject, Auto: true},
			{Type: SectionUser, Auto: true},
		},
	}
}
