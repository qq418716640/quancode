package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qq418716640/quancode/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Detect installed CLIs and generate a quancode config",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("[quancode] scanning for installed AI coding CLIs...")

		// Detect which known CLIs are available (sorted for deterministic order)
		var knownKeys []string
		for key := range config.KnownAgents {
			knownKeys = append(knownKeys, key)
		}
		sort.Strings(knownKeys)

		var found []string
		for _, key := range knownKeys {
			ac := config.KnownAgents[key]
			if _, err := exec.LookPath(ac.Command); err == nil {
				fmt.Printf("  found: %s (%s)\n", ac.Name, ac.Command)
				found = append(found, key)
			}
		}

		if len(found) == 0 {
			fmt.Println("\n  no known AI coding CLIs found in PATH.")
			fmt.Println("  supported: claude, codex, aider, opencode")
			return nil
		}

		// Choose primary agent
		fmt.Printf("\nwhich CLI should be the primary agent?\n")
		for i, key := range found {
			fmt.Printf("  %d) %s\n", i+1, key)
		}
		fmt.Print("choose [1]: ")

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		primaryIdx := 0
		if input != "" {
			for i, key := range found {
				if input == fmt.Sprintf("%d", i+1) || input == key {
					primaryIdx = i
					break
				}
			}
		}
		primary := found[primaryIdx]

		// Build config
		cfg := &config.Config{
			DefaultPrimary: primary,
			Agents:         make(map[string]config.AgentConfig),
		}
		for _, key := range found {
			cfg.Agents[key] = config.KnownAgents[key]
		}

		// Write config file
		cfgPath := config.ConfigPath()
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}

		// Check if file exists
		if _, err := os.Stat(cfgPath); err == nil {
			fmt.Printf("\nconfig already exists at %s\n", cfgPath)
			fmt.Print("overwrite? [y/N]: ")
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) != "y" {
				fmt.Println("aborted.")
				return nil
			}
		}

		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshal config: %w", err)
		}

		if err := os.WriteFile(cfgPath, data, 0644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		fmt.Printf("\nconfig written to %s\n", cfgPath)
		fmt.Printf("primary agent: %s\n", primary)
		fmt.Printf("agents: %s\n", strings.Join(found, ", "))
		fmt.Println("\nrun `quancode doctor` to verify your setup.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
