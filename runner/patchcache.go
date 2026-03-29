package runner

import (
	"fmt"
	"os"
	"path/filepath"
)

// PatchCacheDir returns the directory for cached patches.
func PatchCacheDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "quancode", "patches")
	}
	return ""
}

// CachePatch saves a patch to the cache directory keyed by delegation ID.
// Returns the file path on success.
func CachePatch(delegationID, patch string) (string, error) {
	dir := PatchCacheDir()
	if dir == "" {
		return "", fmt.Errorf("cannot determine patch cache directory")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, delegationID+".diff")
	if err := os.WriteFile(path, []byte(patch), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// LoadCachedPatch reads a cached patch by delegation ID.
func LoadCachedPatch(delegationID string) (string, error) {
	dir := PatchCacheDir()
	if dir == "" {
		return "", fmt.Errorf("cannot determine patch cache directory")
	}
	path := filepath.Join(dir, delegationID+".diff")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("patch not found for delegation %s: %w", delegationID, err)
	}
	return string(data), nil
}
