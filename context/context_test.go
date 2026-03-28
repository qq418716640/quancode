package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewBuilder_Defaults(t *testing.T) {
	b := NewBuilder(nil, nil)
	if b.maxTotalBytes != DefaultMaxTotalBytes {
		t.Errorf("maxTotalBytes = %d, want %d", b.maxTotalBytes, DefaultMaxTotalBytes)
	}
	if b.maxFileBytes != DefaultMaxFileBytes {
		t.Errorf("maxFileBytes = %d, want %d", b.maxFileBytes, DefaultMaxFileBytes)
	}
	if len(b.autoFiles) != len(defaultAutoFiles) {
		t.Errorf("autoFiles len = %d, want %d", len(b.autoFiles), len(defaultAutoFiles))
	}
}

func TestNewBuilder_MergeSpec(t *testing.T) {
	global := &ContextSpec{MaxTotalBytes: 64 * 1024}
	agent := &ContextSpec{MaxTotalBytes: 16 * 1024}

	b := NewBuilder(global, agent)
	if b.maxTotalBytes != 16*1024 {
		t.Errorf("agent override not applied: got %d", b.maxTotalBytes)
	}
}

func TestNewBuilder_GlobalOnly(t *testing.T) {
	global := &ContextSpec{MaxTotalBytes: 64 * 1024, MaxFileBytes: 8 * 1024}
	b := NewBuilder(global, nil)
	if b.maxTotalBytes != 64*1024 {
		t.Errorf("global not applied: got %d", b.maxTotalBytes)
	}
	if b.maxFileBytes != 8*1024 {
		t.Errorf("global maxFileBytes not applied: got %d", b.maxFileBytes)
	}
}

func TestBuild_AutoFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("build: go test"), 0644)

	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, nil, "", 0)

	if len(bundle.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(bundle.Files))
	}
	if bundle.Files[0].Path != "CLAUDE.md" {
		t.Errorf("expected CLAUDE.md, got %s", bundle.Files[0].Path)
	}
	if bundle.Files[0].Content != "build: go test" {
		t.Errorf("unexpected content: %q", bundle.Files[0].Content)
	}
}

func TestBuild_ExtraFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)

	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, []string{"main.go"}, "", 0)

	if len(bundle.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(bundle.Files))
	}
	if bundle.Files[0].Path != "main.go" {
		t.Errorf("expected main.go, got %s", bundle.Files[0].Path)
	}
}

func TestBuild_ExtraFileNotFound(t *testing.T) {
	dir := t.TempDir()
	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, []string{"nonexistent.go"}, "", 0)

	if len(bundle.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(bundle.Files))
	}
	if len(bundle.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(bundle.Warnings))
	}
}

func TestBuild_Dedupe(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("hello"), 0644)

	b := NewBuilder(nil, nil)
	// CLAUDE.md appears in both extra and auto
	bundle := b.Build(dir, []string{"CLAUDE.md"}, "", 0)

	if len(bundle.Files) != 1 {
		t.Errorf("expected 1 file (deduped), got %d", len(bundle.Files))
	}
}

func TestBuild_FileTruncation(t *testing.T) {
	dir := t.TempDir()
	// Create a file larger than default maxFileBytes (16KB)
	bigContent := strings.Repeat("x", 20*1024)
	os.WriteFile(filepath.Join(dir, "big.go"), []byte(bigContent), 0644)

	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, []string{"big.go"}, "", 0)

	if len(bundle.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(bundle.Files))
	}
	if !bundle.Files[0].Truncated {
		t.Error("expected file to be truncated")
	}
	if len(bundle.Files[0].Content) >= 20*1024 {
		t.Error("content should be shorter than original")
	}
}

func TestBuild_TotalBudget(t *testing.T) {
	dir := t.TempDir()
	// Create two files, each 10KB, with a 16KB total budget
	content := strings.Repeat("a", 10*1024)
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte(content), 0644)

	b := NewBuilder(nil, nil)
	// 16KB total, 80% for files = 12.8KB, only one 10KB file fits
	bundle := b.Build(dir, []string{"a.go", "b.go"}, "", 16*1024)

	if len(bundle.Files) != 1 {
		t.Fatalf("expected 1 file (budget limit), got %d", len(bundle.Files))
	}
	if len(bundle.Warnings) != 1 {
		t.Errorf("expected 1 budget warning, got %d", len(bundle.Warnings))
	}
}

