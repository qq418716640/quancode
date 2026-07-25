// Package batch runs one task template across a list of independent items,
// with a frozen manifest and resumable per-item state.
//
// Motivation: the 2026-05..07 ledger review found batch work was the single
// largest use of QuanCode — 561 of 1279 attempts (44%) came from two projects
// applying one task template over a list of scopes — and it had no tool
// support. Users drove it by hand, one `delegate` at a time, which showed up
// as 116 tasks re-delegated across separate runs for 291 attempts (23% of all
// activity). One scope was re-sent seven times before succeeding; another was
// re-sent five times and never succeeded.
//
// Two facts are kept deliberately separate:
//
//   - The manifest is immutable and answers "what was this batch supposed to
//     do": the exact template text, the item list, and the execution
//     constraints at creation time. The ledger cannot answer this — it only
//     records what actually ran, so it can never tell resume which items were
//     never started.
//   - The state is mutable and answers "what should the scheduler do next".
//
// The ledger remains the audit trail for individual attempts.
//
// Guarantee: successful items are not re-run, and an interrupted batch can be
// resumed. This is "skip-on-success with at-least-once execution", not
// exactly-once — an agent can modify files and then the process can die
// before the result is durably recorded. Batch tasks should be written to
// tolerate being repeated.
package batch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SchemaVersion guards against reading state written by a newer binary.
const SchemaVersion = 1

// Item statuses.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	// StatusInterrupted marks an item whose runner died mid-execution. It is
	// deliberately distinct from pending: the agent may have already changed
	// files, so resuming it is a retry, not a first attempt.
	StatusInterrupted = "interrupted"
)

