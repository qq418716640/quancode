package cmd

import "testing"

func TestTruncateForField(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{name: "short string unchanged", input: "abc", maxLen: 5, want: "abc"},
		{name: "equal to max length unchanged", input: "abcde", maxLen: 5, want: "abcde"},
		{name: "long string truncated", input: "abcdef", maxLen: 5, want: "abcde...[truncated]"},
		{name: "empty string", input: "", maxLen: 5, want: ""},
		{name: "zero max length", input: "abcdef", maxLen: 0, want: "...[truncated]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateForField(tt.input, tt.maxLen); got != tt.want {
				t.Fatalf("truncateForField(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
