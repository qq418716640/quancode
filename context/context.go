package context

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

const (
	DefaultMaxTotalBytes = 32 * 1024 // 32KB
	DefaultMaxFileBytes  = 16 * 1024 // 16KB
	DefaultRecentCommits = 0         // off by default
)

// defaultAutoFiles are always injected when they exist.
// Only files that sub-agents can't discover on their own.
var defaultAutoFiles = []string{
	"CLAUDE.md",
	"AGENTS.md",
}

// ContextSpec defines what context to collect for delegation.
type ContextSpec struct {
	AutoFiles     []string `yaml:"auto_files"`
	MaxTotalBytes int      `yaml:"max_total_bytes"`
	MaxFileBytes  int      `yaml:"max_file_bytes"`
}

// FileEntry represents a collected file with its content.
type FileEntry struct {
	Path      string
	Content   string
	Size      int
	Truncated bool
}

// ContextBundle is the assembled context package.
type ContextBundle struct {
	WorkDir   string
	Files     []FileEntry
	GitDiff   string
	Warnings  []string
	UsedBytes int
}

// Builder constructs context bundles.
type Builder struct {
	maxTotalBytes int
	maxFileBytes  int
	autoFiles     []string
}

// NewBuilder creates a Builder, merging global and agent-specific specs.
func NewBuilder(global, agentSpec *ContextSpec) *Builder {
	b := &Builder{
		maxTotalBytes: DefaultMaxTotalBytes,
		maxFileBytes:  DefaultMaxFileBytes,
		autoFiles:     defaultAutoFiles,
	}

	// Apply global defaults first
	if global != nil {
		if global.MaxTotalBytes > 0 {
			b.maxTotalBytes = global.MaxTotalBytes
		}
		if global.MaxFileBytes > 0 {
			b.maxFileBytes = global.MaxFileBytes
		}
		if len(global.AutoFiles) > 0 {
			b.autoFiles = global.AutoFiles
		}
	}

	// Agent-specific overrides
	if agentSpec != nil {
		if agentSpec.MaxTotalBytes > 0 {
			b.maxTotalBytes = agentSpec.MaxTotalBytes
		}
		if agentSpec.MaxFileBytes > 0 {
			b.maxFileBytes = agentSpec.MaxFileBytes
		}
		if len(agentSpec.AutoFiles) > 0 {
			b.autoFiles = agentSpec.AutoFiles
		}
	}

	return b
}

// Build collects context from the working directory.
// extraFiles are user-specified paths via --context-files.
// diffMode is "staged", "working", or "" (off).
// maxSizeOverride overrides MaxTotalBytes when > 0.
func (b *Builder) Build(workDir string, extraFiles []string, diffMode string, maxSizeOverride int) *ContextBundle {
	bundle := &ContextBundle{WorkDir: workDir}

	maxTotal := b.maxTotalBytes
	if maxSizeOverride > 0 {
		maxTotal = maxSizeOverride
	}

	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("resolve workdir: %v", err))
		return bundle
	}

	// Reserve 20% for diff
	diffBudget := maxTotal / 5
	fileBudget := maxTotal - diffBudget

	// Collect files: extra files first (higher priority), then auto files
	var files []FileEntry
	for _, f := range extraFiles {
		entry, readErr := b.readFile(absWorkDir, f)
		if readErr != nil {
			bundle.Warnings = append(bundle.Warnings, readErr.Error())
			continue
		}
		files = append(files, entry)
	}
	for _, f := range b.autoFiles {
		entry, readErr := b.readFile(absWorkDir, f)
		if readErr != nil {
			continue
		}
		files = append(files, entry)
	}

	// Dedupe
	files = dedupeFiles(files)

	// Apply budget
	var kept []FileEntry
	used := 0
	for _, f := range files {
		content := f.Content
		// readFile already limits to maxFileBytes+1; apply exact truncation here
		if len(content) > b.maxFileBytes {
			content = content[:b.maxFileBytes] + "\n... [truncated, original " + strconv.Itoa(f.Size) + " bytes]\n"
			f.Content = content
			f.Truncated = true
		}

		if used+len(f.Content) > fileBudget {
			bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("omitted %s: exceeds size budget", f.Path))
			continue
		}

		kept = append(kept, f)
		used += len(f.Content)
	}
	bundle.Files = kept

	// Git diff (optional)
	if diffMode != "" {
		diff := b.getGitDiff(workDir, diffMode)
		if len(diff) > diffBudget {
			diff = diff[:diffBudget] + "\n... [diff truncated]\n"
		}
		if diff != "" {
			bundle.GitDiff = diff
			used += len(diff)
		}
	}

	bundle.UsedBytes = used
	return bundle
}

