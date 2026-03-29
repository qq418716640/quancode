package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCachePatchAndLoad(t *testing.T) {
	// Override HOME to use temp dir
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", dir)

	patch := "diff --git a/f.go b/f.go\n--- a/f.go\n+++ b/f.go\n@@ -1 +1 @@\n-old\n+new\n"
	id := "del_test123"

	path, err := CachePatch(id, patch)
	if err != nil {
		t.Fatalf("CachePatch: %v", err)
	}

	expected := filepath.Join(dir, ".config", "quancode", "patches", id+".diff")
	if path != expected {
		t.Fatalf("expected path %s, got %s", expected, path)
	}

	loaded, err := LoadCachedPatch(id)
	if err != nil {
		t.Fatalf("LoadCachedPatch: %v", err)
	}
	if loaded != patch {
		t.Fatalf("expected patch content preserved, got %q", loaded)
	}
}

func TestLoadCachedPatchNotFound(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", dir)

	_, err := LoadCachedPatch("del_nonexistent")
	if err == nil {
		t.Fatal("expected error for missing patch")
	}
}
