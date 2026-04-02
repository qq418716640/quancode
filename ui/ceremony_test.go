package ui

import "testing"

func TestFirstLine(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello world", 80, "hello world"},
		{"line one\nline two", 80, "line one"},
		{"a very long string that exceeds the limit", 20, "a very long strin..."},
		{"short", 5, "short"},
		{"exactly", 7, "exactly"},
		{"abc", 2, "ab"},
		{"abcdef", 3, "abc"},
		{"", 10, ""},
	}
	for _, tt := range tests {
		got := FirstLine(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("FirstLine(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "0ms"},
		{500, "500ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{12300, "12.3s"},
		{59999, "60.0s"},
		{60000, "1m0s"},
		{65000, "1m5s"},
		{125000, "2m5s"},
		{3600000, "60m0s"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.ms)
		if got != tt.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestFallbackChainFormat(t *testing.T) {
	// FallbackChain writes to stderr, so we test the formatting logic indirectly.
	// Verify that the function doesn't panic on edge cases.

	// Empty chain — should be a no-op
	FallbackChain(nil)
	FallbackChain([]ChainLink{})

	// Single element — should be a no-op (no fallback occurred)
	FallbackChain([]ChainLink{{Agent: "codex", FailureClass: ""}})

	// Normal chain with fallback
	FallbackChain([]ChainLink{
		{Agent: "claude", FailureClass: "timed_out"},
		{Agent: "codex", FailureClass: ""},
	})

	// All failures
	FallbackChain([]ChainLink{
		{Agent: "claude", FailureClass: "timed_out"},
		{Agent: "codex", FailureClass: "rate_limited"},
		{Agent: "qoder", FailureClass: "agent_failed"},
	})
}
