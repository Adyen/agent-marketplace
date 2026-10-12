package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestCodexAggregateReportsResolvedDestinationID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	entries, err := codexHookFragmentEntries([]byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./managed"}]}]}}`), "managed")
	if err != nil {
		t.Fatal(err)
	}
	work := &ownedJSONAggregateWork{}
	for _, entry := range entries {
		work.add(entry)
	}
	key := outputKey{target: targetmeta.TargetCodex}
	state := ownedAggregateState{values: map[outputKey]any{
		key: &codexOwnedAggregateState{hooks: work},
	}}
	report := &Report{PerTarget: map[targetmeta.Target]TargetReport{}}
	destinations := []harness.Destination{{
		ID:   "resolved-config-root",
		Root: root,
		Info: harness.TargetInfo{OwnershipReconciledAggregates: true},
	}}

	report = (codexOwnedAggregateHandler{}).reconcile(context.Background(), report, state, ownedAggregateState{}, key, "", destinations, true)
	if len(report.PerRoot) != 1 {
		t.Fatalf("per-root reports = %#v, want one", report.PerRoot)
	}
	if got := report.PerRoot[0].RootID; got != destinations[0].ID {
		t.Fatalf("root ID = %q, want resolved destination %q", got, destinations[0].ID)
	}
}

func TestCodexHooksReconcileWithDurableOwnership(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	hooksPath := filepath.Join(repo, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "{\n  // handwritten first\n  \"hooks\": {\n    \"PreToolUse\": [{\"matcher\":\"Read\",\"hooks\":[{\"type\":\"command\",\"command\":\"./handwritten\"}]}]\n  },\n  \"other\": true\n}\n"
	if err := os.WriteFile(hooksPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("managed", "PreToolUse", "Bash", "./managed", "command", nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "managed", Resource: hook}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}

	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("report = %#v", report)
	}
	after, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(after)
	for _, want := range []string{"// handwritten first", `"other": true`, "./handwritten", "./managed"} {
		if !strings.Contains(text, want) {
			t.Errorf("hooks lost %q:\n%s", want, text)
		}
	}

	if report, err := Run(context.Background(), repo, nil); err != nil {
		t.Fatal(err)
	} else if hasErrorDiagnostics(report.Diags) {
		t.Fatalf("prune diagnostics: %#v", report.Diags)
	}
	pruned, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pruned), "./managed") || !strings.Contains(string(pruned), "./handwritten") {
		t.Fatalf("ownership prune wrong:\n%s", pruned)
	}
}

func TestCodexHookDiagnosticsDoNotUseDevinCodes(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	hooksPath := filepath.Join(repo, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./managed"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("managed", "PreToolUse", "Bash", "./managed", "command", nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "managed", Resource: hook}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}

	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeMaterializeCodexHookSatisfiedByUnmanaged); !ok {
		t.Fatalf("Codex satisfaction diagnostic missing: %#v", report.Diags)
	}
	if _, leaked := findDiag(report.Diags, diag.CodeMaterializeDevinHookSatisfiedByUnmanaged); leaked {
		t.Fatalf("Codex emitted a Devin diagnostic: %#v", report.Diags)
	}
}

func TestCodexHooksReplaceReformattedOwnedEntryAsDrift(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	hook := ir.NewHookWithMatcher("managed", "PreToolUse", "Bash", "./managed", "command", nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "managed", Resource: hook}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	if report, err := Run(context.Background(), repo, selection); err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed hook: report=%#v err=%v", report, err)
	}
	hooksPath := filepath.Join(repo, ".codex", "hooks.json")
	reformatted := []byte(`{
  "hooks": {
    "PreToolUse": [
      {
        // keep this owned-entry formatting
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "./managed"
          }
        ]
      }
    ]
  }
}
`)
	if err := os.WriteFile(hooksPath, reformatted, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repo, selection)
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("drift repair sync: report=%#v err=%v", report, err)
	}
	if !diag.HasCode(report.Diags, diag.CodeOwnershipEntryDrift) {
		t.Fatalf("diagnostics = %#v, want %s", report.Diags, diag.CodeOwnershipEntryDrift)
	}
	after, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(reformatted) || strings.Contains(string(after), "keep this owned-entry formatting") {
		t.Fatalf("raw-byte drift was not replaced from the desired hook:\n%s", after)
	}
}

func TestCodexMCPReconcilePreservesUnmanagedTOML(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	configPath := filepath.Join(repo, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "# keep this comment\nmodel = 'gpt-5.6-terra'\n\n[mcp_servers.handwritten]\ncommand = 'handwritten'\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := ir.NewMcpServer("managed", "npx", []string{"managed"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "managed", Resource: mcp}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	if report, err := Run(context.Background(), repo, selection); err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("run: report=%#v err=%v", report, err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# keep this comment", "model = 'gpt-5.6-terra'", "mcp_servers.handwritten", `mcp_servers."managed"`} {
		if !strings.Contains(string(after), want) {
			t.Errorf("config lost %q:\n%s", want, after)
		}
	}
	updated := ir.NewMcpServer("managed", "npx", []string{"updated"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{})
	selection[0].Resource = updated
	if report, err := Run(context.Background(), repo, selection, WithDryRun()); err != nil || report.InSync() {
		t.Fatalf("dry-run update: report=%#v err=%v", report, err)
	}
	if raw, err := os.ReadFile(configPath); err != nil || strings.Contains(string(raw), "updated") {
		t.Fatalf("dry-run changed config: %s (err=%v)", raw, err)
	}
	if report, err := Run(context.Background(), repo, selection); err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("update: report=%#v err=%v", report, err)
	}
	if raw, err := os.ReadFile(configPath); err != nil || !strings.Contains(string(raw), "updated") || strings.Contains(string(raw), "args = ['managed']") {
		t.Fatalf("owned update wrong: %s (err=%v)", raw, err)
	}
	if report, err := Run(context.Background(), repo, nil); err != nil {
		t.Fatal(err)
	} else if hasErrorDiagnostics(report.Diags) {
		t.Fatalf("prune diagnostics: %#v", report.Diags)
	}
	pruned, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pruned), `mcp_servers."managed"`) || !strings.Contains(string(pruned), "mcp_servers.handwritten") {
		t.Fatalf("ownership prune wrong:\n%s", pruned)
	}
}

func hasErrorDiagnostics(diagnostics []diag.Diagnostic) bool { return diag.HasErrors(diagnostics) }
