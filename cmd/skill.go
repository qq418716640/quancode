//go:generate cp ../skills/quancode/SKILL.md embedded_skills/quancode/SKILL.md
//go:generate cp ../skills/quancode/README.md embedded_skills/quancode/README.md

package cmd

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

//go:embed embedded_skills/quancode/SKILL.md
var embeddedSkillMD []byte

//go:embed embedded_skills/quancode/README.md
var embeddedSkillREADME []byte

var skillForce bool

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage QuanCode skills",
}

var skillInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the /quancode skill to ~/.claude/skills/",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}

		targetDir := filepath.Join(home, ".claude", "skills", "quancode")

		files := map[string][]byte{
			"SKILL.md":  embeddedSkillMD,
			"README.md": embeddedSkillREADME,
		}

		// Check if already installed
		existing := filepath.Join(targetDir, "SKILL.md")
		if data, err := os.ReadFile(existing); err == nil {
			if string(data) == string(embeddedSkillMD) {
				fmt.Println("[quancode] skill already installed and up to date")
				return nil
			}
			if !skillForce {
				fmt.Println("[quancode] skill already installed but differs from built-in version")
				fmt.Println("[quancode] use --force to overwrite")
				return nil
			}
			fmt.Println("[quancode] overwriting existing skill (--force)")
		}

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("create skill dir: %w", err)
		}

		for name, content := range files {
			path := filepath.Join(targetDir, name)
			if err := os.WriteFile(path, content, 0644); err != nil {
				return fmt.Errorf("write %s: %w", name, err)
			}
		}

		fmt.Printf("[quancode] skill installed to %s\n", targetDir)
		fmt.Println("[quancode] /quancode command is now available in Claude Code")
		return nil
	},
}

var skillUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the /quancode skill from ~/.claude/skills/",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}

		targetDir := filepath.Join(home, ".claude", "skills", "quancode")
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			fmt.Println("[quancode] skill not installed")
			return nil
		}

		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("remove skill: %w", err)
		}

		fmt.Printf("[quancode] skill removed from %s\n", targetDir)
		return nil
	},
}

func init() {
	skillInstallCmd.Flags().BoolVar(&skillForce, "force", false, "overwrite existing skill files")
	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillUninstallCmd)
	rootCmd.AddCommand(skillCmd)
}

// Ensure embed import is used
var _ embed.FS
