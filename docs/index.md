# guff Documentation

**guff Community Edition** -- a local LLM runtime with multi-provider routing, MCP tool integration, and dual API compatibility.

Created by **John Soprych**, Chief Scientist at [Elko.AI](https://elko.ai).

## Overview

guff runs GGUF models locally via llama.cpp (through yzma FFI bindings), routes requests to remote APIs (OpenAI, Anthropic, DeepSeek), and exposes both Ollama-compatible and OpenAI-compatible HTTP endpoints with an embedded chat UI. GPU acceleration is automatic (CUDA, Metal, Vulkan). Supports grammar-constrained generation, LoRA adapters, embeddings, and a 12-stage sampler chain.

## Quick Start

```bash
# Build
make build

# Download a model
export GUFF_HUGGINGFACE_TOKEN="hf_xxxx"
guff pull granite-3b

# Single prompt
guff run "Explain quicksort in 3 sentences"

# Interactive chat
guff chat

# API server
guff serve
```

## Documentation Index

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | System design, package structure, data flow |
| [Configuration](configuration.md) | Full config reference (`config.yaml`, env vars, CLI flags) |
| [Providers](providers.md) | Provider routing, setup for OpenAI/Anthropic/DeepSeek/custom |
| [MCP & Tools](mcp-tools.md) | MCP server integration, tool registry, function calling |
| [Context Management](context-management.md) | Context strategies, token budgets, status display |
| [Prompt Builder](prompt-builder.md) | Multi-part system prompts, auto-discovery, per-model overrides |
| [API Reference](api-reference.md) | OpenAI + Ollama endpoint specifications |
| [Sampling](sampling.md) | Sampler chain, parameters, tuning guide |
| [Known Issues](known-issues.md) | Bugs, limitations, workarounds |
| [Changelog](changelog.md) | Version history |

## CLI Commands

| Command | Description |
|---------|-------------|
| `guff pull <model>` | Download a model from HuggingFace |
| `guff run <prompt>` | One-shot text generation |
| `guff chat [prompt]` | Interactive chat with session persistence |
| `guff serve` | Start the HTTP API server |

## Requirements

- **Go 1.25+** for building from source
- **HuggingFace token** for model downloads (`GUFF_HUGGINGFACE_TOKEN`)
- GPU drivers (CUDA/Vulkan) for GPU acceleration (optional -- CPU fallback is automatic)
