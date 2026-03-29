package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/qq418716640/quancode/runner"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	applyPatchWorkdir string
	applyPatchFile    string
	applyPatchID      string
)

var applyPatchCmd = &cobra.Command{
	Use:          "apply-patch",
	Short:        "Apply a git patch to the working directory",
	Long:         "Applies a unified diff patch (from delegate --isolation patch) via git apply --3way. Reads from --file or stdin.",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		workDir := applyPatchWorkdir
		if workDir == "" {
			workDir, _ = os.Getwd()
		}

		if !runner.IsGitRepo(workDir) {
			return fmt.Errorf("not a git repository: %s", workDir)
		}

		var patch []byte
		var err error

		if applyPatchID != "" {
			// Load from patch cache by delegation ID
			cached, loadErr := runner.LoadCachedPatch(applyPatchID)
			if loadErr != nil {
				return loadErr
			}
			patch = []byte(cached)
		} else if applyPatchFile != "" {
			patch, err = os.ReadFile(applyPatchFile)
			if err != nil {
				return fmt.Errorf("read patch file: %w", err)
			}
		} else {
			if term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("no input: use --id <delegation-id>, --file <patch-file>, or pipe via stdin")
			}
			patch, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
		}

		patchStr := string(patch)
		if len(strings.TrimSpace(patchStr)) == 0 {
			fmt.Fprintln(os.Stderr, "[quancode] empty patch, nothing to apply")
			return nil
		}

		summary, statErr := runner.PatchSummary(workDir, patchStr)
		if statErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: could not generate patch summary: %v\n", statErr)
		} else if summary != "" {
			fmt.Fprintf(os.Stderr, "[quancode] patch summary:\n%s\n", summary)
		}

		if err := runner.ApplyPatch(workDir, patchStr); err != nil {
			return err
		}

		fmt.Fprintln(os.Stderr, "[quancode] patch applied successfully")
		return nil
	},
}

func init() {
	applyPatchCmd.Flags().StringVar(&applyPatchWorkdir, "workdir", "", "working directory (default: current)")
	applyPatchCmd.Flags().StringVar(&applyPatchFile, "file", "", "patch file to apply (default: read from stdin)")
	applyPatchCmd.Flags().StringVar(&applyPatchID, "id", "", "apply cached patch by delegation ID")
	rootCmd.AddCommand(applyPatchCmd)
}
