#!/bin/bash
set -e

# run.sh - Example usage of guff chat with Granite model
#
# This script demonstrates the chat functionality with persistent session management.
# The granite-3b-code-instruct model is optimized for code generation, not conversation.

# Configuration
MODEL="granite-3b"
MODEL_FILE="models/granite-3b/model.Q4_K_M.gguf"
GUFF_BIN="./guff"

# Display help
show_help() {
    cat <<EOF
Usage: $0 [OPTIONS]

Demo script for guff LLM runtime with Granite model.

Options:
  -h, --help          Show this help message
  --build             Build guff binary before running
  --no-persist        Run chat without session persistence
  --test              Run quick test (no interactive chat)
  --session ID        Use specific session ID
  --prompt TEXT       Run single prompt and exit
  --temperature NUM   Set temperature (default: 0.2)
  --max-tokens NUM    Set max tokens (default: 256)

Examples:
  $0                     # Interactive chat with new session
  $0 --no-persist        # Chat without saving history
  $0 --session abc123    # Resume specific session
  $0 --prompt "Hello"    # Single prompt test
  $0 --test              # Quick functionality test
  $0 --build             # Build and run

Environment variables:
  GUFF_HUGGINGFACE_TOKEN  Hugging Face token for model downloads

Session data: ~/.local/share/guff/chat.db
Model path: $MODEL_FILE
EOF
    exit 0
}

# Parse command line arguments
BUILD=false
NO_PERSIST=false
TEST=false
SESSION=""
PROMPT=""
TEMPERATURE=0.2
MAX_TOKENS=256

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            ;;
        --build)
            BUILD=true
            shift
            ;;
        --no-persist)
            NO_PERSIST=true
            shift
            ;;
        --test)
            TEST=true
            shift
            ;;
        --session)
            SESSION="$2"
            shift 2
            ;;
        --prompt)
            PROMPT="$2"
            shift 2
            ;;
        --temperature)
            TEMPERATURE="$2"
            shift 2
            ;;
        --max-tokens)
            MAX_TOKENS="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            show_help
            ;;
    esac
done

# Check if guff binary exists or build requested
if [ ! -f "$GUFF_BIN" ] || [ "$BUILD" = true ]; then
    echo "Building guff..."
    make build
fi

# Check if model exists
if [ ! -f "$MODEL_FILE" ]; then
    echo "Error: Model file not found: $MODEL_FILE"
    echo "Please download the model first using:"
    echo "  ./guff pull $MODEL"
    exit 1
fi

# Set Hugging Face token if available
if [ -z "$GUFF_HUGGINGFACE_TOKEN" ]; then
    echo "Note: GUFF_HUGGINGFACE_TOKEN not set (only needed for model downloads)"
fi

echo "=== guff Chat Demo ==="
echo "Model: $MODEL"
echo "File: $MODEL_FILE"
echo "Temperature: $TEMPERATURE"
echo "Max tokens: $MAX_TOKENS"
[ -n "$SESSION" ] && echo "Session: $SESSION"
[ "$NO_PERSIST" = true ] && echo "Persistence: disabled"
echo ""

# Quick test mode
if [ "$TEST" = true ]; then
    echo "--- Quick Test ---"
    echo "Testing model loading and basic generation..."
    timeout 10 "$GUFF_BIN" run --model "$MODEL" --max-tokens 10 "Hello" 2>/dev/null && echo "✓ Basic test passed" || echo "✗ Basic test failed"
    echo "Test complete."
    exit 0
fi

# Single prompt mode
if [ -n "$PROMPT" ]; then
    echo "--- Single Prompt ---"
    echo "Prompt: $PROMPT"
    echo ""
    "$GUFF_BIN" run --model "$MODEL" --max-tokens "$MAX_TOKENS" --temperature "$TEMPERATURE" "$PROMPT"
    exit 0
fi

# Interactive chat
echo "--- Interactive Chat ---"
echo "Features:"
echo "1. Persistent session management (SQLite storage)"
echo "2. Context window handling with truncation strategies"
echo "3. Token counting using yzma integration"
echo "4. Model switching support"
echo ""
echo "Type your prompts, then Ctrl+D to exit."
echo "Commands: /exit, /clear"
echo ""

# Build command
CMD="$GUFF_BIN chat --model $MODEL --max-tokens $MAX_TOKENS --temperature $TEMPERATURE"

# Add session if specified
if [ -n "$SESSION" ]; then
    CMD="$CMD --session $SESSION"
fi

# Add no-persist if requested
if [ "$NO_PERSIST" = true ]; then
    CMD="$CMD --no-persist"
fi

# Add system prompt for coding assistance
CMD="$CMD --system \"You are a helpful coding assistant.\""

echo "Running: $CMD"
echo ""
eval "$CMD"

# Show session info after exit
if [ "$NO_PERSIST" = false ] && [ -z "$SESSION" ]; then
    echo ""
    echo "Note: Session was automatically created and saved."
    echo "To resume this session later, use: --session <session-id>"
    echo "Session data: ~/.local/share/guff/chat.db"
fi