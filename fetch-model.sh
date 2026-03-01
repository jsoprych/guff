#!/bin/bash
set -e

# fetch-model.sh - Download GGUF models from Hugging Face
# Usage: ./fetch-model.sh [model] [quantization]
# Example: ./fetch-model.sh granite-3b Q4_K_M

MODEL="${1:-granite-3b}"
QUANT="${2:-Q4_K_M}"

# Hugging Face token - prefer environment variable, fallback to config
TOKEN="${GUFF_HUGGINGFACE_TOKEN:-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}"

if [ -z "$TOKEN" ]; then
    echo "ERROR: No Hugging Face token found."
    echo "Set GUFF_HUGGINGFACE_TOKEN environment variable or edit this script."
    exit 1
fi

# Map model names to Hugging Face repos and file patterns
case "$MODEL" in
    granite-3b)
        REPO="ibm-granite/granite-3b-code-instruct-2k-GGUF"
        FILE="granite-3b-code-instruct.$QUANT.gguf"
        ;;
    tinyllama)
        REPO="TheBloke/TinyLlama-1.1B-GGUF"
        FILE="tinyllama-1.1b-$QUANT.gguf"
        ;;
    *)
        echo "ERROR: Unknown model '$MODEL'"
        echo "Supported models: granite-3b, tinyllama"
        exit 1
        ;;
esac

# Destination directory
MODELS_DIR="./models"
MODEL_DIR="$MODELS_DIR/$MODEL"
DEST="$MODEL_DIR/model.$QUANT.gguf"
TMP="$DEST.part"

# Create directory
mkdir -p "$MODEL_DIR"

# Hugging Face resolve URL (handles LFS redirects)
URL="https://huggingface.co/$REPO/resolve/main/$FILE"

echo "Downloading $MODEL ($QUANT) from Hugging Face..."
echo "Repo: $REPO"
echo "File: $FILE"
echo "Destination: $DEST"
echo ""

# Check for existing partial download
if [ -f "$TMP" ]; then
    echo "Resuming partial download ($(du -h "$TMP" | cut -f1))..."
    RESUME_FLAG="-C -"
else
    RESUME_FLAG=""
fi

# Download with curl
curl -L \
    -H "Authorization: Bearer $TOKEN" \
    -H "User-Agent: guff/0.1.0" \
    $RESUME_FLAG \
    --progress-bar \
    -o "$TMP" \
    "$URL"

# Check exit code
if [ $? -eq 0 ]; then
    # Rename temporary file
    mv "$TMP" "$DEST"
    echo ""
    echo "✅ Successfully downloaded to $DEST"
    echo "File size: $(du -h "$DEST" | cut -f1)"
    
    # Verify GGUF magic
    if head -c 4 "$DEST" | xxd -p | grep -q ^47475546; then
        echo "✅ GGUF header verified"
    else
        echo "⚠️  Warning: File does not appear to be a valid GGUF file"
    fi
else
    echo ""
    echo "❌ Download failed"
    if [ -f "$TMP" ]; then
        echo "Partial download saved as $TMP"
    fi
    exit 1
fi