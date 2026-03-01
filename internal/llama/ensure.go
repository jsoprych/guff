package llama

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hybridgroup/yzma/pkg/download"
)

// EnsureLibraries ensures llama.cpp libraries are available in libDir.
// If libraries are missing, it downloads the latest precompiled binaries.
// It sets YZMA_LIB environment variable to libDir.
func EnsureLibraries(libDir string) error {
	// Set environment variable for yzma
	if err := os.Setenv("YZMA_LIB", libDir); err != nil {
		return fmt.Errorf("set YZMA_LIB: %w", err)
	}

	// Create lib directory
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("create lib directory: %w", err)
	}

	// Check if library already exists
	libName := download.LibraryName(runtime.GOOS)
	libPath := filepath.Join(libDir, libName)
	if _, err := os.Stat(libPath); err == nil {
		// Library exists, nothing to do
		return nil
	}

	// Determine processor type
	processor := "cpu"
	if cudaInstalled, cudaVersion := download.HasCUDA(); cudaInstalled {
		fmt.Printf("CUDA detected (version %s), using CUDA build\n", cudaVersion)
		processor = "cuda"
	} else if runtime.GOOS == "darwin" {
		// macOS: prefer Metal if available
		processor = "metal"
	}

	// Get latest llama.cpp version
	version, err := download.LlamaLatestVersion()
	if err != nil {
		return fmt.Errorf("get latest llama.cpp version: %w", err)
	}

	fmt.Printf("Downloading llama.cpp %s (%s/%s/%s) to %s\n",
		version, runtime.GOARCH, runtime.GOOS, processor, libDir)

	// Download libraries
	if err := download.Get(runtime.GOARCH, runtime.GOOS, processor, version, libDir); err != nil {
		// Fallback to CPU if CUDA/Metal fails
		if processor != "cpu" {
			fmt.Printf("Failed to download %s build, falling back to CPU: %v\n", processor, err)
			processor = "cpu"
			if err := download.Get(runtime.GOARCH, runtime.GOOS, processor, version, libDir); err != nil {
				return fmt.Errorf("download llama.cpp (CPU fallback): %w", err)
			}
		} else {
			return fmt.Errorf("download llama.cpp: %w", err)
		}
	}

	fmt.Println("llama.cpp libraries installed successfully")
	return nil
}
