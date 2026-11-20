package builder

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CreateTarStream walks a directory on disk and packages its files into an in-memory
// tar archive reader, ready to be streamed directly to Docker's POST /build endpoint.
//
// Analogy: Instead of saving a heavy .tar file to your hard drive (like filling a bucket),
// this function pipes the file bytes directly through memory (like a garden hose).
func CreateTarStream(srcDir string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path inside the tar archive (e.g., "Dockerfile", "main.go")
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("failed to calculate relative path: %w", err)
		}

		// Skip the root folder itself
		if relPath == "." {
			return nil
		}

		// Convert Windows backslashes (\) to standard tar forward slashes (/)
		tarName := filepath.ToSlash(relPath)

		// Create the tar header based on file metadata
		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return fmt.Errorf("failed to create tar header for %s: %w", relPath, err)
		}
		header.Name = tarName

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header for %s: %w", relPath, err)
		}

		// If it's a regular file (not directory), write its byte contents
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %w", path, err)
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return fmt.Errorf("failed to copy file contents for %s: %w", relPath, err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk source directory: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}

	return &buf, nil
}
