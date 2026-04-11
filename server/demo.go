package server

import (
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/qq418716640/quancode/ledger"
)

var demoTaskDescriptions = [...]string{
	"write unit tests for config parser module",
	"add integration tests for payment processing flow",
	"fix flaky test: TestWorkerPool_Shutdown",
	"write benchmarks for JSON serialization path",
	"add table-driven tests for URL routing logic",
	"refactor database connection pool to use context",
	"refactor error handling to use custom error types",
	"refactor file upload handler to stream to S3",
	"extract common middleware into shared package",
	"simplify retry logic in HTTP client",
	"implement pagination for /api/users endpoint",
	"add rate limiting middleware with sliding window",
	"implement webhook delivery with exponential backoff",
	"add health check endpoint with dependency probing",
	"implement circuit breaker for external API calls",
	"add graceful shutdown handler for worker processes",
	"implement cache invalidation for config reloads",
	"implement distributed locking with Redis",
	"fix race condition in concurrent map access",
	"fix timezone handling in scheduled task runner",
	"fix deadlock in worker pool shutdown sequence",
	"fix goroutine leak in WebSocket connection handler",
	"fix N+1 query in user profile loader",
	"fix panic on nil pointer in error middleware",
	"analyze memory allocation patterns in hot path",
	"review and optimize SQL queries in user service",
	"review security of session token generation",
	"compare approaches for config hot-reload mechanism",
	"add OpenTelemetry tracing to gRPC interceptors",
	"update protobuf definitions for v2 API",
	"add structured logging with slog package",
	"add database migration for multi-tenant support",
	"optimize bulk insert with batch operations",
	"add input validation for registration endpoint",
	"configure CORS headers for public API endpoints",
	"set up database connection retry with backoff",
	"migrate authentication to OAuth 2.0 PKCE flow",
	"add WebSocket support for real-time notifications",
	"implement request deduplication middleware",
	"add Prometheus metrics for HTTP handler latencies",
}

var demoAgentWeights = [...]struct {
	name   string
	weight int
}{
	{"claude", 35},
	{"codex", 25},
	{"gemini", 15},
	{"copilot", 15},
	{"qoder", 10},
}

var demoChangedFiles = [...]string{
	"api/handler.go", "api/middleware.go", "api/routes.go",
	"internal/service/user.go", "internal/service/payment.go",
	"internal/repository/postgres.go", "internal/repository/redis.go",
	"cmd/server/main.go", "pkg/auth/jwt.go", "pkg/retry/backoff.go",
	"config/config.go", "config/config_test.go",
	"internal/worker/pool.go", "internal/worker/pool_test.go",
	"api/handler_test.go", "internal/cache/cache.go",
	"migrations/0042_add_tenant_id.sql",
}

var demoAgentInfoList = []agentInfo{
	{Key: "claude", Name: "Claude Code", Enabled: true, Strengths: []string{"architecture", "complex-reasoning", "multi-file"}},
	{Key: "codex", Name: "Codex CLI", Enabled: true, Strengths: []string{"quick-edits", "code-generation", "test-writing"}},
	{Key: "copilot", Name: "GitHub Copilot CLI", Enabled: true, Strengths: []string{"code-generation", "github-integration", "repository-context"}},
	{Key: "gemini", Name: "Gemini CLI", Enabled: true, Strengths: []string{"large-context", "exploration", "explanation"}},
	{Key: "qoder", Name: "Qoder CLI", Enabled: true, Strengths: []string{"code-analysis", "debugging", "mcp-integration"}},
}

func generateDemoEntries() []ledger.Entry {
	rng := rand.New(rand.NewSource(42))
	const n = 538
	entries := make([]ledger.Entry, 0, n)
	now := time.Now()

	for i := 0; i < n; i++ {
		t := rng.Float64()
		hoursAgo := t * t * 14 * 24
		ts := now.Add(-time.Duration(hoursAgo * float64(time.Hour)))

		agent := pickDemoAgent(rng)
		task := demoTaskDescriptions[rng.Intn(len(demoTaskDescriptions))]

		var exitCode int
		var timedOut bool
		var durationMs int64

		roll := rng.Intn(100)
		switch {
		case roll < 82:
			durationMs = 30000 + rng.Int63n(150000)
		case roll < 94:
			exitCode = 1
			durationMs = 10000 + rng.Int63n(80000)
		default:
			timedOut = true
			exitCode = 1
			durationMs = 240000 + rng.Int63n(120000)
		}

		var isolation string
		switch rng.Intn(10) {
		case 0, 1, 2, 3:
			isolation = "worktree"
		case 4, 5:
			isolation = "patch"
		}

		var changedFiles []string
		if exitCode == 0 && rng.Intn(10) < 7 {
			nf := 1 + rng.Intn(4)
			for j := 0; j < nf; j++ {
				changedFiles = append(changedFiles, demoChangedFiles[rng.Intn(len(demoChangedFiles))])
			}
		}

		entries = append(entries, ledger.Entry{
			Timestamp:    ts.UTC().Format(time.RFC3339),
			Agent:        agent,
			Task:         task,
			ExitCode:     exitCode,
			TimedOut:     timedOut,
			DurationMs:   durationMs,
			ChangedFiles: changedFiles,
			Isolation:    isolation,
			WorkDir:      "/home/dev/myproject",
			DelegationID: fmt.Sprintf("del_%016x", rng.Int63()),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})
	return entries
}

func pickDemoAgent(rng *rand.Rand) string {
	total := 0
	for _, a := range demoAgentWeights {
		total += a.weight
	}
	roll := rng.Intn(total)
	cum := 0
	for _, a := range demoAgentWeights {
		cum += a.weight
		if roll < cum {
			return a.name
		}
	}
	return demoAgentWeights[len(demoAgentWeights)-1].name
}
