package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"text/template"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/batch"
	"github.com/qq418716640/quancode/config"
	qcontext "github.com/qq418716640/quancode/context"
	"github.com/qq418716640/quancode/health"
	"github.com/qq418716640/quancode/job"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/router"
	"github.com/qq418716640/quancode/runner"
	"github.com/spf13/cobra"
)

var (
	batchTemplate     string
	batchTemplateFile string
	batchItems        []string
	batchItemsFile    string
	batchResume       string
	batchRetryFailed  bool
	batchStopOnFail   bool
	batchDryRun       bool
	batchAgent        string
	batchWorkdir      string
	batchIsolation    string
	batchTimeout      int
	batchFormat       string
	batchList         bool
)

// maxRenderedTaskBytes bounds a single rendered task so a runaway template or
// a pathological item cannot produce a multi-megabyte prompt.
const maxRenderedTaskBytes = 256 * 1024

// BatchResult is the JSON shape for one `quancode batch` invocation.
type BatchResult struct {
	BatchID     string            `json:"batch_id"`
	Generation  int               `json:"generation"`
	WorkDir     string            `json:"work_dir"`
	Total       int               `json:"total"`
	Ran         int               `json:"ran"`
	Succeeded   int               `json:"succeeded"`
	Failed      int               `json:"failed"`
	Skipped     int               `json:"skipped"`
	Interrupted bool              `json:"interrupted,omitempty"`
	Counts      map[string]int    `json:"counts"`
	Items       []BatchItemResult `json:"items"`
}

