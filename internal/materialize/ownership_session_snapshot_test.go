package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestReconcileOwnedJSONAggregatesLoadsOwnershipOnceAcrossWrites(t *testing.T) {
	root := t.TempDir()
	stateRoot := t.TempDir()
	loads := 0
	store, err := ownership.Open(root, ownership.WithStateRoot(stateRoot), ownership.WithProofLoadObserver(func() { loads++ }))
	if err != nil {
		t.Fatal(err)
	}
	files := []ownedJSONFileSpec{
		{rel: "first.json", container: "mcpServers", keyed: true},
		{rel: "second.json", container: "mcpServers", keyed: true},
	}
	initial := []byte(`{"mcpServers":{"old":{"command":"old"}}}`)
	old := ownership.Entry{
		Kind: ownership.KindMcpServer, Container: "mcpServers", Key: "old",
		IdentityDigest: ownership.McpIdentityDigest("old"),
		ValueDigest:    ownership.Digest([]byte(`{"command":"old"}`)),
		Resource:       "old-resource",
	}
	for i, file := range files {
		abs := filepath.Join(root, file.rel)
		if err := os.WriteFile(abs, initial, 0o644); err != nil {
			t.Fatal(err)
		}
		generation := uint64(i)
		if _, err := store.Apply(ownership.Mutation{
			Rel: file.rel, AbsFile: abs, NextBytes: initial, Entries: []ownership.Entry{old},
			Reason: ownership.ReasonSync, ExpectPriorGeneration: generation,
			ExpectPriorDigest: ownership.Digest(initial),
		}); err != nil {
			t.Fatalf("seed %s: %v", file.rel, err)
		}
	}
	work := &ownedJSONAggregateWork{}
	keys := []string{"next-first", "next-second"}
	for i, file := range files {
		key := keys[i]
		raw := []byte(`{"command":"` + key + `"}`)
		work.add(ownedJSONEntry{
			rel: file.rel, kind: ownership.KindMcpServer, container: "mcpServers",
			key: key, raw: raw, identity: ownership.McpIdentityDigest(key), resource: key,
		})
	}
	loads = 0
	options := codexHookJSONOptions()
	options.store, options.sessionName = store, "load-count-test"
	report := reconcileOwnedJSONAggregates(context.Background(), &Report{PerTarget: make(map[hcc.Target]TargetReport)}, outputKey{target: hcc.TargetCodex}, root, files, work, options, false)
	requireNoErrorDiags(t, report.Diags)
	if loads != 1 {
		t.Fatalf("Proof-path state loads across two aggregate writes = %d, want 1", loads)
	}
	if len(report.PerRoot) != 2 {
		t.Fatalf("aggregate writes = %d, want 2", len(report.PerRoot))
	}
}
func TestCodexRootRecoversOnceAcrossHookAndMCPReconciles(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	hooksPath := filepath.Join(repo, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	hooks := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo existing"}]}]}}`)
	if err := os.WriteFile(hooksPath, hooks, 0o644); err != nil {
		t.Fatal(err)
	}
	recoveries := make(map[string]int)
	ownedAggregateRecoverHook = func(rootKey string) { recoveries[rootKey]++ }
	t.Cleanup(func() { ownedAggregateRecoverHook = nil })

	resolved := []resolver.ResolvedArtifact{
		codexResource("managed-mcp", ir.NewMcpServer("managed-mcp", "managed-command", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
	}
	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)
	if len(recoveries) != 1 {
		t.Fatalf("Recover root keys = %#v, want one root", recoveries)
	}
	for rootKey, calls := range recoveries {
		if calls != 1 {
			t.Fatalf("Recover calls for root %q = %d, want 1", rootKey, calls)
		}
	}
	gotHooks, err := os.ReadFile(hooksPath)
	if err != nil || !bytes.Equal(gotHooks, hooks) {
		t.Fatalf("unowned Codex hooks changed: %s / %v", gotHooks, err)
	}
	gotMCP, err := os.ReadFile(filepath.Join(repo, ".codex", "config.toml"))
	if err != nil || !bytes.Contains(gotMCP, []byte("managed-mcp")) || !bytes.Contains(gotMCP, []byte("managed-command")) {
		t.Fatalf("Codex MCP output = %s / %v", gotMCP, err)
	}
}

// The expected counts are the diagnostics the base (per-reconciler session)
// code reports: each reconciler reports a failure only when it has work, so a
// root with no Codex hook work must not gain a hook-coded "root skipped"
// diagnostic, and a root with no MCP work must not gain an MCP diagnostic.
func TestCodexLockFailureKeepsHookAndMCPDiagnostics(t *testing.T) {
	tests := []struct {
		name, failure string
		hooks, mcp    bool
		want          map[string]int
	}{
		{name: "lock-timeout", failure: "lock-timeout", hooks: true, mcp: true, want: map[string]int{diag.CodeOwnershipLockTimeout: 1, diag.CodeMaterializeCodexAggregateMalformed: 1}},
		{name: "open-failure", failure: "open-failure", hooks: true, mcp: true, want: map[string]int{diag.CodeMaterializeCodexAggregateMalformed: 2}},
		{name: "recover-failure", failure: "recover-failure", hooks: true, mcp: true, want: map[string]int{diag.CodeMaterializeCodexAggregateMalformed: 1, diag.CodeOwnershipStateCorrupt: 1}},
		{name: "lock-timeout-mcp-only", failure: "lock-timeout", mcp: true, want: map[string]int{diag.CodeMaterializeCodexAggregateMalformed: 1}},
		{name: "open-failure-mcp-only", failure: "open-failure", mcp: true, want: map[string]int{diag.CodeMaterializeCodexAggregateMalformed: 1}},
		{name: "recover-failure-mcp-only", failure: "recover-failure", mcp: true, want: map[string]int{diag.CodeMaterializeCodexAggregateMalformed: 1}},
		{name: "lock-timeout-hooks-only", failure: "lock-timeout", hooks: true, want: map[string]int{diag.CodeOwnershipLockTimeout: 1}},
		{name: "recover-failure-hooks-only", failure: "recover-failure", hooks: true, want: map[string]int{diag.CodeOwnershipStateCorrupt: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, home := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := filepath.Join(repo, ".codex")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.hooks {
				hooks := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo existing"}]}]}}`)
				if err := os.WriteFile(filepath.Join(root, "hooks.json"), hooks, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			resolved := []resolver.ResolvedArtifact{codexSubagent("reviewer", "Review carefully.", config.RepoCommitted)}
			if tt.mcp {
				resolved = []resolver.ResolvedArtifact{
					codexResource("managed-mcp", ir.NewMcpServer("managed-mcp", "managed-command", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
				}
			}
			switch tt.failure {
			case "lock-timeout":
				holder, err := ownership.Open(root)
				if err != nil {
					t.Fatal(err)
				}
				release, err := holder.Lock()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(release)
				t.Setenv(ownership.LockTimeoutEnvVar, "0")
			case "open-failure":
				stateFile := filepath.Join(t.TempDir(), "state")
				if err := os.WriteFile(stateFile, nil, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Setenv("XDG_STATE_HOME", stateFile)
			case "recover-failure":
				store, err := ownership.Open(root)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(store.JournalFile()), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(store.JournalFile(), []byte("{not json"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			report, err := Run(context.Background(), repo, resolved)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := make(map[string]int)
			for _, diagnostic := range report.Diags {
				got[diagnostic.Code]++
				if diagnostic.Severity != diag.SeverityError {
					t.Errorf("diagnostic %q severity = %q, want error", diagnostic.Code, diagnostic.Severity)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("diagnostic codes = %#v, want %#v", got, tt.want)
			}
			for code, count := range tt.want {
				if got[code] != count {
					t.Errorf("diagnostic %q count = %d, want %d", code, got[code], count)
				}
			}
		})
	}
}
