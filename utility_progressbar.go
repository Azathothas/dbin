//go:build !spinner
// +build !spinner

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/goccy/go-json"
	"github.com/schollz/progressbar/v3"
)

func spawnProgressBar(contentLength int64, useSpinnerType bool) *progressbar.ProgressBar {
	if useSpinnerType {
		return progressbar.NewOptions(int(contentLength),
			progressbar.OptionClearOnFinish(),
			progressbar.OptionFullWidth(),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSpinnerType(39),
		)
	}

	return progressbar.NewOptions(int(contentLength),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionFullWidth(),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}

func fetchBinaryFromURLToDest(ctx context.Context, url, checksum, destination string) (string, error) {
	// Create a new HTTP request with cache-control headers
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request for %s: %v", url, err)
	}

	// Add headers to disable caching
	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Expires", "0")

	// Perform the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Ensure the parent directory exists
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return "", fmt.Errorf("failed to create parent directories for %s: %v", destination, err)
	}

	// Create temp file
	tempFile := destination + ".tmp"
	out, err := os.Create(tempFile)
	if err != nil {
		return "", err
	}
	defer out.Close()

	// Create progress bar
	contentLength := resp.ContentLength
	bar := spawnProgressBar(contentLength, false)

	defer bar.Close()

	var downloaded int64
	buf := make([]byte, 4096)

	hash := sha256.New()

downloadLoop:
	for {
		select {
		case <-ctx.Done():
			_ = os.Remove(tempFile)
			return "", ctx.Err()
		default:
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if _, err := out.Write(buf[:n]); err != nil {
					_ = os.Remove(tempFile)
					return "", err
				}

				// Write to hash for checksum calculation
				if _, err := hash.Write(buf[:n]); err != nil {
					_ = os.Remove(tempFile)
					return "", err
				}

				// Update progress bar
				downloaded += int64(n)
				bar.Add(n)
			}
			if err == io.EOF {
				break downloadLoop
			}
			if err != nil {
				_ = os.Remove(tempFile)
				return "", err
			}
		}
	}

	// Final checksum verification
	if checksum != "" {
		calculatedChecksum := hex.EncodeToString(hash.Sum(nil))
		if calculatedChecksum != checksum {
			_ = os.Remove(tempFile)
			return "", fmt.Errorf("checksum verification failed: expected %s, got %s", checksum, calculatedChecksum)
		}
	} else {
		fmt.Println("Warning: No checksum exists for this binary in the metadata files, skipping verification.")
	}

	// Make a few corrections in case the downloaded binary is a nix object
	if err := removeNixGarbageFoundInTheRepos(tempFile); err != nil {
		_ = os.Remove(tempFile)
		return "", err
	}

	if err := os.Rename(tempFile, destination); err != nil {
		_ = os.Remove(tempFile)
		return "", err
	}

	if err := os.Chmod(destination, 0755); err != nil {
		_ = os.Remove(destination)
		return "", fmt.Errorf("failed to set executable bit for %s: %v", destination, err)
	}

	return destination, nil
}

func fetchJSON(url string, v interface{}) error {
	// Create a new HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("error creating request for %s: %v", url, err)
	}

	// Add headers to disable caching
	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Expires", "0")

	// Perform the request using http.DefaultClient
	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error fetching from %s: %v", url, err)
	}
	defer response.Body.Close()

	// Set up progress bar based on Content-Length if available
	contentLength := response.ContentLength
	bar := spawnProgressBar(contentLength, true)

	defer bar.Close()

	// Read the response body with progress tracking
	body := &bytes.Buffer{}
	reader := io.TeeReader(response.Body, bar)

	if _, err := io.Copy(body, reader); err != nil {
		return fmt.Errorf("error reading from %s: %v", url, err)
	}

	// Unmarshal the JSON response
	if err := json.Unmarshal(body.Bytes(), v); err != nil {
		return fmt.Errorf("error decoding from %s: %v", url, err)
	}

	return nil
}
