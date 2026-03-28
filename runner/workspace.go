package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsGitRepo checks if the directory is inside a git repository.
func IsGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
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
