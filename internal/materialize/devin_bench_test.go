package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// Devin aggregate reconciliation is on the `sync` latency path for every user who
// selects the harness, so per repository policy it earns a benchmark the first
// time it is written rather than the first time it is optimized.

// devinBenchEnv isolates HOME, the cache, and the ownership state root for a
// benchmark iteration.
func devinBenchEnv(b *testing.B) string {
	b.Helper()
	root := filepath.Join(b.TempDir(), ".devin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		b.Fatal(err)
	}
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_STATE_HOME", filepath.Join(b.TempDir(), "state"))
	b.Setenv("XDG_CACHE_HOME", filepath.Join(b.TempDir(), "cache"))
	return root
}

// devinBenchWork compiles n hooks and n MCP servers into desired aggregate work,
// so the measurement covers the reconciler rather than the compilers.
func devinBenchWork(b *testing.B, n int, machineScope bool) *ownedJSONAggregateWork {
	b.Helper()
	work := &ownedJSONAggregateWork{}
	opts := []hcc.Option{}
	if machineScope {
		opts = append(opts, hcc.WithScope(hcc.ScopeUser))
	}
	for i := 0; i < n; i++ {
		name := "bench-hook-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		hook := ir.NewHook(name, "PostToolUse", "echo "+name, "command", nil, ir.SourceRef{})
		result, err := hcc.Compile(hook, hcc.TargetDevin, opts...)
		if err != nil {
			b.Fatal(err)
		}
		entries, err := devinFragmentEntries(result.Artifacts[0].Path, result.Artifacts[0].Bytes, machineScope, name)
		if err != nil {
			b.Fatal(err)
		}
		for _, entry := range entries {
			work.add(entry)
		}

		server := "bench-mcp-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		mcp := ir.NewMcpServer(server, "bin", []string{"--flag"}, map[string]string{"K": "V"}, nil, ir.SourceRef{})
		mcpResult, err := hcc.Compile(mcp, hcc.TargetDevin, opts...)
		if err != nil {
			b.Fatal(err)
		}
		mcpEntries, err := devinFragmentEntries(mcpResult.Artifacts[0].Path, mcpResult.Artifacts[0].Bytes, machineScope, server)
		if err != nil {
			b.Fatal(err)
		}
		for _, entry := range mcpEntries {
			work.add(entry)
		}
	}
	return work
}

// BenchmarkDevinAggregateReconcileFirstRun measures the cold path: absent
// aggregates, no ownership state, so every entry is created and one journaled
// generation per file is committed.
func BenchmarkDevinAggregateReconcileFirstRun(b *testing.B) {
	discardBenchLogs(b)
	root := devinBenchEnv(b)
	work := devinBenchWork(b, 24, false)
	key := outputKey{target: hcc.TargetDevin}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for _, rel := range []string{harness.DevinProjectHooksFile, harness.DevinMcpConfigFile} {
			_ = os.Remove(filepath.Join(root, rel))
		}
		stateRoot, err := ownership.StateRoot()
		if err != nil {
			b.Fatal(err)
		}
		_ = os.RemoveAll(stateRoot)
		report := &Report{PerTarget: map[hcc.Target]TargetReport{}}
		b.StartTimer()
		reconcileDevinAggregates(context.Background(), report, key, root, work, false)
	}
}

// BenchmarkDevinAggregateReconcileIdempotent measures the warm path every
// subsequent `sync` takes: parse, classify every entry against proof, discover
// nothing changed, and write nothing. This is the one that must stay cheap.
func BenchmarkDevinAggregateReconcileIdempotent(b *testing.B) {
	discardBenchLogs(b)
	root := devinBenchEnv(b)
	work := devinBenchWork(b, 24, false)
	key := outputKey{target: hcc.TargetDevin}
	reconcileDevinAggregates(context.Background(), &Report{PerTarget: map[hcc.Target]TargetReport{}}, key, root, work, false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := &Report{PerTarget: map[hcc.Target]TargetReport{}}
		reconcileDevinAggregates(context.Background(), report, key, root, work, false)
		if len(report.PerRoot) != 0 {
			b.Fatalf("idempotent reconciliation wrote %d files", len(report.PerRoot))
		}
	}
}

// BenchmarkDevinHookIdentity isolates the behaviour-tuple digest, which runs once
// per desired hook and once per existing hook element on every sync.
func BenchmarkDevinHookIdentity(b *testing.B) {
	rule := []byte(`{"command":"./scripts/check.sh --strict","matcher":"Bash","timeout":30,"type":"command","x-extra":{"nested":[1,2,3]}}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ownedJSONHookIdentity("PostToolUse", rule); err != nil {
			b.Fatal(err)
		}
	}
}
