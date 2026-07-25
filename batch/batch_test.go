package batch

import (
	"os"
	"testing"
)

func withDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	SetDirForTest(dir)
}

func TestNewAssignsStableIDs(t *testing.T) {
	b := New("batch_1", "do {{.Item}}", "", "/tmp", "", "", "", 0, []string{"a", "b"})
	if len(b.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(b.Items))
	}
	// Same index+text must always produce the same ID so resume can match.
	if b.Items[0].ID != ItemID(0, "a") {
		t.Errorf("ID = %q, not stable", b.Items[0].ID)
	}
	if b.Items[0].ID == b.Items[1].ID {
		t.Error("distinct items must get distinct IDs")
	}
}

// TestDuplicateItemsArePreserved: repeating an item may be intentional, and
// deduplicating would silently drop work the user asked for.
func TestDuplicateItemsArePreserved(t *testing.T) {
	b := New("batch_1", "t", "", "/tmp", "", "", "", 0, []string{"same", "same"})
	if len(b.Items) != 2 {
		t.Fatalf("got %d items, want 2 — duplicates must not be collapsed", len(b.Items))
	}
	if b.Items[0].ID == b.Items[1].ID {
		t.Errorf("duplicate item text must still get distinct IDs, both were %q", b.Items[0].ID)
	}
}

func TestNeedsRunSkipsSucceeded(t *testing.T) {
	it := Item{Status: StatusSucceeded}
	if it.NeedsRun(false) || it.NeedsRun(true) {
		t.Error("a succeeded item must never be re-run, even with --retry-failed")
	}
}

func TestNeedsRunPendingAndInterrupted(t *testing.T) {
	for _, st := range []string{StatusPending, StatusInterrupted, StatusRunning} {
		it := Item{Status: st}
		if !it.NeedsRun(false) {
			t.Errorf("status %q should run", st)
		}
	}
}

// TestNeedsRunTransientVsDeterministic is the guard against the A-BATCH-023
// pattern: a deterministic failure re-sent five times, never succeeding.
// Transient failures retry automatically; deterministic ones need an
// explicit --retry-failed.
func TestNeedsRunTransientVsDeterministic(t *testing.T) {
	cases := []struct {
		class         string
		autoRetry     bool
		withRetryFlag bool
	}{
		{"timed_out", true, true},
		{"rate_limited", true, true},
		{"launch_failure", true, true},
		{"agent_failed", false, true},
		{"verify_failed", false, true},
		{"patch_conflict", false, true},
		{"orchestration_error", false, true},
		{"template_error", false, true},
		{"", false, true},
	}
	for _, c := range cases {
		it := Item{Status: StatusFailed, FailureClass: c.class}
		if got := it.NeedsRun(false); got != c.autoRetry {
			t.Errorf("class %q: NeedsRun(false) = %v, want %v", c.class, got, c.autoRetry)
		}
		if got := it.NeedsRun(true); got != c.withRetryFlag {
			t.Errorf("class %q: NeedsRun(true) = %v, want %v", c.class, got, c.withRetryFlag)
		}
	}
}

