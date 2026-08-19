package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestCodexMCPDuplicateDesiredIdentityFailsClosed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	first, err := codexDesiredMCP([]byte("[mcp_servers.same]\ncommand = 'first'\n"), "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := codexDesiredMCP([]byte("[mcp_servers.same]\ncommand = 'second'\n"), "second")
	if err != nil {
		t.Fatal(err)
	}
	report := reconcileCodexMCP(context.Background(), &Report{PerTarget: map[targetmeta.Target]TargetReport{}}, outputKey{target: targetmeta.TargetCodex}, root, append(first, second...), false, false)
	if _, err := os.Stat(filepath.Join(root, codexMCPFile)); !os.IsNotExist(err) {
		t.Fatalf("duplicate desired identity wrote config.toml: %v", err)
	}
	found := false
	for _, d := range report.Diags {
		found = found || d.Code == "materialize.codex.duplicate-identity"
	}
	if !found {
		t.Fatalf("missing duplicate identity diagnostic: %#v", report.Diags)
	}
}

func TestCodexMCPDesiredTablesAreCanonicallyQuoted(t *testing.T) {
	entries, err := codexDesiredMCP([]byte("[mcp_servers.managed]\ncommand = 'npx'\n"), "managed")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasPrefix(string(entries[0].raw), "[mcp_servers.\"managed\"]\n") {
		t.Fatalf("non-canonical MCP fragment: %q", entries[0].raw)
	}
}

func TestCodexMCPQuotedExistingIdentityUsesNormalCollisionRules(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	abs := filepath.Join(root, codexMCPFile)
	original := []byte("[mcp_servers.\"managed\"]\ncommand = 'handwritten'\n")
	if err := os.WriteFile(abs, original, 0o644); err != nil {
		t.Fatal(err)
	}
	desired, err := codexDesiredMCP([]byte("[mcp_servers.managed]\ncommand = 'generated'\n"), "managed")
	if err != nil {
		t.Fatal(err)
	}
	report := reconcileCodexMCP(context.Background(), &Report{PerTarget: map[targetmeta.Target]TargetReport{}}, outputKey{target: targetmeta.TargetCodex}, root, desired, false, false)
	if !hasErrorDiagnostics(report.Diags) {
		t.Fatal("expected unmanaged quoted identity collision")
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("quoted unmanaged identity changed:\n%s", got)
	}
}

func TestCodexMCPInlineExistingIdentityUsesNormalCollisionRules(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	abs := filepath.Join(root, codexMCPFile)
	original := []byte("[mcp_servers]\nmanaged = { command = 'handwritten' }\n")
	if err := os.WriteFile(abs, original, 0o644); err != nil {
		t.Fatal(err)
	}
	desired, err := codexDesiredMCP([]byte("[mcp_servers.managed]\ncommand = 'generated'\n"), "managed")
	if err != nil {
		t.Fatal(err)
	}
	report := reconcileCodexMCP(context.Background(), &Report{PerTarget: map[targetmeta.Target]TargetReport{}}, outputKey{target: targetmeta.TargetCodex}, root, desired, false, false)
	if !hasErrorDiagnostics(report.Diags) {
		t.Fatal("expected unmanaged inline identity collision")
	}
	got, err := os.ReadFile(abs)
	if err != nil || string(got) != string(original) {
		t.Fatalf("inline unmanaged identity changed: %s (err=%v)", got, err)
	}
}

func TestCodexMCPDottedExistingIdentityIsClassified(t *testing.T) {
	sections, existing, err := codexMCPSections([]byte("mcp_servers.managed.command = 'npx'\nmcp_servers.managed.args = ['x']\n"))
	if err != nil || len(sections) != 1 || len(existing) != 1 || sections["managed"].value.ID.Key != "managed" {
		t.Fatalf("dotted MCP classification: sections=%#v existing=%#v err=%v", sections, existing, err)
	}
}

func TestCodexMCPInlineOwnedUpdateFailsClosedWithoutAbsorbingSibling(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	raw := []byte("[mcp_servers]\nfoo = { command = 'old' }\nbar = { command = 'keep' }\n")
	sections, _, err := codexMCPSections(raw)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	foo := sections["foo"].value
	if _, err := store.Apply(ownership.Mutation{Rel: codexMCPFile, AbsFile: filepath.Join(root, codexMCPFile), NextBytes: raw, Entries: []ownership.Entry{{Kind: foo.ID.Kind, Container: foo.ID.Container, Key: foo.ID.Key, IdentityDigest: foo.ID.IdentityDigest, ValueDigest: foo.ValueDigest, Resource: "foo"}}, Reason: ownership.ReasonSync}); err != nil {
		t.Fatal(err)
	}
	desired, err := codexDesiredMCP([]byte("[mcp_servers.foo]\ncommand = 'new'\n"), "foo")
	if err != nil {
		t.Fatal(err)
	}
	report := reconcileCodexMCP(context.Background(), &Report{PerTarget: map[targetmeta.Target]TargetReport{}}, outputKey{target: targetmeta.TargetCodex}, root, desired, false, false)
	if !hasErrorDiagnostics(report.Diags) {
		t.Fatal("expected unpatchable inline diagnostic")
	}
	got, err := os.ReadFile(filepath.Join(root, codexMCPFile))
	if err != nil || string(got) != string(raw) {
		t.Fatalf("inline sibling structure changed: %s (err=%v)", got, err)
	}
}

