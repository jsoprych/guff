package model

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
)

// HuggingFaceDownloader downloads models from Hugging Face
type HuggingFaceDownloader struct {
	client      *http.Client
	accessToken string
}

// NewHuggingFaceDownloader creates a new downloader
func NewHuggingFaceDownloader(token string) *HuggingFaceDownloader {
	jar, _ := cookiejar.New(nil)
	return &HuggingFaceDownloader{
		client: &http.Client{
			Jar: jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Preserve Authorization header across redirects
				if token != "" && req.Header.Get("Authorization") == "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				return nil
			},
		},
		accessToken: token,
	}
}

// DownloadFile downloads a file from URL to destination with progress reporting
func (d *HuggingFaceDownloader) DownloadFile(ctx context.Context, url, dest string, progress func(downloaded, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "guff/0.1.0")
	req.Header.Set("Accept", "*/*")
	if d.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.accessToken)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Try to read error body
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d: %s - %s", resp.StatusCode, resp.Status, string(body))
	}

	// Create destination directory
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Create temporary file
	tmpDest := dest + ".tmp"
	file, err := os.Create(tmpDest)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	// Copy with progress
	var downloaded int64
	total := resp.ContentLength

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := file.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("write file: %w", writeErr)
			}
			downloaded += int64(n)
			if progress != nil && total > 0 {
				progress(downloaded, total)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("read response: %w", err)
		}
	}

	// Rename temporary file to final destination
	if err := os.Rename(tmpDest, dest); err != nil {
		return fmt.Errorf("rename file: %w", err)
	}

	return nil
}

// GetHuggingFaceFileURLs returns possible direct download URLs for a file in a Hugging Face repo
func (d *HuggingFaceDownloader) GetHuggingFaceFileURLs(repo, filePath string) []string {
	// Only use resolve URL - it handles LFS redirects properly
	return []string{
		fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, filePath),
	}
}

// parseLFSPointer parses a Git LFS pointer file
func parseLFSPointer(content []byte) (oid string, size int64, ok bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(content)))

	// Check first line
	if !scanner.Scan() {
		return "", 0, false
	}
	firstLine := scanner.Text()
	if !strings.HasPrefix(firstLine, "version https://git-lfs.github.com/spec/v1") {
		return "", 0, false
	}

	// Parse oid and size
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "oid sha256:") {
			oid = strings.TrimPrefix(line, "oid sha256:")
		} else if strings.HasPrefix(line, "size ") {
			fmt.Sscanf(line, "size %d", &size)
		}
	}

	return oid, size, oid != "" && size > 0
}

// downloadFromLFS downloads a file from Git LFS using the object ID
func (d *HuggingFaceDownloader) downloadFromLFS(ctx context.Context, repo, oid string, size int64, dest string, progress func(downloaded, total int64)) error {
	// LFS endpoint URL
	// Note: Hugging Face uses custom LFS endpoints, not standard Git LFS
	// The resolve URL should handle this automatically via redirects
	// If we get here, something went wrong with redirects
	return fmt.Errorf("LFS download not implemented, oid: %s, size: %d", oid, size)
}
