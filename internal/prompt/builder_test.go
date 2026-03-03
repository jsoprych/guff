package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuilderInlineContent(t *testing.T) {
	cfg := PromptConfig{
		Sections: []Section{
			{Type: SectionBase, Content: "You are a helpful assistant."},
			{Type: SectionUser, Content: "Always be concise."},
		},
	}
	b := NewBuilder(cfg, "", "")
	result := b.Build("test-model")
	expected := "You are a helpful assistant.\n\nAlways be concise."
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestBuilderFileContent(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "base.txt")
	os.WriteFile(promptFile, []byte("Base prompt from file"), 0644)

	cfg := PromptConfig{
		Sections: []Section{
			{Type: SectionBase, File: promptFile},
		},
	}
	b := NewBuilder(cfg, tmpDir, "")
	result := b.Build("test-model")
	if result != "Base prompt from file" {
		t.Errorf("Expected 'Base prompt from file', got %q", result)
	}
}

func TestBuilderAutoDiscoverProject(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .guff/prompt.md
	guffDir := filepath.Join(tmpDir, ".guff")
	os.MkdirAll(guffDir, 0755)
	os.WriteFile(filepath.Join(guffDir, "prompt.md"), []byte("Project: test app"), 0644)

	cfg := PromptConfig{
		Sections: []Section{
			{Type: SectionProject, Auto: true},
		},
	}
	b := NewBuilder(cfg, "", tmpDir)
	result := b.Build("test-model")
	if result != "Project: test app" {
		t.Errorf("Expected 'Project: test app', got %q", result)
	}
}

func TestBuilderAutoDiscoverUser(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "user-prompt.txt"), []byte("User preferences here"), 0644)

	cfg := PromptConfig{
		Sections: []Section{
			{Type: SectionUser, Auto: true},
		},
	}
	b := NewBuilder(cfg, tmpDir, "")
	result := b.Build("test-model")
	if result != "User preferences here" {
		t.Errorf("Expected 'User preferences here', got %q", result)
	}
}

func TestBuilderPerModelOverride(t *testing.T) {
	cfg := PromptConfig{
		Sections: []Section{
			{Type: SectionBase, Content: "Default base"},
		},
		Models: map[string]ModelConf{
			"granite-3b": {
				Sections: []Section{
					{Type: SectionBase, Content: "Granite-specific base"},
				},
			},
		},
	}
	b := NewBuilder(cfg, "", "")

	// Default model
	result := b.Build("llama-3")
	if result != "Default base" {
		t.Errorf("Expected 'Default base', got %q", result)
	}

	// Overridden model
	result = b.Build("granite-3b")
	if result != "Granite-specific base" {
		t.Errorf("Expected 'Granite-specific base', got %q", result)
	}
}

func TestBuilderExtras(t *testing.T) {
	cfg := PromptConfig{
		Sections: []Section{
			{Type: SectionBase, Content: "Base"},
		},
	}
	b := NewBuilder(cfg, "", "")

	tools := Section{Type: SectionTools, Content: "Available tools: search, calculator"}
	result := b.Build("test", tools)
	expected := "Base\n\nAvailable tools: search, calculator"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestBuilderEmptySections(t *testing.T) {
	cfg := PromptConfig{
		Sections: []Section{
			{Type: SectionBase, Content: "Base"},
			{Type: SectionProject, Auto: true}, // won't find anything
			{Type: SectionUser, Content: ""},   // empty
		},
	}
	b := NewBuilder(cfg, "", "/nonexistent")
	result := b.Build("test")
	// Only "Base" should appear since other sections resolve to empty
	if result != "Base" {
		t.Errorf("Expected 'Base', got %q", result)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.Sections) != 3 {
		t.Errorf("Expected 3 default sections, got %d", len(cfg.Sections))
	}
	for _, s := range cfg.Sections {
		if !s.Auto {
			t.Errorf("Expected all default sections to be Auto, section %s is not", s.Type)
		}
	}
}

func TestBuilderProjectPromptWalkUp(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .guff/prompt.md in the root
	guffDir := filepath.Join(tmpDir, ".guff")
	os.MkdirAll(guffDir, 0755)
	os.WriteFile(filepath.Join(guffDir, "prompt.md"), []byte("Root project prompt"), 0644)

	// Work dir is a subdirectory
	subDir := filepath.Join(tmpDir, "src", "pkg")
	os.MkdirAll(subDir, 0755)

	cfg := PromptConfig{
		Sections: []Section{
			{Type: SectionProject, Auto: true},
		},
	}
	b := NewBuilder(cfg, "", subDir)
	result := b.Build("test")
	if result != "Root project prompt" {
		t.Errorf("Expected 'Root project prompt', got %q", result)
	}
}