func TestCodexSyncDoesNotPruneUnrelatedManagedJSON(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	unrelated := filepath.Join(repo, "unrelated", "settings.json")
	if err := os.MkdirAll(filepath.Dir(unrelated), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"entries":[{"name":"keep","_managed_by":"agent-marketplace"}]}`)
	if err := os.WriteFile(unrelated, original, 0o644); err != nil {
		t.Fatal(err)
	}

	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "demo", Resource: ir.NewSkill("demo", "demo", "body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	if _, err := Run(context.Background(), repo, selection); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("unrelated JSON changed:\n%s", got)
	}
}

func TestCodexCompileFailurePreservesOwnedHook(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	valid := ir.NewHookWithMatcher("managed", "PreToolUse", "Bash", "./managed", "command", nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "managed", Resource: valid}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	if report, err := Run(context.Background(), repo, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed: report=%#v err=%v", report, err)
	}

	invalid := ir.NewHookWithMatcher("managed", "PreToolUse", "Bash", "./managed", "prompt", nil, ir.SourceRef{})
	selection[0].Resource = invalid
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if !hasErrorDiagnostics(report.Diags) {
		t.Fatal("expected rejected hook diagnostic")
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "./managed") {
		t.Fatalf("failed compilation pruned owned hook:\n%s", raw)
	}
}

func TestCodexCompileFailurePreservesOwnedMCP(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	valid := ir.NewMcpServer("managed", "npx", []string{"managed"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "managed", Resource: valid}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	if report, err := Run(context.Background(), repo, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed: report=%#v err=%v", report, err)
	}
	invalid := ir.NewMcpServer("managed", "npx", []string{"managed"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{})
	invalid.OAuthClientID = "unsupported"
	selection[0].Resource = invalid
	report, err := Run(context.Background(), repo, selection)
	if err != nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("rejected MCP: report=%#v err=%v", report, err)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".codex", codexMCPFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `mcp_servers."managed"`) {
		t.Fatalf("failed compilation pruned owned MCP:\n%s", raw)
	}
}

func TestCodexIncompleteReconcileRetainsHookOwnershipProof(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a := ir.NewHookWithMatcher("a", "PreToolUse", "Bash", "./a", "command", nil, ir.SourceRef{})
	b := ir.NewHookWithMatcher("b", "PostToolUse", "Bash", "./b", "command", nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "a", Resource: a}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}},
		{Entry: catalog.Entry{Name: "b", Resource: b}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}},
	}
	if report, err := Run(context.Background(), repo, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed: report=%#v err=%v", report, err)
	}
	selection[0].Resource = ir.NewHookWithMatcher("a", "PreToolUse", "Bash", "./a", "prompt", nil, ir.SourceRef{})
	selection[1].Resource = ir.NewHookWithMatcher("b", "PostToolUse", "Bash", "./changed", "command", nil, ir.SourceRef{})
	if _, err := Run(context.Background(), repo, selection); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(filepath.Join(repo, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(codexHooksFile)
	if err != nil || proof.OwnedCount() != 3 {
		t.Fatalf("hook proof count = %d, err=%v", proof.OwnedCount(), err)
	}
}

func TestCodexIncompleteReconcileRetainsMCPOwnershipProof(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a := ir.NewMcpServer("a", "npx", []string{"a"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{})
	b := ir.NewMcpServer("b", "npx", []string{"b"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Name: "a", Resource: a}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}},
		{Entry: catalog.Entry{Name: "b", Resource: b}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}},
	}
	if report, err := Run(context.Background(), repo, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed: report=%#v err=%v", report, err)
	}
	a.OAuthClientID = "unsupported"
	selection[1].Resource = ir.NewMcpServer("b", "npx", []string{"changed"}, map[string]string{"TOKEN": "${TOKEN}"}, nil, ir.SourceRef{})
	if _, err := Run(context.Background(), repo, selection); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(filepath.Join(repo, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(codexMCPFile)
	if err != nil || proof.OwnedCount() != 2 {
		t.Fatalf("MCP proof count = %d, err=%v", proof.OwnedCount(), err)
	}
}

func TestCodexMCPReportsCorruptOwnershipProofDuringDryRun(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	mcp := ir.NewMcpServer("managed", "npx", nil, nil, nil, ir.SourceRef{})
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "managed", Resource: mcp}, Source: config.RepoCommitted, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	if report, err := Run(context.Background(), repo, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed: report=%#v err=%v", report, err)
	}
	root := filepath.Join(repo, ".codex")
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.StateFile(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(root, codexMCPFile))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), repo, selection, WithDryRun())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range report.Diags {
		found = found || diagnostic.Code == diag.CodeOwnershipStateCorrupt
	}
	if !found {
		t.Fatalf("missing corrupt ownership diagnostic: %#v", report.Diags)
	}
	after, err := os.ReadFile(filepath.Join(root, codexMCPFile))
	if err != nil || string(after) != string(original) {
		t.Fatalf("dry run mutated MCP config: %s (err=%v)", after, err)
	}
}

func TestCodexInvalidHomePreflightDoesNotPruneRepository(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "relative")
	unrelated := filepath.Join(repo, "settings.json")
	original := []byte(`{"entry":{"_managed_by":"agent-marketplace"}}`)
	if err := os.WriteFile(unrelated, original, 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "demo", Resource: ir.NewSkill("demo", "demo", "body", nil, nil, ir.SourceRef{})}, Source: config.UserPersonal, Harnesses: []targetmeta.Target{targetmeta.TargetCodex}}}
	report, err := Run(context.Background(), repo, selection)
	if err != nil {
		t.Fatal(err)
	}
	if !hasErrorDiagnostics(report.Diags) {
		t.Fatal("expected invalid CODEX_HOME diagnostic")
	}
	got, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("invalid destination still triggered cleanup:\n%s", got)
	}
}