func TestPendingCount(t *testing.T) {
	b := New("batch_1", "t", "", "/tmp", "", "", "", 0, []string{"a", "b", "c", "d"})
	b.Items[0].Status = StatusSucceeded
	b.Items[1].Status = StatusFailed
	b.Items[1].FailureClass = "agent_failed" // deterministic
	b.Items[2].Status = StatusFailed
	b.Items[2].FailureClass = "timed_out" // transient
	// Items[3] stays pending.

	if got := b.Pending(false); got != 2 {
		t.Errorf("Pending(false) = %d, want 2 (transient failure + pending)", got)
	}
	if got := b.Pending(true); got != 3 {
		t.Errorf("Pending(true) = %d, want 3 (both failures + pending)", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withDir(t)
	b := New("batch_rt", "do {{.Item}}", "src.tmpl", "/tmp/wd", "/tmp/wd", "codex", "inplace", 120,
		[]string{"one", "two"})
	b.Items[0].Status = StatusSucceeded
	b.Items[0].DelegationID = "del_abc"
	b.Generation = 2

	if err := Save(b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load("batch_rt")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Template != b.Template || got.TemplateHash != b.TemplateHash {
		t.Error("template did not survive round-trip")
	}
	if got.Agent != "codex" || got.Isolation != "inplace" || got.TimeoutSecs != 120 {
		t.Error("manifest execution constraints did not survive round-trip")
	}
	if len(got.Items) != 2 || got.Items[0].Status != StatusSucceeded || got.Items[0].DelegationID != "del_abc" {
		t.Errorf("item state did not survive round-trip: %+v", got.Items)
	}
	if got.Generation != 2 {
		t.Errorf("Generation = %d, want 2", got.Generation)
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	withDir(t)
	b := New("batch_future", "t", "", "/tmp", "", "", "", 0, []string{"a"})
	b.SchemaVersion = SchemaVersion + 1
	if err := Save(b); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("batch_future"); err == nil {
		t.Error("Load should refuse state written by a newer quancode")
	}
}

func TestLoadMissingBatch(t *testing.T) {
	withDir(t)
	if _, err := Load("batch_nope"); err == nil {
		t.Error("Load of a nonexistent batch should error")
	}
}

func TestTemplateHashChangesWithTemplate(t *testing.T) {
	// Resume relies on the stored template; the hash is what lets a human
	// notice their template file drifted from the frozen copy.
	if HashTemplate("a") == HashTemplate("b") {
		t.Error("different templates must hash differently")
	}
	if HashTemplate("a") != HashTemplate("a") {
		t.Error("hashing must be deterministic")
	}
}

func TestListNewestFirst(t *testing.T) {
	withDir(t)
	older := New("batch_old", "t", "", "/tmp", "", "", "", 0, []string{"a"})
	older.CreatedAt = "2026-01-01T00:00:00Z"
	newer := New("batch_new", "t", "", "/tmp", "", "", "", 0, []string{"a"})
	newer.CreatedAt = "2026-07-01T00:00:00Z"
	if err := Save(older); err != nil {
		t.Fatal(err)
	}
	if err := Save(newer); err != nil {
		t.Fatal(err)
	}

	got, err := List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "batch_new" {
		t.Errorf("List should return newest first, got %v", ids(got))
	}
	if limited, _ := List(1); len(limited) != 1 {
		t.Errorf("List(1) should honor the limit, got %d", len(limited))
	}
}

func TestSaveDoesNotLeaveTempFiles(t *testing.T) {
	withDir(t)
	b := New("batch_tmp", "t", "", "/tmp", "", "", "", 0, []string{"a"})
	if err := Save(b); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.Name()) > 4 && e.Name()[len(e.Name())-4:] == ".tmp" {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestCounts(t *testing.T) {
	b := New("batch_c", "t", "", "/tmp", "", "", "", 0, []string{"a", "b", "c"})
	b.Items[0].Status = StatusSucceeded
	b.Items[1].Status = StatusFailed
	c := b.Counts()
	if c[StatusSucceeded] != 1 || c[StatusFailed] != 1 || c[StatusPending] != 1 {
		t.Errorf("Counts = %v", c)
	}
}

func ids(bs []*Batch) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.ID)
	}
	return out
}

// TestClaimIsExclusive covers the TOCTOU that a separate check-then-write
// allowed: two concurrent resumes could both see OwnerPID=0 and both run.
func TestClaimIsExclusive(t *testing.T) {
	withDir(t)
	b := New("batch_claim", "t", "", "/tmp", "", "", "", 0, []string{"a"})
	if err := Save(b); err != nil {
		t.Fatal(err)
	}

	alive := func(pid int, start int64) bool { return pid == 4242 }

	first, err := Claim("batch_claim", 4242, 1, alive)
	if err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	if first.OwnerPID != 4242 {
		t.Errorf("OwnerPID = %d, want 4242", first.OwnerPID)
	}
	if first.Generation != 1 {
		t.Errorf("Generation = %d, want 1", first.Generation)
	}

	if _, err := Claim("batch_claim", 9999, 1, alive); err == nil {
		t.Error("a second live process must not be able to claim the same batch")
	}
}

// TestClaimTakesOverFromDeadOwner: a crashed runner must not lock the batch
// forever.
func TestClaimTakesOverFromDeadOwner(t *testing.T) {
	withDir(t)
	b := New("batch_dead", "t", "", "/tmp", "", "", "", 0, []string{"a"})
	b.OwnerPID = 111
	b.OwnerPIDStartTime = 5
	if err := Save(b); err != nil {
		t.Fatal(err)
	}

	neverAlive := func(int, int64) bool { return false }
	got, err := Claim("batch_dead", 222, 9, neverAlive)
	if err != nil {
		t.Fatalf("should take over from a dead owner: %v", err)
	}
	if got.OwnerPID != 222 {
		t.Errorf("OwnerPID = %d, want 222", got.OwnerPID)
	}
}

func TestClaimBumpsGenerationEachTime(t *testing.T) {
	withDir(t)
	b := New("batch_gen", "t", "", "/tmp", "", "", "", 0, []string{"a"})
	if err := Save(b); err != nil {
		t.Fatal(err)
	}
	dead := func(int, int64) bool { return false }
	for want := 1; want <= 3; want++ {
		got, err := Claim("batch_gen", 100+want, 1, dead)
		if err != nil {
			t.Fatal(err)
		}
		if got.Generation != want {
			t.Errorf("Generation = %d, want %d", got.Generation, want)
		}
	}
}

// TestStaleTransientClassWouldLoopForever documents why the pre-launch
// failure branches clear FailureClass: a failed item still carrying a
// transient class re-runs automatically on every resume.
func TestStaleTransientClassWouldLoopForever(t *testing.T) {
	stale := Item{Status: StatusFailed, FailureClass: "rate_limited"}
	if !stale.NeedsRun(false) {
		t.Fatal("precondition: a transient class auto-retries")
	}
	cleared := Item{Status: StatusFailed, FailureClass: ""}
	if cleared.NeedsRun(false) {
		t.Error("a failure with no transient class must wait for --retry-failed")
	}
}
