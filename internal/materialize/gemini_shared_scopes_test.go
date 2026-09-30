package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func sharedGeminiResources() []resolver.ResolvedArtifact {
	var desired []resolver.ResolvedArtifact
	for _, scope := range []struct {
		name   string
		source config.Source
	}{{"repository", config.RepoCommitted}, {"machine", config.UserPersonal}} {
		for _, resource := range []ir.Resource{
			ir.NewMcpServer(scope.name+"-mcp", scope.name+"-tool", nil, nil, nil, ir.SourceRef{}),
			ir.NewHook(scope.name+"-audit", "PostToolUse", "echo "+scope.name+"-audit", "command", nil, ir.SourceRef{}),
		} {
			desired = append(desired, resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: resource.Kind(), Name: resource.Name(), Resource: resource}, Source: scope.source, Harnesses: []hcc.Target{hcc.TargetGemini}})
		}
	}
	return desired
}

func configureSharedGeminiHome(tb testing.TB) string {
	tb.Helper()
	home := tb.TempDir()
	tb.Setenv("HOME", home)
	tb.Setenv("XDG_STATE_HOME", tb.TempDir())
	tb.Setenv("XDG_CACHE_HOME", tb.TempDir())
	tb.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	return home
}

// spec: owned-entry-reconciliation/gemini-shared-settings-retain-both-scopes
func TestGeminiSharedScopeFileKeepsBothScopes(t *testing.T) {
	home := configureSharedGeminiHome(t)
	root := filepath.Join(home, ".gemini")
	file := filepath.Join(root, "settings.json")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  // handwritten setting\n  \"theme\": \"manual\",\n  \"mcpServers\": {\"handwritten\": {\"command\": \"manual-tool\"}}\n}\n")
	if err := os.WriteFile(file, original, 0o640); err != nil {
		t.Fatal(err)
	}
	desired := sharedGeminiResources()
	report, err := Run(context.Background(), home, desired)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("sync failed: %v %+v", err, report)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	settings := nativeMCPDocument(t, before)
	servers := settings["mcpServers"].(map[string]any)
	for _, name := range []string{"repository-mcp", "machine-mcp", "handwritten"} {
		if _, ok := servers[name]; !ok {
			t.Fatalf("shared settings lost %q: %s", name, before)
		}
	}
	for _, want := range []string{"// handwritten setting", "repository-audit", "machine-audit", "manual-tool"} {
		if !bytes.Contains(before, []byte(want)) {
			t.Fatalf("shared settings lost %q: %s", want, before)
		}
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("custom permissions changed: %v %v", info, err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof("settings.json")
	if err != nil || len(proof.OwnedIDs()) != 2 {
		t.Fatalf("combined ownership proof missing: %v %v", proof, err)
	}
	for _, opts := range [][]RunOption{{WithDryRun()}, nil} {
		report, err := Run(context.Background(), home, desired, opts...)
		if err != nil || !report.InSync() {
			t.Fatalf("identical sync or check drifted: %v %+v", err, report)
		}
		after, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("identical sync changed bytes: %v", err)
		}
		afterInfo, err := os.Stat(file)
		if err != nil || !afterInfo.ModTime().Equal(info.ModTime()) {
			t.Fatalf("identical sync rewrote settings: %v", err)
		}
	}
	report, err = Run(context.Background(), home, desired[2:])
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("pruning failed: %v %+v", err, report)
	}
	after, err := os.ReadFile(file)
	if err != nil || bytes.Contains(after, []byte("repository-mcp")) || bytes.Contains(after, []byte("repository-audit")) || !bytes.Contains(after, []byte("machine-mcp")) || !bytes.Contains(after, []byte("handwritten")) {
		t.Fatalf("pruning changed the wrong scope: %v %s", err, after)
	}
}

// spec: owned-entry-reconciliation/gemini-shared-identity-conflict-refuses-mutation
// spec: owned-entry-reconciliation/gemini-shared-scope-restricted-mutation-refuses
func TestGeminiSharedScopeRefusalsPreserveSettingsAndOwnership(t *testing.T) {
	for _, operation := range []string{"conflict", "repository-only", "machine-only"} {
		t.Run(operation, func(t *testing.T) {
			home := configureSharedGeminiHome(t)
			desired := sharedGeminiResources()
			report, err := Run(context.Background(), home, desired)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("valid control failed: %v %+v", err, report)
			}
			root := filepath.Join(home, ".gemini")
			store, err := ownership.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			file := &RepairFile{OutDir: root, Rel: "settings.json", store: store}
			before := ownedCleanupDiskState(t, file)
			info, err := os.Stat(file.AbsFile())
			if err != nil {
				t.Fatal(err)
			}
			var opts []RunOption
			switch operation {
			case "conflict":
				name := "repository-mcp"
				desired[2].Entry = catalog.Entry{Kind: ir.KindMcpServer, Name: name, Resource: ir.NewMcpServer(name, "different-tool", nil, nil, nil, ir.SourceRef{})}
			case "repository-only":
				opts = []RunOption{WithRepoScopeOnly()}
			case "machine-only":
				opts = []RunOption{WithMachineScopeOnly()}
			}
			for _, dryRun := range []bool{false, true} {
				options := append([]RunOption(nil), opts...)
				if dryRun {
					options = append(options, WithDryRun())
				}
				report, err := Run(context.Background(), home, desired, options...)
				if err != nil || !hasDiagCode(report.Diags, "materialize.destination.overlap") {
					t.Fatalf("shared destination was not refused: %v %+v", err, report)
				}
				requireOwnedCleanupDiskState(t, file, before)
				after, err := os.Stat(file.AbsFile())
				if err != nil || !info.ModTime().Equal(after.ModTime()) || info.Mode() != after.Mode() {
					t.Fatalf("refusal changed file metadata: %v", err)
				}
			}
		})
	}
}

func TestGeminiSharedScopeCompatibleIdentityAndIncompleteInput(t *testing.T) {
	home := configureSharedGeminiHome(t)
	desired := sharedGeminiResources()
	desired[2].Entry = desired[0].Entry
	report, err := Run(context.Background(), home, desired)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("equal cross-scope identity refused: %v %+v", err, report)
	}
	desired[0].Resource = nil
	desired[2].Resource = nil
	report, err = Run(context.Background(), home, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagCode(report.Diags, "materialize.resource.nil") {
		t.Fatalf("incomplete input did not exercise the refusal fence: %v", report.Diags)
	}
	data, err := os.ReadFile(filepath.Join(home, ".gemini", "settings.json"))
	if err != nil || !bytes.Contains(data, []byte("repository-mcp")) {
		t.Fatalf("incomplete input pruned the existing owned entry: %v %s", err, data)
	}
}

func BenchmarkSharedGeminiNativeSyncDisk(b *testing.B) {
	discardBenchLogs(b)
	home := configureSharedGeminiHome(b)
	desired := sharedGeminiResources()
	if _, err := Run(context.Background(), home, desired); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Run(context.Background(), home, desired); err != nil {
			b.Fatal(err)
		}
	}
}