// BatchItemResult is the per-item summary. Agent output is intentionally not
// included — a 1000-item batch would not fit in memory or in a useful JSON
// document. Use the delegation_id to fetch output from the ledger.
type BatchItemResult struct {
	ID           string `json:"id"`
	Index        int    `json:"index"`
	Item         string `json:"item"`
	Status       string `json:"status"`
	DelegationID string `json:"delegation_id,omitempty"`
	FailureClass string `json:"failure_class,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	Error        string `json:"error,omitempty"`
}

var batchCmd = &cobra.Command{
	Use:   "batch",
	Short: "Run one task template across a list of items, resumably",
	Long: `batch applies a single task template to many independent items, one delegation
per item, and remembers which ones succeeded so an interrupted or partially
failed batch can be resumed without redoing completed work.

The template is Go text/template with {{.Item}}, {{.Index}} (0-based),
{{.Number}} (1-based), and {{.Total}}.

The item list and the template text are frozen into a manifest at creation
time. Resuming uses the stored template, so editing the template file later
cannot silently change what a half-finished batch is doing — create a new
batch for that.

Execution is serial. Batch work is the heaviest thing QuanCode does, and
running items in parallel multiplies the rate at which a shared account hits
its quota, so concurrency is deliberately not offered yet.

Resume semantics:
  - succeeded            never re-run
  - pending/interrupted  always run
  - failed (transient)   re-run automatically (timeout, rate limit, launch failure)
  - failed (other)       skipped unless --retry-failed

Examples:
  quancode batch --template-file review.tmpl --items-file scopes.txt --dry-run
  quancode batch --template-file review.tmpl --items-file scopes.txt
  quancode batch --resume batch_7f3a2b1c
  quancode batch --resume batch_7f3a2b1c --retry-failed
  quancode batch --list`,
	RunE: runBatch,
}

func runBatch(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	if batchList {
		return listBatches()
	}
	if batchFormat != "text" && batchFormat != "json" {
		return fmt.Errorf("--format must be text or json")
	}

	b, err := loadOrCreateBatch(cmd, cfg)
	if err != nil {
		return err
	}

	if batchDryRun {
		return batchPreflight(b)
	}
	return executeBatch(cfg, b)
}

// loadOrCreateBatch resolves --resume into an existing batch, or builds a new
// one from the template and item flags.
func loadOrCreateBatch(cmd *cobra.Command, cfg *config.Config) (*batch.Batch, error) {
	if batchResume != "" {
		b, err := batch.Load(batchResume)
		if err != nil {
			return nil, err
		}
		if err := verifyResumeEnvironment(b); err != nil {
			return nil, err
		}
		return b, nil
	}

	tmplText, source, err := resolveBatchTemplate()
	if err != nil {
		return nil, err
	}
	items, err := resolveBatchItems()
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no items given; use --items-file or repeat --item")
	}

	workDir := batchWorkdir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	if fi, err := os.Stat(workDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("workdir %s is not a directory", workDir)
	}

	if batchAgent != "" {
		if _, ok := cfg.Agents[batchAgent]; !ok {
			return nil, fmt.Errorf("unknown agent: %s", batchAgent)
		}
	}
	// Validate isolation up front. An unrecognized value would otherwise pass
	// SupportsIsolation (which accepts anything when SupportedIsolations is
	// empty) and then run as inplace, so the user would believe they had
	// isolation while the agent edited the main working tree.
	switch batchIsolation {
	case "", "inplace", "worktree", "patch":
	default:
		return nil, fmt.Errorf("--isolation must be inplace, worktree, or patch (got %q)", batchIsolation)
	}

	id, err := newBatchID()
	if err != nil {
		return nil, err
	}
	b := batch.New(id, tmplText, source, workDir, gitToplevel(workDir),
		batchAgent, batchIsolation, batchTimeout, items)

	// Validate every item renders before committing the manifest — finding a
	// broken template on item 1 of 1000 is far better than on item 999.
	for i := range b.Items {
		if _, err := renderBatchTask(b, &b.Items[i]); err != nil {
			return nil, fmt.Errorf("item %d (%q): %w", i, truncate(b.Items[i].Text, 60), err)
		}
	}
	return b, nil
}

// verifyResumeEnvironment refuses to resume into a materially different place.
func verifyResumeEnvironment(b *batch.Batch) error {
	if fi, err := os.Stat(b.WorkDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("batch %s was created in %s, which no longer exists; re-create the batch or restore the directory", b.ID, b.WorkDir)
	}
	if b.WorkDirRepo != "" {
		now := gitToplevel(b.WorkDir)
		if now == "" {
			return fmt.Errorf("batch %s was created in git repo %s but %s is no longer inside a repository; refusing to resume",
				b.ID, b.WorkDirRepo, b.WorkDir)
		}
		if now != b.WorkDirRepo {
			return fmt.Errorf("batch %s was created in repo %s but %s now resolves to %s; refusing to resume",
				b.ID, b.WorkDirRepo, b.WorkDir, now)
		}
	}
	// Ownership is re-checked atomically in batch.Claim; this is only an
	// early, friendlier error before any work is set up.
	if b.OwnerPID != 0 && b.OwnerPID != os.Getpid() && job.IsProcessAlive(b.OwnerPID, b.OwnerPIDStartTime) {
		return fmt.Errorf("batch %s is already running (pid %d); wait for it to finish or kill it first", b.ID, b.OwnerPID)
	}
	return nil
}

// batchPreflight prints what would run without executing anything.
func batchPreflight(b *batch.Batch) error {
	pending := b.Pending(batchRetryFailed)
	fmt.Printf("batch:     %s\n", b.ID)
	fmt.Printf("template:  %s (hash %s)\n", orDash(b.TemplateSource), b.TemplateHash)
	fmt.Printf("workdir:   %s\n", b.WorkDir)
	fmt.Printf("agent:     %s\n", orAuto(b.Agent))
	fmt.Printf("items:     %d total, %d would run\n", len(b.Items), pending)
	if c := b.Counts(); len(c) > 0 {
		fmt.Printf("status:    %s\n", formatCounts(c))
	}

	// Show the first and last rendered task: enough to catch a broken
	// template without dumping a thousand prompts.
	show := func(label string, it *batch.Item) {
		rendered, err := renderBatchTask(b, it)
		if err != nil {
			fmt.Printf("\n--- %s (item %d) RENDER ERROR: %v\n", label, it.Index, err)
			return
		}
		fmt.Printf("\n--- %s (item %d, %s) ---\n%s\n", label, it.Index, it.ID, truncate(rendered, 800))
	}
	show("first", &b.Items[0])
	if len(b.Items) > 1 {
		show("last", &b.Items[len(b.Items)-1])
	}
	fmt.Printf("\nDry run — nothing executed. Remove --dry-run to start.\n")
	return nil
}

// executeBatch runs every item that needs running, serially.
func executeBatch(cfg *config.Config, b *batch.Batch) error {
	if b.Pending(batchRetryFailed) == 0 {
		fmt.Fprintf(os.Stderr, "[quancode] batch %s: nothing to do (%s)\n", b.ID, formatCounts(b.Counts()))
		return emitBatchResult(b, 0, false)
	}

	// A newly created batch exists only in memory — persist it before Claim,
	// which reads the authoritative copy from disk. Creation deliberately does
	// not persist, so --dry-run leaves no record behind.
	if _, err := os.Stat(batch.StatePath(b.ID)); os.IsNotExist(err) {
		if err := batch.Save(b); err != nil {
			return fmt.Errorf("create batch: %w", err)
		}
	}

	// Claim re-reads state, verifies no other live process owns the batch, and
	// records ownership — all under one lock, so two concurrent resumes cannot
	// both start. It returns the authoritative state, which may differ from
	// what we loaded a moment ago.
	pid := os.Getpid()
	pidStart, _ := job.GetProcessStartTime(pid)
	claimed, err := batch.Claim(b.ID, pid, pidStart, job.IsProcessAlive)
	if err != nil {
		return err
	}
	*b = *claimed
	// Release ownership on the way out so a later resume is not blocked by a
	// stale PID that happens to be reused.
	defer func() {
		b.OwnerPID = 0
		b.OwnerPIDStartTime = 0
		_ = batch.Save(b)
	}()

	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	// atomic: written by the signal goroutine, read by the main loop.
	var interrupted atomic.Bool
	go func() {
		select {
		case <-sigCh:
			interrupted.Store(true)
			fmt.Fprintln(os.Stderr, "\n[quancode] interrupt received — finishing the current item, then stopping. Resume with --resume "+b.ID)
			cancelAll()
		case <-ctx.Done():
		}
	}()

	total := b.Pending(batchRetryFailed)
	fmt.Fprintf(os.Stderr, "[quancode] ⚡ batch %s (gen %d): %d of %d items to run, serially\n",
		b.ID, b.Generation, total, len(b.Items))

	ran := 0
	for i := range b.Items {
		if ctx.Err() != nil {
			break
		}
		it := &b.Items[i]
		if !it.NeedsRun(batchRetryFailed) {
			continue
		}
		ran++
		fmt.Fprintf(os.Stderr, "[quancode] [%d/%d] item %d: %s\n", ran, total, it.Index, truncate(it.Text, 70))

		if err := runBatchItem(ctx, cfg, b, it); err != nil {
			fmt.Fprintf(os.Stderr, "[quancode] item %d error: %v\n", it.Index, err)
		}
		// Persist after every item so a crash loses at most one item's record.
		if err := batch.Save(b); err != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: could not save batch state: %v\n", err)
		}
		if batchStopOnFail && it.Status == batch.StatusFailed {
			fmt.Fprintf(os.Stderr, "[quancode] --stop-on-failure: halting after item %d\n", it.Index)
			break
		}
	}

	return emitBatchResult(b, ran, interrupted.Load())
}

// runBatchItem executes a single item as one logical delegation, reusing the
// normal attempt path so fallback, health, context injection, and ledger
// recording all behave exactly as they do for `quancode delegate`.
func runBatchItem(ctx context.Context, cfg *config.Config, b *batch.Batch, it *batch.Item) error {
	task, err := renderBatchTask(b, it)
	if err != nil {
		it.Status = batch.StatusFailed
		it.FailureClass = FailureClassTemplateError
		it.LastError = err.Error()
		it.UpdatedAt = nowRFC3339()
		return err
	}

	// Health is refreshed per item, not once per batch: a batch can run for
	// hours, and a snapshot taken at the start would be badly stale by the end.
	hs := health.NewSnapshot(cfg.Preferences.AgentHealth)

	agentKey := b.Agent
	initialProbeUsed := false
	if agentKey == "" {
		sel, probed, _ := router.SelectHealthy(cfg, task, nil, hs, true)
		initialProbeUsed = probed
		if sel == nil {
			it.Status = batch.StatusFailed
			// Clear any stale transient class, or NeedsRun would auto-retry
			// this item on every future resume.
			it.FailureClass = ""
			it.LastError = "no available agent"
			it.UpdatedAt = nowRFC3339()
			return fmt.Errorf("no available agent")
		}
		agentKey = sel.AgentKey
	} else if open, reason := hs.IsOpen(agentKey); open {
		fmt.Fprintf(os.Stderr, "[quancode] warning: %s is unhealthy (%s)\n", agentKey, reason)
	}

	ac, ok := cfg.Agents[agentKey]
	if !ok || !ac.Enabled {
		it.Status = batch.StatusFailed
		it.FailureClass = ""
		it.LastError = fmt.Sprintf("agent %s unavailable", agentKey)
		it.UpdatedAt = nowRFC3339()
		return fmt.Errorf("agent %s unavailable", agentKey)
	}

	isolation := b.Isolation
	if isolation == "" {
		isolation = ac.DefaultIsolation
	}
	if isolation == "" {
		isolation = cfg.Preferences.DefaultIsolation
	}
	if isolation == "" {
		isolation = "inplace"
	}
	if !ac.SupportsIsolation(isolation) {
		isolation = ac.FallbackIsolation()
	}

	runID, err := ledger.NewRunID()
	if err != nil {
		it.Status = batch.StatusFailed
		it.FailureClass = ""
		it.LastError = "generate run id: " + err.Error()
		it.UpdatedAt = nowRFC3339()
		return fmt.Errorf("generate run id: %w", err)
	}

	it.Status = batch.StatusRunning
	it.Executions++
	it.RunID = runID
	it.UpdatedAt = nowRFC3339()
	// Save before running so a crash mid-agent leaves the item marked running,
	// which resume treats as interrupted rather than never-started.
	_ = batch.Save(b)

	a := agent.FromConfig(agentKey, ac)
	// Context is rebuilt per agent inside the loop: agents can have different
	// Context rules, and a fallback agent must not inherit the first one's.
	buildCtx := func(key string) string {
		c := cfg.Agents[key]
		if bundle := qcontext.NewBuilder(cfg.ContextDefaults, c.Context).Build(b.WorkDir, nil, "", 0); bundle != nil {
			return qcontext.Format(bundle)
		}
		return ""
	}
	ctxPrefix := buildCtx(agentKey)

	// Respect preferences.fallback_mode: off — a user who disabled fallback
	// does not want batch quietly spending a second agent per item.
	fallbackOff := cfg.Preferences.FallbackMode == "off"
	fl := newFallbackLoop(cfg, task, agentKey, isolation, 0).withHealth(hs)
	if initialProbeUsed {
		fl.markProbeUsed()
	}
	meta := attemptMeta{RunID: runID, Attempt: 1, BatchID: b.ID, BatchItemID: it.ID}

	var ar attemptResult
	for {
		ar = runDelegateAttempt(DelegateAttemptOptions{
			Agent:                 a,
			AgentKey:              agentKey,
			Task:                  task,
			CtxPrefix:             ctxPrefix,
			WorkDir:               b.WorkDir,
			Isolation:             isolation,
			TimeoutOverride:       b.TimeoutSecs,
			MinTimeout:            cfg.Preferences.MinTimeoutSecs,
			Ctx:                   ctx,
			ContextDiffMode:       "",
			TaskSizeWarnThreshold: cfg.Preferences.EffectiveTaskSizeWarnThreshold(),
		})
		logAttempt(agentKey, task, b.WorkDir, isolation, meta, ar)

		if fallbackOff || !fl.shouldRetry(ar, meta.Attempt) || ctx.Err() != nil {
			break
		}
		// In inplace mode a failed agent may already have edited the tree.
		// Handing that half-done state to another agent produces duplicated or
		// conflicting edits, so stop the chain — same rule as `delegate`.
		if isolation == "inplace" {
			if !runner.IsGitRepo(b.WorkDir) {
				fmt.Fprintf(os.Stderr, "[quancode] item %d: %s failed in non-git directory — skipping fallback\n", it.Index, agentKey)
				break
			}
			if changes := detectNewChanges(b.WorkDir, ar.preSnapshot); len(changes) > 0 {
				fmt.Fprintf(os.Stderr, "[quancode] item %d: %s failed but modified files — skipping fallback\n", it.Index, agentKey)
				break
			}
		}
		nextKey, nextAgent, reason := fl.nextAgent()
		if nextAgent == nil {
			break
		}
		fmt.Fprintf(os.Stderr, "[quancode] item %d: %s %s, falling back to %s (%s)\n",
			it.Index, agentKey, ar.failureClass, nextKey, reason)
		meta = attemptMeta{
			RunID: runID, Attempt: meta.Attempt + 1,
			FallbackFrom: agentKey, FallbackReason: ar.failureClass,
			BatchID: b.ID, BatchItemID: it.ID,
		}
		agentKey, a = nextKey, nextAgent
		ctxPrefix = buildCtx(agentKey)
	}

	it.UpdatedAt = nowRFC3339()
	it.FailureClass = ar.failureClass
	if ar.result != nil {
		it.DelegationID = ar.result.DelegationID
		it.DurationMs = ar.result.DurationMs
	}

	switch {
	case ar.result != nil && ar.result.Cancelled:
		// This attempt was actually killed by the interrupt, so the item is
		// unfinished rather than failed. Checking ctx.Err() alone would
		// mislabel a deterministic failure that happened to finish just as
		// SIGINT arrived, and interrupted items always re-run.
		it.Status = batch.StatusInterrupted
		it.LastError = "interrupted"
	case ar.failureClass == "" && ar.err == nil:
		it.Status = batch.StatusSucceeded
		it.LastError = ""
	default:
		it.Status = batch.StatusFailed
		if ar.err != nil {
			it.LastError = truncate(ar.err.Error(), 500)
		} else {
			it.LastError = truncate(firstLine(ar.output), 500)
		}
	}
	return nil
}

// renderBatchTask renders the frozen template for one item.
func renderBatchTask(b *batch.Batch, it *batch.Item) (string, error) {
	// The data is a struct, not a map, so a typo like {{.Itme}} is already an
	// execute error ("can't evaluate field Itme") rather than a silent empty
	// string — which is what makes the preflight in loadOrCreateBatch able to
	// reject a bad template before a thousand prompts go out. missingkey is
	// set anyway to keep that guarantee if the data ever becomes a map.
	tmpl, err := template.New("batch").Option("missingkey=error").Parse(b.Template)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	data := struct {
		Item   string
		Index  int
		Number int
		Total  int
	}{Item: it.Text, Index: it.Index, Number: it.Index + 1, Total: len(b.Items)}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	if buf.Len() > maxRenderedTaskBytes {
		return "", fmt.Errorf("rendered task is %d bytes, over the %d-byte limit", buf.Len(), maxRenderedTaskBytes)
	}
	if buf.Len() == 0 {
		return "", fmt.Errorf("rendered task is empty")
	}
	return buf.String(), nil
}

func resolveBatchTemplate() (text, source string, err error) {
	if batchTemplate != "" && batchTemplateFile != "" {
		return "", "", fmt.Errorf("--template and --template-file are mutually exclusive")
	}
	if batchTemplate != "" {
		return batchTemplate, "(inline)", nil
	}
	if batchTemplateFile == "" {
		return "", "", fmt.Errorf("one of --template or --template-file is required")
	}
	data, err := os.ReadFile(batchTemplateFile)
	if err != nil {
		return "", "", fmt.Errorf("read template file: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return "", "", fmt.Errorf("template file %s is empty", batchTemplateFile)
	}
	abs, _ := filepath.Abs(batchTemplateFile)
	return string(data), abs, nil
}

// resolveBatchItems collects items from --item flags and --items-file.
//
// The file format is one item per line. Blank lines and lines starting with
// '#' are skipped, CRLF is tolerated, and surrounding whitespace is trimmed.
// Duplicates are preserved — repeating an item may well be intentional.
func resolveBatchItems() ([]string, error) {
	var items []string
	for _, it := range batchItems {
		if s := strings.TrimSpace(it); s != "" {
			items = append(items, s)
		}
	}
	if batchItemsFile == "" {
		return items, nil
	}
	data, err := os.ReadFile(batchItemsFile)
	if err != nil {
		return nil, fmt.Errorf("read items file: %w", err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("items file %s contains NUL bytes; expected a text file with one item per line", batchItemsFile)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		items = append(items, line)
	}
	return items, nil
}

func emitBatchResult(b *batch.Batch, ran int, interrupted bool) error {
	counts := b.Counts()
	res := BatchResult{
		BatchID:     b.ID,
		Generation:  b.Generation,
		WorkDir:     b.WorkDir,
		Total:       len(b.Items),
		Ran:         ran,
		Succeeded:   counts[batch.StatusSucceeded],
		Failed:      counts[batch.StatusFailed],
		Skipped:     counts[batch.StatusPending],
		Interrupted: interrupted,
		Counts:      counts,
	}
	for _, it := range b.Items {
		res.Items = append(res.Items, BatchItemResult{
			ID: it.ID, Index: it.Index, Item: it.Text, Status: it.Status,
			DelegationID: it.DelegationID, FailureClass: it.FailureClass,
			DurationMs: it.DurationMs, Error: it.LastError,
		})
	}

	if batchFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return err
		}
	} else {
		fmt.Printf("\nbatch %s (gen %d): %d/%d succeeded", b.ID, b.Generation, res.Succeeded, res.Total)
		if res.Failed > 0 {
			fmt.Printf(", %d failed", res.Failed)
		}
		if res.Skipped > 0 {
			fmt.Printf(", %d not run", res.Skipped)
		}
		fmt.Println()
		for _, it := range res.Items {
			if it.Status == batch.StatusFailed || it.Status == batch.StatusInterrupted {
				fmt.Printf("  %-8s item %d %s — %s\n", it.Status, it.Index, truncate(it.Item, 50), truncate(it.Error, 80))
			}
		}
		if res.Failed > 0 || res.Skipped > 0 || interrupted {
			fmt.Printf("\nresume with: quancode batch --resume %s\n", b.ID)
		}
	}

	if interrupted {
		return fmt.Errorf("batch interrupted")
	}
	if res.Failed > 0 {
		return fmt.Errorf("%d of %d items failed", res.Failed, res.Total)
	}
	return nil
}

func listBatches() error {
	batches, err := batch.List(20)
	if err != nil {
		return err
	}
	if len(batches) == 0 {
		fmt.Println("no batches yet")
		return nil
	}
	for _, b := range batches {
		fmt.Printf("%s  %s  %d items  %s  %s\n",
			b.ID, b.CreatedAt, len(b.Items), formatCounts(b.Counts()), truncate(b.WorkDir, 40))
	}
	return nil
}

// --- small helpers ---

func newBatchID() (string, error) {
	id, err := ledger.NewRunID()
	if err != nil {
		return "", fmt.Errorf("generate batch id: %w", err)
	}
	return "batch_" + strings.TrimPrefix(id, "run_"), nil
}

func gitToplevel(dir string) string {
	if !runner.IsGitRepo(dir) {
		return ""
	}
	out, err := runner.GitToplevel(dir)
	if err != nil {
		return ""
	}
	return out
}

func formatCounts(c map[string]int) string {
	var parts []string
	for _, k := range []string{batch.StatusSucceeded, batch.StatusFailed, batch.StatusInterrupted, batch.StatusRunning, batch.StatusPending} {
		if c[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[k], k))
		}
	}
	if len(parts) == 0 {
		return "empty"
	}
	return strings.Join(parts, ", ")
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orAuto(s string) string {
	if s == "" {
		return "(auto-route)"
	}
	return s
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func init() {
	batchCmd.Flags().StringVar(&batchTemplate, "template", "", "inline task template (Go text/template)")
	batchCmd.Flags().StringVar(&batchTemplateFile, "template-file", "", "file containing the task template")
	batchCmd.Flags().StringArrayVar(&batchItems, "item", nil, "an item (repeatable)")
	batchCmd.Flags().StringVar(&batchItemsFile, "items-file", "", "file with one item per line (# comments allowed)")
	batchCmd.Flags().StringVar(&batchResume, "resume", "", "resume an existing batch by id")
	batchCmd.Flags().BoolVar(&batchRetryFailed, "retry-failed", false, "also re-run items that failed for non-transient reasons")
	batchCmd.Flags().BoolVar(&batchStopOnFail, "stop-on-failure", false, "stop the batch at the first failed item")
	batchCmd.Flags().BoolVar(&batchDryRun, "dry-run", false, "validate and show what would run, without executing")
	batchCmd.Flags().StringVar(&batchAgent, "agent", "", "agent for every item (default: auto-route per item)")
	batchCmd.Flags().StringVar(&batchWorkdir, "workdir", "", "working directory (default: cwd)")
	batchCmd.Flags().StringVar(&batchIsolation, "isolation", "", "isolation mode: inplace, worktree, patch")
	batchCmd.Flags().IntVar(&batchTimeout, "timeout", 0, "per-item timeout in seconds")
	batchCmd.Flags().StringVar(&batchFormat, "format", "text", "output format: text or json")
	batchCmd.Flags().BoolVar(&batchList, "list", false, "list recent batches")
	rootCmd.AddCommand(batchCmd)
}
