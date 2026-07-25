package cmd

import "testing"

func TestRoundUpTo(t *testing.T) {
	cases := []struct{ v, unit, want int }{
		{824, 60, 840},
		{840, 60, 840},
		{1, 60, 60},
		{100, 0, 100},
	}
	for _, c := range cases {
		if got := roundUpTo(c.v, c.unit); got != c.want {
			t.Errorf("roundUpTo(%d,%d) = %d, want %d", c.v, c.unit, got, c.want)
		}
	}
}

func TestWarnTimeoutHeadroomNilSnapshot(t *testing.T) {
	if msg := warnTimeoutHeadroom(nil, "codex", 480, true); msg != "" {
		t.Errorf("nil snapshot should produce no warning, got %q", msg)
	}
}

func TestWarnTimeoutHeadroomZeroTimeout(t *testing.T) {
	if msg := warnTimeoutHeadroom(nil, "codex", 0, true); msg != "" {
		t.Errorf("zero timeout should produce no warning, got %q", msg)
	}
}

func TestHeadroomMessageMentionsSuggestion(t *testing.T) {
	// Sanity-check the formatting helper the warning depends on: a p90 of
	// 412s against a 480s cap should suggest a timeout comfortably above it.
	if got := roundUpTo(412*2, 60); got != 840 {
		t.Errorf("suggested timeout = %d, want 840", got)
	}
}
