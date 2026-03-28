package version

import "testing"

func TestVersionDefaultIsDev(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("expected default version %q, got %q", "dev", Version)
	}
}
