# Prompt Builder

guff supports composable, multi-part system prompts that assemble from multiple sources and can be overridden per model.

## Section Types

| Type | Purpose | Auto-discovery |
|------|---------|----------------|
| `base` | Core identity/behavior | `~/.config/guff/system-prompt.txt` |
| `project` | Project-specific context | `.guff/prompt.md` (walks up from CWD) |
| `tools` | Tool/MCP descriptions | Injected at runtime by tool registry |
| `user` | User preferences | `~/.config/guff/user-prompt.txt` |

## Resolution Order

Each section resolves its content through this priority chain:

1. **Inline content** -- `content:` field in config
2. **File** -- `file:` field, loaded from disk
3. **Auto-discover** -- `auto: true`, looks in conventional locations

Empty sections are silently skipped.

## Configuration

### Basic (auto-discover everything)

```yaml
prompt:
  sections:
    - type: base
      auto: true
    - type: project
      auto: true
    - type: user
      auto: true
```

This is the default configuration returned by `DefaultConfig()`.

### Inline content

```yaml
prompt:
  sections:
    - type: base
      content: "You are a helpful coding assistant. Be concise."
    - type: user
      content: "Always use Go. Prefer table-driven tests."
```

### File-based

```yaml
prompt:
  sections:
    - type: base
      file: ~/prompts/base.md
    - type: project
      file: .guff/prompt.md
```

File paths support `~/` expansion and are resolved relative to the config directory.

### Per-model overrides

```yaml
prompt:
  sections:
    - type: base
      content: "You are a helpful assistant."
  models:
    granite-3b:
      sections:
        - type: base
          content: "You are a concise coding assistant. Respond with code only."
    llama-3:
      sections:
        - type: base
          file: ~/prompts/llama-base.md
        - type: user
          content: "Use markdown formatting."
```

When a model matches a key in `models:`, that model's sections completely replace the global sections.

## Auto-Discovery

### Project Prompt (`.guff/prompt.md`)

The builder walks up from the current working directory looking for `.guff/prompt.md`:

```
/home/user/projects/myapp/src/pkg/  <- CWD
/home/user/projects/myapp/src/.guff/prompt.md  (checked)
/home/user/projects/myapp/.guff/prompt.md      (checked, used if found)
/home/user/projects/.guff/prompt.md            (checked)
/home/user/.guff/prompt.md                     (checked)
...up to filesystem root
```

This lets you put project-specific prompt context at the project root and it works from any subdirectory.

### User Prompt (`user-prompt.txt`)

Located at `~/.config/guff/user-prompt.txt`. Contains personal preferences that apply to all projects.

### Base Prompt (`system-prompt.txt`)

Located at `~/.config/guff/system-prompt.txt`. Backward-compatible with the simple `system_prompt` config.

## Runtime Injection

The `Build()` method accepts extra sections that are appended at runtime:

```go
builder := prompt.NewBuilder(cfg, configDir, workDir)

// Inject tool descriptions from the tool registry
toolSection := prompt.Section{
    Type:    prompt.SectionTools,
    Content: registry.FormatForPrompt(),
}

fullPrompt := builder.Build("granite-3b", toolSection)
```

This is how MCP tool descriptions get into the system prompt without being in the config file.

## Assembled Prompt

Sections are joined with `\n\n`:

```
You are a helpful coding assistant.

Project: myapp - A Go web service for user management.
Key files: main.go, handlers.go, models.go

You have access to the following tools:
### filesystem_list
List files in a directory
...

Always use Go. Prefer table-driven tests.
```

## API

```go
// Create builder
builder := prompt.NewBuilder(cfg, configDir, workDir)

// Build prompt for a specific model
prompt := builder.Build("granite-3b")

// Build with extra runtime sections
prompt := builder.Build("granite-3b", toolSection, extraSection)

// Get default config (3 auto-discover sections)
cfg := prompt.DefaultConfig()
```

## Relationship to System Prompt Config

The `system_prompt:` config block (simple mode) and `prompt:` config block (advanced mode) are independent. The simple mode is used by `ResolveSystemPrompt()` in `cmd/chat.go`. The advanced prompt builder is available for more sophisticated prompt assembly.

For new projects, prefer the `prompt:` config block. The `system_prompt:` block exists for backward compatibility and simple use cases.