// Item is one unit of work in a batch.
type Item struct {
	// ID is stable across resumes: index plus a hash of the item text, so
	// duplicate item strings stay distinguishable.
	ID    string `json:"id"`
	Index int    `json:"index"`
	Text  string `json:"text"`

	Status string `json:"status"`
	// Executions counts logical runs of this item across all resumes. A
	// single execution may contain several fallback attempts; those are
	// recorded in the ledger, not here.
	Executions   int    `json:"executions"`
	DelegationID string `json:"delegation_id,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	// FailureClass carries the last failure's classification so resume can
	// distinguish transient failures (retry automatically) from deterministic
	// ones (only with --retry-failed).
	FailureClass string `json:"failure_class,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// Batch is the on-disk record: frozen manifest fields plus mutable state.
type Batch struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`

	// --- Manifest: frozen at creation, never rewritten by resume ---

	// Template is the literal template text, stored so that resuming a batch
	// cannot silently pick up an edited template file.
	Template     string `json:"template"`
	TemplateHash string `json:"template_hash"`
	// TemplateSource records where the template came from, for humans.
	TemplateSource string `json:"template_source,omitempty"`
	WorkDir        string `json:"work_dir"`
	// WorkDirRepo is the git toplevel at creation time, if any. Resume
	// refuses to run against a different repository.
	WorkDirRepo string `json:"work_dir_repo,omitempty"`
	Agent       string `json:"agent,omitempty"` // empty = auto-route per item
	Isolation   string `json:"isolation,omitempty"`
	TimeoutSecs int    `json:"timeout_secs,omitempty"`
	CreatedAt   string `json:"created_at"`

	// --- Mutable state ---

	Items []Item `json:"items"`
	// Generation increments on each resume, so ledger entries can be
	// attributed to the run that produced them.
	Generation int `json:"generation"`
	// Owner identifies the process currently running this batch, so two
	// resumes cannot run the same batch concurrently.
	OwnerPID          int    `json:"owner_pid,omitempty"`
	OwnerPIDStartTime int64  `json:"owner_pid_start_time,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

// ItemID builds the stable identifier for an item. Duplicate item text is
// preserved rather than deduplicated — repeating an item may be intentional —
// so identity combines position with a content hash.
func ItemID(index int, text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%04d-%s", index, hex.EncodeToString(sum[:])[:8])
}

// HashTemplate returns the template fingerprint recorded in the manifest.
func HashTemplate(tmpl string) string {
	sum := sha256.Sum256([]byte(tmpl))
	return hex.EncodeToString(sum[:])[:16]
}

// New builds a batch from a template and item list. Items keep their given
// order and duplicates are preserved.
func New(id, template, templateSource, workDir, workDirRepo, agent, isolation string, timeoutSecs int, items []string) *Batch {
	b := &Batch{
		SchemaVersion:  SchemaVersion,
		ID:             id,
		Template:       template,
		TemplateHash:   HashTemplate(template),
		TemplateSource: templateSource,
		WorkDir:        workDir,
		WorkDirRepo:    workDirRepo,
		Agent:          agent,
		Isolation:      isolation,
		TimeoutSecs:    timeoutSecs,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	for i, text := range items {
		b.Items = append(b.Items, Item{
			ID:     ItemID(i, text),
			Index:  i,
			Text:   text,
			Status: StatusPending,
		})
	}
	return b
}

// Counts summarizes item statuses.
func (b *Batch) Counts() map[string]int {
	out := map[string]int{}
	for _, it := range b.Items {
		out[it.Status]++
	}
	return out
}

// Pending reports how many items still need work under the given retry policy.
func (b *Batch) Pending(retryFailed bool) int {
	n := 0
	for i := range b.Items {
		if b.Items[i].NeedsRun(retryFailed) {
			n++
		}
	}
	return n
}

// transientClasses are failure classes worth retrying automatically on a
// later resume. Deterministic failures are excluded: re-running a task that
// fails the same way every time only burns quota, which is what happened to
// scope A-BATCH-023 (five manual re-sends, never succeeded).
var transientClasses = map[string]bool{
	"timed_out":      true,
	"rate_limited":   true,
	"launch_failure": true,
}

// NeedsRun reports whether an item should be executed on this pass.
//
// Succeeded items are never re-run. Pending and interrupted items always
// run. Failed items run only if the failure was transient, or if the caller
// explicitly asked to retry deterministic failures too.
func (it *Item) NeedsRun(retryFailed bool) bool {
	switch it.Status {
	case StatusSucceeded:
		return false
	case StatusPending, StatusRunning, StatusInterrupted:
		// Running means a previous runner died holding this item.
		return true
	case StatusFailed:
		return retryFailed || transientClasses[it.FailureClass]
	default:
		return true
	}
}

// --- storage ---

var (
	dirOnce  sync.Once
	dirValue string
)

// Dir returns the batches directory.
func Dir() string {
	dirOnce.Do(func() {
		if home, err := os.UserHomeDir(); err == nil {
			dirValue = filepath.Join(home, ".config", "quancode", "batches")
		} else {
			dirValue = filepath.Join(".", ".quancode", "batches")
		}
	})
	return dirValue
}

// SetDirForTest overrides the batches directory. Test-only.
func SetDirForTest(dir string) {
	dirOnce.Do(func() {})
	dirValue = dir
}

// StatePath returns the on-disk path for a batch.
func StatePath(id string) string {
	return filepath.Join(Dir(), id+".json")
}

func lockPath(id string) string {
	return filepath.Join(Dir(), id+".lock")
}

// EnsureDir creates the batches directory.
func EnsureDir() error {
	return os.MkdirAll(Dir(), 0755)
}

// Save writes a batch atomically under an exclusive lock.
func Save(b *Batch) error {
	if err := EnsureDir(); err != nil {
		return fmt.Errorf("create batches dir: %w", err)
	}
	// The lock file is persistent: flock locks inodes, not paths, so deleting
	// it would let a concurrent writer lock a different inode.
	lf, err := os.OpenFile(lockPath(b.ID), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	return writeUnlocked(b)
}

// writeUnlocked serializes a batch atomically. Callers must hold the lock.
func writeUnlocked(b *Batch) error {
	b.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}
	path := StatePath(b.ID)
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write batch: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename batch: %w", err)
	}
	return nil
}

// Claim atomically takes ownership of a batch: it re-reads the current state
// under the lock, refuses if another live process owns it, then records this
// process as owner and bumps the generation.
//
// Checking ownership and writing it must happen under one lock. Doing the
// check in one call and the write in another lets two concurrent --resume
// invocations both observe OwnerPID=0 and then both run the same items.
func Claim(id string, pid int, pidStartTime int64, aliveFn func(int, int64) bool) (*Batch, error) {
	if err := EnsureDir(); err != nil {
		return nil, fmt.Errorf("create batches dir: %w", err)
	}
	lf, err := os.OpenFile(lockPath(id), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	b, err := loadUnlocked(id)
	if err != nil {
		return nil, err
	}
	if b.OwnerPID != 0 && b.OwnerPID != pid && aliveFn(b.OwnerPID, b.OwnerPIDStartTime) {
		return nil, fmt.Errorf("batch %s is already running (pid %d); wait for it to finish or kill it first", id, b.OwnerPID)
	}
	b.OwnerPID = pid
	b.OwnerPIDStartTime = pidStartTime
	b.Generation++
	if err := writeUnlocked(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Load reads a batch by ID.
func Load(id string) (*Batch, error) { return loadUnlocked(id) }

func loadUnlocked(id string) (*Batch, error) {
	data, err := os.ReadFile(StatePath(id))
	if err != nil {
		return nil, fmt.Errorf("read batch %s: %w", id, err)
	}
	var b Batch
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse batch %s: %w", id, err)
	}
	if b.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("batch %s was written by a newer quancode (schema %d > %d)", id, b.SchemaVersion, SchemaVersion)
	}
	return &b, nil
}

// List returns all batches, newest first.
func List(limit int) ([]*Batch, error) {
	paths, err := filepath.Glob(filepath.Join(Dir(), "*.json"))
	if err != nil {
		return nil, err
	}
	var out []*Batch
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".json")
		b, err := Load(id)
		if err != nil {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
