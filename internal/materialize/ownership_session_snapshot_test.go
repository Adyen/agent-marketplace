package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
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
	canonicalRoot, err := ownership.CanonicalRoot(filepath.Join(repo, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	wantRootKey := ownership.RootKey(canonicalRoot)
	recoveries := make(map[string]int)
	ownedAggregateRecoverHook = func(rootKey string) { recoveries[rootKey]++ }
	t.Cleanup(func() { ownedAggregateRecoverHook = nil })

	resolved := []resolver.ResolvedArtifact{
		codexHookResolved(t, "managed-hook", ir.NewHookWithMatcher("managed-hook", "PreToolUse", "Bash", "echo managed", "command", nil, ir.SourceRef{}), config.RepoCommitted),
		codexResource("managed-mcp", ir.NewMcpServer("managed-mcp", "managed-command", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
	}
	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)
	if len(recoveries) != 1 || recoveries[wantRootKey] != 1 {
		t.Fatalf("Recover calls per root = %#v, want one Recover for %q", recoveries, wantRootKey)
	}
}
