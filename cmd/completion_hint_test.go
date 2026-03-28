package cmd

import "testing"

func TestCompletionSetupHint(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	if got := completionSetupHint(); got == "" || got != "if you did not install via Homebrew and have not enabled completion yet, add: echo 'source <(quancode completion zsh)' >> ~/.zshrc" {
		t.Fatalf("unexpected zsh hint: %q", got)
	}

	t.Setenv("SHELL", "/bin/bash")
	if got := completionSetupHint(); got == "" || got != "if you did not install via Homebrew and have not enabled completion yet, add: echo 'source <(quancode completion bash)' >> ~/.bashrc" {
		t.Fatalf("unexpected bash hint: %q", got)
	}

	t.Setenv("SHELL", "/opt/homebrew/bin/fish")
	if got := completionSetupHint(); got == "" || got != "if you did not install via Homebrew and have not enabled completion yet, run: quancode completion fish > ~/.config/fish/completions/quancode.fish" {
		t.Fatalf("unexpected fish hint: %q", got)
	}

	t.Setenv("SHELL", "/bin/sh")
	if got := completionSetupHint(); got != "" {
		t.Fatalf("expected no hint for sh, got %q", got)
	}
}
