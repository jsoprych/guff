# Configuration

## File Locations

guff uses XDG base directories:

| Purpose | Path | Contains |
|---------|------|----------|
| Config | `~/.config/guff/` | `config.yaml`, `system-prompt.txt`, `user-prompt.txt` |
| Data | `~/.local/share/guff/` | Models, SQLite databases, llama.cpp libraries |
| Cache | `~/.cache/guff/` | Temporary downloads |

## Config File

`~/.config/guff/config.yaml`

```yaml
# Server settings (for `guff serve`)
server:
  host: "127.0.0.1"    # default
  port: 8080            # default
  read_timeout: 60      # seconds, default

# Model settings
model:
  default_model: ""          # auto-selects first available if empty
  default_quant: "q4_K_M"   # default quantization for downloads
  num_gpu_layers: 35         # layers offloaded to GPU (0 = CPU only)
  use_mmap: true             # memory-mapped model loading
  use_mlock: false           # lock model in RAM (prevents swapping)

# Generation defaults
generate:
  temperature: 0.8      # 0.0 = deterministic, higher = more random
  top_p: 0.9            # nucleus sampling threshold
  top_k: 40             # top-k candidate count
  min_p: 0.0            # min-p filtering threshold
  max_tokens: 2048      # max tokens per generation
  repeat_penalty: 1.1   # penalty for token repetition
  typical_p: 0.0        # typical sampling (0 = disabled)
  top_n_sigma: 0.0      # top-n-sigma filter (0 = disabled)
  dry_multiplier: 0.0   # DRY anti-repetition (0 = disabled)
  dry_base: 1.75        # DRY exponential base
  dry_allowed_len: 0    # DRY allowed repetition length
  dry_penalty_last: 0   # DRY lookback window
  grammar: ""           # GBNF grammar for constrained output

# LoRA adapter
lora:
  path: ""              # path to LoRA adapter file
  scale: 1.0            # LoRA scaling factor

# System prompt (simple mode)
system_prompt:
  default: ""                           # inline default prompt
  file: ""                              # load default from file
  models:                               # per-model overrides
    granite-3b: "You are a coding assistant."
    llama-3: "@/path/to/llama-prompt.txt"  # @ prefix loads from file

# Multi-part prompt (advanced mode -- see prompt-builder.md)
prompt:
  sections:
    - type: base
      auto: true
    - type: project
      auto: true
    - type: user
      auto: true
  models:
    granite-3b:
      sections:
        - type: base
          content: "You are a concise coding assistant."

# Remote API providers
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}          # env var expansion
  anthropic:
    type: anthropic
    api_key: ${ANTHROPIC_API_KEY}
  deepseek:
    type: deepseek
    api_key: ${DEEPSEEK_API_KEY}
  together:
    type: openai-compatible
    api_key: ${TOGETHER_API_KEY}
    base_url: https://api.together.xyz/v1

# Model routing aliases
model_routes:
  gpt-4o:
    provider: openai
    model: gpt-4o
  sonnet:
    provider: anthropic
    model: claude-sonnet-4-5-20250929
  deepseek-coder:
    provider: deepseek
    model: deepseek-coder

# MCP server connections
mcp:
  filesystem:
    command: npx
    args: ["-y", "@anthropic/mcp-filesystem", "/home/user/projects"]
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: ${GITHUB_TOKEN}

# HuggingFace settings
huggingface:
  token: ""   # or set GUFF_HUGGINGFACE_TOKEN env var
```

## Environment Variables

All config values can be overridden with `GUFF_` prefix and `_` separators:

| Variable | Config Path | Description |
|----------|-------------|-------------|
| `GUFF_HUGGINGFACE_TOKEN` | `huggingface.token` | HuggingFace API token |
| `GUFF_SERVER_PORT` | `server.port` | Server listen port |
| `GUFF_SERVER_HOST` | `server.host` | Server bind address |
| `GUFF_MODEL_DEFAULT_MODEL` | `model.default_model` | Default model name |
| `GUFF_MODEL_NUM_GPU_LAYERS` | `model.num_gpu_layers` | GPU layers to offload |
| `GUFF_GENERATE_TEMPERATURE` | `generate.temperature` | Default temperature |
| `GUFF_GENERATE_MAX_TOKENS` | `generate.max_tokens` | Default max tokens |
| `YZMA_LIB` | (external) | Override llama.cpp library path |

API keys in provider configs support `${ENV_VAR}` expansion syntax.

## CLI Flags

### Global flags (all commands)

| Flag | Default | Description |
|------|---------|-------------|
| `--model`, `-m` | (auto) | Model name |
| `--temperature`, `-t` | 0.8 | Sampling temperature |
| `--verbose`, `-v` | false | Verbose output |

### `guff run`

| Flag | Default | Description |
|------|---------|-------------|
| `--max-tokens` | 512 | Max tokens to generate |
| `--top-p` | 0 | Top-p threshold (0 = disabled) |
| `--top-k` | 40 | Top-k candidates |
| `--seed` | 0 | Random seed (0 = random) |
| `--repeat-penalty` | 1.1 | Repeat penalty |
| `--stop` | `\n` | Stop strings (comma-separated) |
| `--stream` | false | Stream tokens to stdout |

### `guff chat`

| Flag | Default | Description |
|------|---------|-------------|
| `--max-tokens` | 1024 | Max tokens per response |
| `--top-p` | 0 | Top-p threshold (0 = disabled) |
| `--top-k` | 40 | Top-k candidates |
| `--seed` | 0 | Random seed |
| `--repeat-penalty` | 1.1 | Repeat penalty |
| `--system` | "" | System prompt text |
| `--system-file` | "" | Load system prompt from file |
| `--session` | "" | Resume a session by ID |
| `--no-persist` | false | Don't save to SQLite |
| `--list-sessions` | false | List saved sessions and exit |
| `--interactive` | false | Stay interactive after initial prompt |

### `guff serve`

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | 127.0.0.1 | Bind address |
| `--port` | 8080 | Listen port |

### `guff pull`

| Flag | Default | Description |
|------|---------|-------------|
| `--quantization` | q4_K_M | GGUF quantization variant |

## System Prompt Resolution

Priority chain (first match wins):

1. `--system` CLI flag (inline text)
2. `--system-file` CLI flag (load from file)
3. Per-model config in `system_prompt.models` (supports `@filepath` syntax)
4. Config `system_prompt.default` (inline) or `system_prompt.file` (file path)
5. `~/.config/guff/system-prompt.txt` (default file)

If none match, no system prompt is used.

## GPU Configuration

GPU acceleration is automatic:

| GPU | Detection | Library |
|-----|-----------|---------|
| NVIDIA | `nvidia-smi` auto-detect | CUDA-enabled llama.cpp |
| Apple Silicon | macOS platform detect | Metal-enabled llama.cpp |
| Vulkan | Driver availability | Vulkan-enabled llama.cpp |
| None | Fallback | CPU-only llama.cpp |

The `model.num_gpu_layers` setting (default 35) controls how many transformer layers are offloaded to GPU VRAM. Set to 0 for CPU-only inference, or to a high number (e.g., 99) to offload all layers.
