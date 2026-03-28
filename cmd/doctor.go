package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/qq418716640/quancode/config"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check quancode setup and diagnose problems",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("[quancode] running health checks...")
		fmt.Println()
		passed := 0
		failed := 0

		check := func(name string, ok bool, detail string) {
			if ok {
				fmt.Printf("  PASS  %s\n", name)
				passed++
			} else {
				fmt.Printf("  FAIL  %s\n", name)
				if detail != "" {
					fmt.Printf("        %s\n", detail)
				}
				failed++
			}
		}

		// 1. Config file
		cfgPath := config.ConfigPath()
		_, cfgExists := os.Stat(cfgPath)
		check("config file exists", cfgExists == nil, fmt.Sprintf("expected at %s, run `quancode init`", cfgPath))

		// 2. Load config
		cfg, err := config.Load(cfgFile)
		if err != nil {
			check("config loads", false, err.Error())
			fmt.Printf("\n%d passed, %d failed\n", passed, failed)
			return nil
		}
		check("config loads", true, "")

		// 3. Validate config
		problems := cfg.Validate()
		check("config valid", len(problems) == 0, strings.Join(problems, "; "))

		// 4. Primary agent
		primaryOk := false
		if ac, ok := cfg.Agents[cfg.DefaultPrimary]; ok && ac.Enabled {
			if _, err := exec.LookPath(ac.Command); err == nil {
				primaryOk = true
			}
		}
		check(fmt.Sprintf("primary agent (%s) available", cfg.DefaultPrimary), primaryOk,
			fmt.Sprintf("command %q not found in PATH", cfg.Agents[cfg.DefaultPrimary].Command))

		// 5. Each agent availability + version
		fmt.Println()
		fmt.Println("  agents:")
		for key, ac := range cfg.Agents {
			if !ac.Enabled {
				fmt.Printf("    %s: disabled\n", key)
				continue
			}
			path, lookErr := exec.LookPath(ac.Command)
			if lookErr != nil {
				check(fmt.Sprintf("  agent %s (%s)", key, ac.Command), false, "not found in PATH")
				continue
			}

			// Try to get version
			version := getVersion(ac.Command)
			if version != "" {
				fmt.Printf("    PASS  %s: %s (%s)\n", key, version, path)
			} else {
				fmt.Printf("    PASS  %s: available (%s)\n", key, path)
			}
			passed++
		}

		// 6. quancode binary in PATH
		_, quanInPath := exec.LookPath("quancode")
		check("quancode in PATH", quanInPath == nil, "add $HOME/go/bin to PATH or install to /usr/local/bin")

		if hint := completionSetupHint(); hint != "" {
			fmt.Println()
			fmt.Printf("  tip   detected shell: %s\n", shellNameForCompletion())
			fmt.Printf("        %s\n", hint)
		}

		fmt.Printf("\n%d passed, %d failed\n", passed, failed)
		if failed > 0 {
			os.Exit(1)
		}
		return nil
	},
}

func getVersion(command string) string {
	// Try --version, then -V
	for _, flag := range []string{"--version", "-V"} {
		out, err := exec.Command(command, flag).Output()
		if err == nil {
			v := strings.TrimSpace(string(out))
			// Take first line only
			if idx := strings.Index(v, "\n"); idx > 0 {
				v = v[:idx]
			}
			if v != "" {
				return v
			}
		}
	}
	return ""
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
