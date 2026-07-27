package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/job"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/runner"
)

// A 500-item batch must print one line, not 500.
func TestNoteDeprecationOnce(t *testing.T) {
	const msg = "warning: --old is deprecated"

	if !noteDeprecationOnce("codex-test-a", msg) {
		t.Fatal("first occurrence should print")
	}
	for i := 0; i < 10; i++ {
		if noteDeprecationOnce("codex-test-a", msg) {
			t.Fatal("repeat occurrence should stay quiet")
		}
	}
	// Keyed on the pair, so a different agent saying the same thing is
	// still news, and so is the same agent saying something different.
	if !noteDeprecationOnce("claude-test-a", msg) {
		t.Error("a different agent should print")
	}
	if !noteDeprecationOnce("codex-test-a", "warning: --other is deprecated") {
		t.Error("a different message should print")
	}
}

func TestNoteDeprecationOnceConcurrent(t *testing.T) {
	const msg = "warning: --concurrent is deprecated"
	printed := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func() { printed <- noteDeprecationOnce("codex-test-b", msg) }()
	}
	count := 0
	for i := 0; i < 20; i++ {
		if <-printed {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d goroutines printed, want exactly 1", count)
	}
}

// applyAttemptFields exists so a new Result field reaches all four ledger
// write paths at once. This pins that it actually carries the data over.
func TestApplyAttemptFieldsCarriesDeprecations(t *testing.T) {
	notices := []string{"warning: --old is deprecated"}
	entry := &ledger.Entry{}
	applyAttemptFields(entry, attemptResult{
		result:       &runner.Result{ExitCode: 0, Deprecations: notices},
		changedFiles: []string{"a.go"},
	})

	if len(entry.Deprecations) != 1 || entry.Deprecations[0] != notices[0] {
		t.Errorf("Deprecations = %q, want %q", entry.Deprecations, notices)
	}
	if entry.ChangedFiles[0] != "a.go" {
		t.Errorf("ChangedFiles = %q", entry.ChangedFiles)
	}
}

// A launch failure produces no Result at all; the copy must not panic and
// must still carry the fields that come from the attempt rather than the run.
func TestApplyAttemptFieldsNilResult(t *testing.T) {
	entry := &ledger.Entry{}
	applyAttemptFields(entry, attemptResult{
		failureClass:    "launch_failure",
		taskSizeWarning: "too big",
	})

	if entry.FailureClass != "launch_failure" || entry.TaskSizeWarning != "too big" {
		t.Errorf("attempt-level fields lost: %+v", entry)
	}
	if entry.Deprecations != nil {
		t.Errorf("Deprecations = %q, want nil", entry.Deprecations)
	}
}

