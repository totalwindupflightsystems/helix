package secrets

import (
	"os"
	"path/filepath"
)

// writeFile writes content to a temp file at path with 0600 permissions.
// Used by tests that need on-disk fixtures for ScanFile / ScanPath.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// osMkdirAll is an alias for os.MkdirAll exposed for test helpers.
func osMkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}
