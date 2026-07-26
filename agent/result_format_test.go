package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/runner"
)

// Captured verbatim from `claude --dangerously-skip-permissions -p
// --output-format json "reply with exactly: OK"` on claude-code 2.1.219,
// trimmed to the fields QuanCode reads plus enough neighbours to prove the
// decoder tolerates unknown ones.
const claudeEnvelope = `{"is_error":false,"duration_api_ms":5498,"num_turns":1,` +
	`"session_id":"6a8cca39-64b0-4e82-b22d-7e9b0c9a3dfd","total_cost_usd":0.0780975,` +
	`"usage":{"input_tokens":2,"cache_creation_input_tokens":6977,` +
	`"cache_read_input_tokens":15273,"output_tokens":4,"service_tier":"standard"},` +
	`"subtype":"success","api_error_status":null,"result":"OK","type":"result"}`

// Captured verbatim from `codex exec --sandbox workspace-write --ephemeral
// --json "reply with exactly: OK"` on codex-cli 0.145.0.
const codexEvents = `{"type":"thread.started","thread_id":"019f9e99-2149-7833-822f-a2b928a3e778"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}
{"type":"turn.completed","usage":{"input_tokens":21064,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}
`

func TestApplyClaudeJSONResult(t *testing.T) {
	r := &runner.Result{Stdout: claudeEnvelope}
	applyClaudeJSONResult(r)

	if r.Stdout != "OK" {
		t.Errorf("Stdout = %q, want %q", r.Stdout, "OK")
	}
	if r.CostUSD == nil || *r.CostUSD != 0.0780975 {
		t.Errorf("CostUSD = %v, want 0.0780975", r.CostUSD)
	}
	// 2 uncached + 6977 cache-write + 15273 cache-read. Recording the bare
	// input_tokens=2 would understate the turn by four orders of magnitude.
	if r.TokensIn == nil || *r.TokensIn != 22252 {
		t.Errorf("TokensIn = %v, want 22252", r.TokensIn)
	}
	if r.TokensOut == nil || *r.TokensOut != 4 {
		t.Errorf("TokensOut = %v, want 4", r.TokensOut)
	}
	if r.AgentSessionID != "6a8cca39-64b0-4e82-b22d-7e9b0c9a3dfd" {
		t.Errorf("AgentSessionID = %q", r.AgentSessionID)
	}
	if r.Transient || r.AgentFault {
		t.Error("a successful envelope must not be marked transient or agent-fault")
	}
}

func TestApplyClaudeJSONResultRateLimited(t *testing.T) {
	r := &runner.Result{
		ExitCode: 1,
		Stdout:   `{"is_error":true,"subtype":"error","api_error_status":429,"result":"rate limited","total_cost_usd":0.01}`,
	}
	applyClaudeJSONResult(r)

	if !r.Transient {
		t.Error("HTTP 429 must be transient so fallback fires")
	}
	if !r.AgentFault {
		t.Error("HTTP 429 must count against agent health")
	}
	if len(r.MatchedHints) != 1 || !strings.Contains(r.MatchedHints[0], "429") {
		t.Errorf("MatchedHints = %v, want one hint mentioning 429", r.MatchedHints)
	}
	// Same sentence as the text-pattern path, so the two never double up.
	if r.MatchedHints[0] != config.MatchFailure("too many requests", nil).Hints[0] {
		t.Errorf("429 hint %q diverged from the pattern table's wording", r.MatchedHints[0])
	}
	// A failed turn still burned money; that is the case most worth recording.
	if r.CostUSD == nil || *r.CostUSD != 0.01 {
		t.Errorf("CostUSD = %v, want 0.01 on the failure path", r.CostUSD)
	}
}

