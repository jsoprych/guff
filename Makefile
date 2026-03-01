# Makefile for guff - local LLM runtime

.PHONY: all build build-all install test bench clean fmt lint deps completions release

BINARY_NAME=guff
VERSION=$(shell git describe --tags 2>/dev/null || echo "0.1.0")

all: build

# Build for current platform
build:
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	go build -o $(BINARY_NAME) .

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o $(BINARY_NAME)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o $(BINARY_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build -o $(BINARY_NAME)-windows-amd64.exe .

# Install to GOPATH/bin
install:
	go install .

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	go test -bench=. ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME) $(BINARY_NAME)-* $(BINARY_NAME)-*.exe
	rm -rf dist/

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint with golangci-lint (must be installed)
lint:
	@echo "Linting..."
	golangci-lint run

# Install dependencies (llama.cpp)
deps:
	@echo "Installing llama.cpp system dependencies..."
	@if command -v apt-get >/dev/null 2>&1; then \
		sudo apt-get update && sudo apt-get install -y build-essential cmake git; \
	elif command -v yum >/dev/null 2>&1; then \
		sudo yum groupinstall -y "Development Tools" && sudo yum install -y cmake git; \
	elif command -v brew >/dev/null 2>&1; then \
		brew install cmake git; \
	else \
		echo "Package manager not supported. Install build-essential/cmake/git manually."; \
	fi
	@echo "Setting up llama.cpp..."
	@if [ ! -d "third_party/llama.cpp" ]; then \
		echo "Cloning llama.cpp to third_party/llama.cpp..."; \
		git clone https://github.com/ggerganov/llama.cpp.git third_party/llama.cpp; \
	else \
		echo "llama.cpp already exists in third_party/llama.cpp"; \
	fi
	@echo "Building llama.cpp..."
	cd third_party/llama.cpp && mkdir -p build && cd build && \
		cmake .. -DLLAMA_CUBLAS=OFF -DLLAMA_METAL=OFF -DLLAMA_BLAS=OFF && \
		make -j$(nproc)
	@echo "Installing llama.cpp headers..."
	@if [ -f "third_party/llama.cpp/include/llama.h" ]; then \
		sudo cp -r third_party/llama.cpp/include/llama.h /usr/local/include/ 2>/dev/null || true; \
		sudo cp -r third_party/llama.cpp/common/common.h /usr/local/include/ 2>/dev/null || true; \
		sudo cp -r third_party/llama.cpp/ggml/include/ggml.h /usr/local/include/ 2>/dev/null || true; \
		sudo ldconfig 2>/dev/null || true; \
		echo "✅ llama.cpp headers installed to /usr/local/include/"; \
	else \
		echo "⚠️  llama.cpp headers not found. Build may have failed."; \
		exit 1; \
	fi

# Download model (uses fetch-model.sh)
model:
	@echo "Downloading default model (granite-3b Q4_K_M)..."
	./fetch-model.sh granite-3b Q4_K_M

# Generate shell completions
completions:
	@echo "Generating shell completions..."
	./$(BINARY_NAME) completion bash > $(BINARY_NAME).bash
	./$(BINARY_NAME) completion zsh > $(BINARY_NAME).zsh
	./$(BINARY_NAME) completion fish > $(BINARY_NAME).fish

# Create release archives
release: build-all
	@echo "Creating release archives..."
	mkdir -p dist
	tar -czf dist/$(BINARY_NAME)-linux-amd64-$(VERSION).tar.gz $(BINARY_NAME)-linux-amd64
	tar -czf dist/$(BINARY_NAME)-linux-arm64-$(VERSION).tar.gz $(BINARY_NAME)-linux-arm64
	tar -czf dist/$(BINARY_NAME)-darwin-amd64-$(VERSION).tar.gz $(BINARY_NAME)-darwin-amd64
	tar -czf dist/$(BINARY_NAME)-darwin-arm64-$(VERSION).tar.gz $(BINARY_NAME)-darwin-arm64
	zip -j dist/$(BINARY_NAME)-windows-amd64-$(VERSION).zip $(BINARY_NAME)-windows-amd64.exe
	@echo "Release archives created in dist/"