var contextTmpl = template.Must(template.New("ctx").Parse(contextTemplate))

// Format renders a ContextBundle into the text that gets prepended to the task.
func Format(bundle *ContextBundle) string {
	if bundle == nil || (len(bundle.Files) == 0 && bundle.GitDiff == "") {
		return ""
	}

	var buf bytes.Buffer
	_ = contextTmpl.Execute(&buf, bundle)
	return buf.String()
}

const contextTemplate = `=== PROJECT CONTEXT ===
{{- range .Files}}

### {{.Path}}{{if .Truncated}} (truncated){{end}}
` + "```" + `
{{.Content}}
` + "```" + `
{{- end}}
{{- if .GitDiff}}

### Uncommitted Changes
` + "```diff" + `
{{.GitDiff}}
` + "```" + `
{{- end}}

=== END CONTEXT ===`

// readFile reads a file with path safety checks and size limits.
// absWorkDir must already be an absolute path.
func (b *Builder) readFile(absWorkDir, relPath string) (FileEntry, error) {
	absFile, err := filepath.Abs(filepath.Join(absWorkDir, relPath))
	if err != nil {
		return FileEntry{}, fmt.Errorf("resolve path %s: %w", relPath, err)
	}
	if !strings.HasPrefix(absFile, absWorkDir+string(filepath.Separator)) && absFile != absWorkDir {
		return FileEntry{}, fmt.Errorf("path %s escapes working directory", relPath)
	}

	// Resolve symlinks and verify the real path is still within workDir
	realPath, err := filepath.EvalSymlinks(absFile)
	if err != nil {
		return FileEntry{}, fmt.Errorf("read %s: %w", relPath, err)
	}
	realWorkDir, _ := filepath.EvalSymlinks(absWorkDir)
	if !strings.HasPrefix(realPath, realWorkDir+string(filepath.Separator)) && realPath != realWorkDir {
		return FileEntry{}, fmt.Errorf("path %s resolves outside working directory", relPath)
	}

	f, err := os.Open(realPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("read %s: %w", relPath, err)
	}
	defer f.Close()

	// Read up to maxFileBytes+1 to detect truncation without loading entire file
	limit := b.maxFileBytes + 1
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)))
	if err != nil {
		return FileEntry{}, fmt.Errorf("read %s: %w", relPath, err)
	}

	// Check for binary content in first 512 bytes
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	if bytes.ContainsRune(data[:checkLen], 0) {
		return FileEntry{}, fmt.Errorf("skipped binary file: %s", relPath)
	}

	// Get actual file size for truncation reporting
	info, _ := f.Stat()
	actualSize := len(data)
	if info != nil {
		actualSize = int(info.Size())
	}

	return FileEntry{
		Path:      relPath,
		Content:   string(data),
		Size:      actualSize,
		Truncated: len(data) > b.maxFileBytes,
	}, nil
}

func (b *Builder) getGitDiff(workDir, mode string) string {
	var args []string
	switch mode {
	case "staged":
		args = []string{"diff", "--cached"}
	case "working":
		args = []string{"diff"}
	default:
		return ""
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func dedupeFiles(files []FileEntry) []FileEntry {
	seen := make(map[string]bool)
	var result []FileEntry
	for _, f := range files {
		norm := filepath.Clean(f.Path)
		if seen[norm] {
			continue
		}
		seen[norm] = true
		result = append(result, f)
	}
	return result
}
