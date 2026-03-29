package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// worktreeIgnoreRules contains patterns that should never appear in a
// collected patch. These are appended to the worktree's .gitignore so
// git add -A skips them automatically.
var worktreeIgnoreRules = []string{
	".tmp/",
	".gocache/",
	".cache/",
	"node_modules/",
	"__pycache__/",
	".venv/",
	"*.pyc",
}

// ensureWorktreeIgnore writes build artifact exclusions to the worktree's
// .git/info/exclude file. Unlike .gitignore, this is a local-only config
// that won't be picked up by CollectPatch (git add -A).
func ensureWorktreeIgnore(worktreeDir string) {
	// In a worktree, .git is a file pointing to the real gitdir.
	// Read it to find the actual git directory.
	gitPath := filepath.Join(worktreeDir, ".git")
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return
	}
	// Format: "gitdir: /path/to/main/.git/worktrees/wt-xxx"
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir: ") {
		return
	}
	gitDir := strings.TrimPrefix(content, "gitdir: ")

	infoDir := filepath.Join(gitDir, "info")
	os.MkdirAll(infoDir, 0755)
	excludePath := filepath.Join(infoDir, "exclude")

	rules := "\n# quancode worktree exclusions\n" + strings.Join(worktreeIgnoreRules, "\n") + "\n"
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(rules)
}

// IsGitRepo checks if the directory is inside a git repository.
func IsGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// PruneOrphanWorktrees removes leftover worktree directories from previous
// runs that were not cleaned up (e.g. due to SIGKILL). It compares entries
// in .quancode/worktrees/ against `git worktree list` to identify orphans.
func PruneOrphanWorktrees(repoDir string) int {
	base := filepath.Join(repoDir, ".quancode", "worktrees")
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0
	}
	if len(entries) == 0 {
		return 0
	}

	// Get active worktree directory names from git.
	// We compare by basename to avoid symlink resolution issues
	// (e.g. macOS /tmp → /private/tmp).
	activeNames := make(map[string]bool)
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "worktree ") {
				wtPath := strings.TrimPrefix(line, "worktree ")
				activeNames[filepath.Base(wtPath)] = true
			}
		}
	}

	pruned := 0
	cutoff := time.Now().Add(-time.Hour)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "wt-") {
			continue
		}
		if activeNames[e.Name()] {
			continue
		}
		// Skip recently created directories to avoid racing with
		// concurrent CreateWorktree calls.
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		wtPath := filepath.Join(base, e.Name())
		// Orphan: not in git worktree list and older than 1 hour
		exec.Command("git", "worktree", "remove", "--force", wtPath).Run()
		os.RemoveAll(wtPath)
		pruned++
	}

	// Also let git clean up its own stale metadata
	if pruned > 0 {
		exec.Command("git", "worktree", "prune").Run()
	}
	return pruned
}

// CreateWorktree creates a temporary git worktree for isolated execution.
// Returns the worktree path and a cleanup function.
func CreateWorktree(repoDir string) (string, func(), error) {
	// Create worktree in .quancode/worktrees/
	base := filepath.Join(repoDir, ".quancode", "worktrees")
	if err := os.MkdirAll(base, 0755); err != nil {
		return "", nil, fmt.Errorf("create worktree dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp(base, "wt-")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}

	branchName := "quancode-wt-" + filepath.Base(tmpDir)

	cmd := exec.Command("git", "worktree", "add", "--detach", tmpDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("git worktree add: %s: %w", string(out), err)
	}

	// Write build artifact exclusions to worktree's .git/info/exclude so
	// CollectPatch never picks up caches regardless of user config.
	ensureWorktreeIgnore(tmpDir)

	cleanup := func() {
		exec.Command("git", "worktree", "remove", "--force", tmpDir).Run()
		// Clean up the branch if we created one
		exec.Command("git", "branch", "-D", branchName).Run()
		os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup, nil
}

// CollectPatch generates a unified diff of all changes in the worktree.
// Returns patch content and list of changed files.
func CollectPatch(worktreeDir string) (string, []string, error) {
	// Stage everything so we can diff
	stageCmd := exec.Command("git", "add", "-A")
	stageCmd.Dir = worktreeDir
	if out, err := stageCmd.CombinedOutput(); err != nil {
		return "", nil, fmt.Errorf("git add -A: %s: %w", string(out), err)
	}

	// Get the patch
	diffCmd := exec.Command("git", "diff", "--cached", "--binary")
	diffCmd.Dir = worktreeDir
	patchBytes, err := diffCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("git diff: %w", err)
	}

	// Get changed file names
	namesCmd := exec.Command("git", "diff", "--cached", "--name-only")
	namesCmd.Dir = worktreeDir
	namesBytes, err := namesCmd.Output()
	if err != nil {
		return string(patchBytes), nil, fmt.Errorf("git diff --name-only: %w", err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(namesBytes)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}

	return string(patchBytes), files, nil
}

// PatchSummary runs git apply --stat to show what a patch would change
// without actually applying it. Returns the summary text.
func PatchSummary(targetDir, patch string) (string, error) {
	if patch == "" {
		return "", nil
	}

	cmd := exec.Command("git", "apply", "--stat")
	cmd.Dir = targetDir
	cmd.Stdin = strings.NewReader(patch)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git apply --stat: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// CheckPatchConflicts runs git apply --check to identify which files
// would conflict when applying a patch. Returns the list of conflicting
// file paths extracted from git's error output.
func CheckPatchConflicts(targetDir, patch string) []string {
	if patch == "" {
		return nil
	}
	cmd := exec.Command("git", "apply", "--check", "--3way")
	cmd.Dir = targetDir
	cmd.Stdin = strings.NewReader(patch)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	return parseConflictFiles(string(out))
}

// parseConflictFiles extracts file paths from git apply error output.
// Matches patterns like "error: patch failed: path/file.go:123" and
// "error: path/file.go: patch does not apply".
func parseConflictFiles(output string) []string {
	seen := make(map[string]bool)
	var files []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "error:") {
			continue
		}
		rest := strings.TrimPrefix(line, "error:")
		rest = strings.TrimSpace(rest)

		// "patch failed: path/file.go:123"
		if strings.HasPrefix(rest, "patch failed: ") {
			path := strings.TrimPrefix(rest, "patch failed: ")
			if idx := strings.LastIndex(path, ":"); idx > 0 {
				path = path[:idx]
			}
			if !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
			continue
		}

		// "path/file.go: patch does not apply"
		if strings.HasSuffix(rest, ": patch does not apply") {
			path := strings.TrimSuffix(rest, ": patch does not apply")
			if !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		}
	}
	return files
}

// ApplyPatch applies a git patch to the target directory.
func ApplyPatch(targetDir, patch string) error {
	if patch == "" {
		return nil
	}

	cmd := exec.Command("git", "apply", "--3way")
	cmd.Dir = targetDir
	cmd.Stdin = strings.NewReader(patch)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git apply: %s: %w", string(out), err)
	}
	return nil
}