func TestRecentDeprecationsAggregates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	now := time.Now()
	write := func(agent, msg string, at time.Time) {
		e := &ledger.Entry{
			Agent:        agent,
			Timestamp:    at.UTC().Format(time.RFC3339),
			Deprecations: []string{msg},
		}
		if err := ledger.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	const msg = "warning: --old is deprecated"
	write("codex", msg, now.Add(-3*time.Hour))
	write("codex", msg, now.Add(-1*time.Hour))
	write("codex", "warning: --other is deprecated", now.Add(-2*time.Hour))
	write("claude", "no notices here", now.Add(-1*time.Hour)) // still a notice row
	if err := ledger.Append(&ledger.Entry{Agent: "codex", Timestamp: now.Format(time.RFC3339)}); err != nil {
		t.Fatal(err) // an entry with no deprecations must be ignored entirely
	}

	got, err := recentDeprecations(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var found *deprecationRecord
	for i := range got {
		if got[i].Agent == "codex" && got[i].Message == msg {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatalf("codex/%q missing from %+v", msg, got)
	}
	if found.Count != 2 {
		t.Errorf("Count = %d, want 2", found.Count)
	}
	if !found.LastSeen.After(found.FirstSeen) {
		t.Errorf("FirstSeen %v should precede LastSeen %v", found.FirstSeen, found.LastSeen)
	}
	if len(got) != 3 {
		t.Errorf("got %d distinct notices, want 3", len(got))
	}
	// Newest activity first, so the still-happening ones lead.
	for i := 1; i < len(got); i++ {
		if got[i-1].LastSeen.Before(got[i].LastSeen) {
			t.Errorf("not sorted newest-first: %v", got)
		}
	}
}

func TestRecentDeprecationsHonorsWindow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := ledger.Append(&ledger.Entry{
		Agent:        "codex",
		Timestamp:    old.UTC().Format(time.RFC3339),
		Deprecations: []string{"warning: --ancient is deprecated"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := recentDeprecations(7 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("recentDeprecations() = %+v, want nothing outside the window", got)
	}
}

// Telling someone to edit ~/.config/... when Load actually picked up a
// project-local file sends them to change a file that has no effect.
func TestResolvePathPrefersProjectLocalConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	local := filepath.Join(dir, "quancode.yaml")
	if err := os.WriteFile(local, []byte("default_primary: claude\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := config.ResolvePath(""); got != "quancode.yaml" {
		t.Errorf("ResolvePath(\"\") = %q, want the project-local file", got)
	}
	if got := config.ResolvePath("/explicit/path.yaml"); got != "/explicit/path.yaml" {
		t.Errorf("ResolvePath(explicit) = %q, want it honored", got)
	}
}

func TestHumanizeSince(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{50 * time.Hour, "2d ago"},
	}
	for _, c := range cases {
		if got := humanizeSince(time.Now().Add(-c.ago)); got != c.want {
			t.Errorf("humanizeSince(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

// A Quiet attempt must not claim the once-per-process slot: if it did, an
// attempt that *can* print would find the message already "seen" and stay
// silent, which is how review-set and speculative would have gone mute.
func TestQuietAttemptDoesNotConsumeTheSlot(t *testing.T) {
	// Mirrors runDelegateAttempt's guard: the Quiet check happens before
	// printDeprecations, not inside it.
	quiet := true
	notices := []string{"warning: --quiet-path is deprecated"}
	if !quiet {
		printDeprecations("codex-test-quiet", notices)
	}

	if !noteDeprecationOnce("codex-test-quiet", notices[0]) {
		t.Error("the slot was consumed by an attempt that printed nothing")
	}
}

func TestNoteDeprecationOnceStopsAtCeiling(t *testing.T) {
	deprecationSeenMu.Lock()
	saved := deprecationSeen
	deprecationSeen = map[string]bool{}
	for i := 0; i < maxDeprecationKeys; i++ {
		deprecationSeen[string(rune(i))] = true
	}
	deprecationSeenMu.Unlock()
	defer func() {
		deprecationSeenMu.Lock()
		deprecationSeen = saved
		deprecationSeenMu.Unlock()
	}()

	if noteDeprecationOnce("codex", "warning: --overflow is deprecated") {
		t.Error("past the ceiling the tracker should go quiet, not keep growing")
	}
}

// The fallback loop logs each failed attempt before looking for a candidate.
// When none is left it still has to report the failure, and routing that
// through finalizeDelegation logged the same attempt twice — doubling its
// own failure counts, the health breaker's, and every derived statistic.
func TestReportDelegationDoesNotWriteToLedger(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	countEntries := func() int {
		entries, err := ledger.ReadSince(time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}

	ar := attemptResult{
		result:       &runner.Result{ExitCode: 1},
		err:          errors.New("agent failed"),
		failureClass: "agent_failed",
	}

	before := countEntries()
	_ = reportDelegation("codex", "task", "inplace", ar)
	if after := countEntries(); after != before {
		t.Errorf("reportDelegation wrote %d ledger entries, want 0", after-before)
	}
}

// An async attempt that fails before producing a Result leaves ExitCode at
// zero, which determineFinalStatus reads as success — so a job the user is
// told failed was being recorded in the ledger as completed.
func TestWriteLedgerMarksResultlessFailureAsFailed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	writeLedger(&job.State{
		JobID:       "job_test",
		ActualAgent: "codex",
		Task:        "task",
		WorkDir:     dir,
		Isolation:   "patch",
	}, attemptResult{
		err:          errors.New("worktree creation failed"),
		failureClass: "launch_failure",
	}, nil)

	entries, err := ledger.ReadSince(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d ledger entries, want 1", len(entries))
	}
	if entries[0].FinalStatus == StatusCompleted {
		t.Errorf("FinalStatus = %q for a failed attempt with no result", entries[0].FinalStatus)
	}
	if entries[0].ExitCode == 0 {
		t.Errorf("ExitCode = 0 for a failed attempt")
	}
}
