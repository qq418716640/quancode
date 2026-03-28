package cmd

import (
	"os"
	"path/filepath"
	"strings"
)

func completionSetupHint() string {
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "zsh":
		return "if you did not install via Homebrew and have not enabled completion yet, add: echo 'source <(quancode completion zsh)' >> ~/.zshrc"
	case "bash":
		return "if you did not install via Homebrew and have not enabled completion yet, add: echo 'source <(quancode completion bash)' >> ~/.bashrc"
	case "fish":
		return "if you did not install via Homebrew and have not enabled completion yet, run: quancode completion fish > ~/.config/fish/completions/quancode.fish"
	default:
		return ""
	}
}

func shellNameForCompletion() string {
	shell := filepath.Base(os.Getenv("SHELL"))
	if shell == "" {
		return ""
	}
	return strings.TrimSpace(shell)
}