// Unwrapping must not hide failure evidence: the parts of the envelope that
// .result omits are exactly where a rate-limit or auth message can live, and
// applyDiagnosticHints runs after the rewrite.
func TestUnwrappingPreservesFailureEvidenceForHints(t *testing.T) {
	a := &genericAgent{cfg: config.AgentConfig{
		ResultFormat: "json_object",
		DiagnosticHints: []config.DiagnosticHint{
			{Pattern: "usage limit reached", Hint: "quota exhausted"},
		},
	}}
	r := &runner.Result{
		ExitCode: 1,
		Stdout:   `{"is_error":true,"subtype":"error_during_execution","result":"see details","error_detail":"usage limit reached"}`,
	}
	a.applyResultFormat(r)
	a.applyDiagnosticHints(r, nil)

	if r.Stdout != "see details" {
		t.Errorf("Stdout = %q, want the unwrapped answer", r.Stdout)
	}
	if !slices.Contains(r.MatchedHints, "quota exhausted") {
		t.Errorf("MatchedHints = %v, want the hint matched via RawStdout", r.MatchedHints)
	}
}

// The 429 shortcut and the text-pattern path can both fire on one failure;
// the user must not see the same sentence twice.
func TestDiagnosticHintsAreNotDuplicated(t *testing.T) {
	const hint = "already recorded"
	a := &genericAgent{cfg: config.AgentConfig{
		DiagnosticHints: []config.DiagnosticHint{{Pattern: "boom", Hint: hint}},
	}}
	r := &runner.Result{ExitCode: 1, Stdout: "boom", MatchedHints: []string{hint}}
	a.applyDiagnosticHints(r, nil)

	if got := len(r.MatchedHints); got != 1 {
		t.Errorf("MatchedHints = %v, want the duplicate suppressed", r.MatchedHints)
	}
}

// A status-only envelope matches no text pattern, so if the shortcut's hint
// were not carried through applyDiagnosticHints the terminal would show a
// fallback happening with no explanation.
func TestStatusOnlyRateLimitHintSurvivesHintPass(t *testing.T) {
	a := &genericAgent{cfg: config.AgentConfig{ResultFormat: "json_object"}}
	r := &runner.Result{ExitCode: 1, Stdout: `{"api_error_status":429}`}
	a.applyResultFormat(r)
	a.applyDiagnosticHints(r, nil)

	if len(r.MatchedHints) != 1 || !strings.Contains(r.MatchedHints[0], "429") {
		t.Errorf("MatchedHints = %v, want the 429 hint retained", r.MatchedHints)
	}
}

// An empty .result is not an answer; swapping it in would leave the
// delegation with no output whatsoever.
func TestApplyClaudeJSONResultKeepsEnvelopeOnEmptyResult(t *testing.T) {
	const envelope = `{"is_error":true,"subtype":"error","result":"","api_error_status":500}`
	r := &runner.Result{Stdout: envelope}
	applyClaudeJSONResult(r)

	if r.Stdout != envelope {
		t.Errorf("Stdout = %q, want the envelope preserved", r.Stdout)
	}
}

// A minimal error envelope carries no result/usage/cost/session, but its
// status code is still the thing fallback needs to see.
func TestApplyClaudeJSONResultRecognizesStatusOnlyEnvelope(t *testing.T) {
	r := &runner.Result{ExitCode: 1, Stdout: `{"api_error_status":429}`}
	applyClaudeJSONResult(r)

	if !r.Transient || !r.AgentFault {
		t.Error("a status-only 429 envelope must still be classified")
	}
}

func TestApplyCodexJSONLEventsPreservesRawStream(t *testing.T) {
	raw := codexEvents
	r := &runner.Result{Stdout: raw}
	applyCodexJSONLEvents(r)

	if r.RawStdout != raw {
		t.Errorf("RawStdout = %q, want the original JSONL", r.RawStdout)
	}
}

func TestApplyClaudeJSONResultKeepsEnvelopeWithoutResultField(t *testing.T) {
	// Some subtypes omit .result entirely. Blanking Stdout would throw away
	// the only text captured, so the raw envelope is left in place.
	const noResult = `{"is_error":true,"subtype":"error_max_turns","total_cost_usd":0.5}`
	r := &runner.Result{Stdout: noResult}
	applyClaudeJSONResult(r)

	if r.Stdout != noResult {
		t.Errorf("Stdout = %q, want the envelope preserved", r.Stdout)
	}
	if r.CostUSD == nil || *r.CostUSD != 0.5 {
		t.Errorf("CostUSD = %v, want 0.5", r.CostUSD)
	}
}