func TestBuild_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	// Write a file with null bytes
	os.WriteFile(filepath.Join(dir, "binary.dat"), []byte("hello\x00world"), 0644)

	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, []string{"binary.dat"}, "", 0)

	if len(bundle.Files) != 0 {
		t.Error("binary file should be skipped")
	}
	if len(bundle.Warnings) != 1 {
		t.Errorf("expected 1 warning for binary file, got %d", len(bundle.Warnings))
	}
}

func TestBuild_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, []string{"../../../etc/passwd"}, "", 0)

	if len(bundle.Files) != 0 {
		t.Error("path traversal should be rejected")
	}
	if len(bundle.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(bundle.Warnings))
	}
}

func TestBuild_NoContext(t *testing.T) {
	dir := t.TempDir()
	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, nil, "", 0)

	if len(bundle.Files) != 0 {
		t.Errorf("expected 0 files in empty dir, got %d", len(bundle.Files))
	}
}

func TestBuild_MaxSizeOverride(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 5*1024)
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(content), 0644)

	b := NewBuilder(nil, nil)
	// Override to tiny budget: 2KB total, 1.6KB for files — file won't fit
	bundle := b.Build(dir, []string{"a.go"}, "", 2*1024)

	if len(bundle.Files) != 0 {
		t.Error("expected 0 files with tiny budget")
	}
}

func TestBuild_GitDiff(t *testing.T) {
	dir := t.TempDir()

	// Init git repo and create a diff
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Skipf("git setup failed: %v", err)
		}
	}

	// Create and commit a file, then modify it
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	cmd := exec.Command("git", "add", "main.go")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = dir
	cmd.Run()

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)

	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, nil, "working", 0)

	if bundle.GitDiff == "" {
		t.Error("expected non-empty git diff")
	}
	if !strings.Contains(bundle.GitDiff, "func main()") {
		t.Errorf("diff should contain added code, got: %s", bundle.GitDiff)
	}
}

func TestBuild_GitDiffStaged(t *testing.T) {
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		cmd.Run()
	}

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	exec.Command("git", "-C", dir, "add", "main.go").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc foo() {}\n"), 0644)
	exec.Command("git", "-C", dir, "add", "main.go").Run()

	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, nil, "staged", 0)

	if bundle.GitDiff == "" {
		t.Error("expected non-empty staged diff")
	}
}

func TestBuild_GitDiffNonGit(t *testing.T) {
	dir := t.TempDir()
	b := NewBuilder(nil, nil)
	bundle := b.Build(dir, nil, "working", 0)

	// Should silently produce empty diff, no error
	if bundle.GitDiff != "" {
		t.Error("expected empty diff in non-git dir")
	}
}

func TestFormat_Empty(t *testing.T) {
	result := Format(nil)
	if result != "" {
		t.Error("nil bundle should produce empty string")
	}

	result = Format(&ContextBundle{})
	if result != "" {
		t.Error("empty bundle should produce empty string")
	}
}

func TestFormat_WithFiles(t *testing.T) {
	bundle := &ContextBundle{
		Files: []FileEntry{
			{Path: "CLAUDE.md", Content: "build: go test"},
		},
	}

	result := Format(bundle)
	if !strings.Contains(result, "=== PROJECT CONTEXT ===") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "### CLAUDE.md") {
		t.Error("missing file header")
	}
	if !strings.Contains(result, "build: go test") {
		t.Error("missing file content")
	}
	if !strings.Contains(result, "=== END CONTEXT ===") {
		t.Error("missing footer")
	}
}

func TestFormat_WithDiff(t *testing.T) {
	bundle := &ContextBundle{
		GitDiff: "+func foo() {}",
	}

	result := Format(bundle)
	if !strings.Contains(result, "Uncommitted Changes") {
		t.Error("missing diff section")
	}
	if !strings.Contains(result, "+func foo() {}") {
		t.Error("missing diff content")
	}
}

func TestDedupeFiles(t *testing.T) {
	files := []FileEntry{
		{Path: "a.go", Content: "first"},
		{Path: "b.go", Content: "second"},
		{Path: "a.go", Content: "duplicate"},
	}

	result := dedupeFiles(files)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
	// First occurrence should win
	if result[0].Content != "first" {
		t.Errorf("first occurrence should be kept, got %q", result[0].Content)
	}
}