func TestApplyCodexJSONLEvents(t *testing.T) {
	r := &runner.Result{Stdout: codexEvents}
	applyCodexJSONLEvents(r)

	if r.Stdout != "OK" {
		t.Errorf("Stdout = %q, want %q", r.Stdout, "OK")
	}
	if r.TokensIn == nil || *r.TokensIn != 21064 {
		t.Errorf("TokensIn = %v, want 21064", r.TokensIn)
	}
	if r.TokensOut == nil || *r.TokensOut != 5 {
		t.Errorf("TokensOut = %v, want 5", r.TokensOut)
	}
	if r.AgentSessionID != "019f9e99-2149-7833-822f-a2b928a3e778" {
		t.Errorf("AgentSessionID = %q", r.AgentSessionID)
	}
	// Codex bills the subscription, not the call — no dollar figure exists.
	if r.CostUSD != nil {
		t.Errorf("CostUSD = %v, want nil (codex reports no cost)", r.CostUSD)
	}
}

func TestApplyCodexJSONLEventsLastMessageWins(t *testing.T) {
	r := &runner.Result{Stdout: `{"type":"item.completed","item":{"type":"agent_message","text":"first"}}
{"type":"item.completed","item":{"type":"reasoning","text":"thinking out loud"}}
{"type":"item.completed","item":{"type":"agent_message","text":"final"}}`}
	applyCodexJSONLEvents(r)

	if r.Stdout != "final" {
		t.Errorf("Stdout = %q, want the last agent_message", r.Stdout)
	}
}

// A truncated stream (process killed mid-turn) must still yield the text
// codex managed to emit, and must not invent usage numbers.
func TestApplyCodexJSONLEventsTruncated(t *testing.T) {
	r := &runner.Result{Stdout: `{"type":"thread.started","thread_id":"t1"}
{"type":"item.completed","item":{"type":"agent_message","text":"partial"}}
{"type":"turn.comple`}
	applyCodexJSONLEvents(r)

	if r.Stdout != "partial" {
		t.Errorf("Stdout = %q, want %q", r.Stdout, "partial")
	}
	if r.TokensIn != nil || r.TokensOut != nil {
		t.Error("a truncated stream must leave token counts unreported")
	}
}

// The whole safety argument for opting an agent into a ResultFormat rests on
// this: if the output is not in that format, nothing is touched.
func TestApplyResultFormatNoOpOnUnparseableOutput(t *testing.T) {
	cases := []struct {
		format string
		stdout string
	}{
		{"json_object", "just plain text\nfrom a CLI that ignored --output-format"},
		{"json_object", ""},
		{"json_object", `{"unrelated":"json"}`},
		{"jsonl_events", "plain text output\nsecond line"},
		{"jsonl_events", ""},
		{"jsonl_events", `{"type":"something.else"}`},
		{"", claudeEnvelope}, // no opt-in: envelope stays raw
		{"unknown_format", claudeEnvelope},
	}

	for _, tc := range cases {
		a := &genericAgent{cfg: config.AgentConfig{ResultFormat: tc.format}}
		r := &runner.Result{Stdout: tc.stdout}
		a.applyResultFormat(r)

		if r.Stdout != tc.stdout {
			t.Errorf("format %q: Stdout mutated to %q, want %q", tc.format, r.Stdout, tc.stdout)
		}
		if r.CostUSD != nil || r.TokensIn != nil || r.TokensOut != nil || r.AgentSessionID != "" {
			t.Errorf("format %q: metadata invented from unparseable output", tc.format)
		}
	}
}

func TestApplyResultFormatNilResult(t *testing.T) {
	a := &genericAgent{cfg: config.AgentConfig{ResultFormat: "json_object"}}
	a.applyResultFormat(nil) // a launch failure yields no Result at all
}

func TestSumTokens(t *testing.T) {
	i := func(v int64) *int64 { return &v }

	if got := sumTokens(nil, nil); got != nil {
		t.Errorf("sumTokens(nil, nil) = %v, want nil", got)
	}
	if got := sumTokens(i(0)); got == nil || *got != 0 {
		t.Errorf("a reported zero must stay reported, got %v", got)
	}
	if got := sumTokens(i(2), nil, i(40)); got == nil || *got != 42 {
		t.Errorf("sumTokens(2, nil, 40) = %v, want 42", got)
	}
}
