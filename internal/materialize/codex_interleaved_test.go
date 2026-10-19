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
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	toml "github.com/pelletier/go-toml/v2"
)

const (
	interleavedTable      = "[profiles.safe]\nhandwritten = true\n\n"
	interleavedArrayTable = "[[profiles.list]]\nname = 'kept'\n\n"
)

func interleaveCodexMCP(t *testing.T, installed, handwritten string) string {
	t.Helper()
	envHeader := strings.Index(installed, `[mcp_servers."foo".env]`)
	if envHeader < 0 {
		t.Fatalf("installed configuration has no foo env table:\n%s", installed)
	}
	return installed[:envHeader] + handwritten + installed[envHeader:]
}

func codexMCPServerValue(t *testing.T, raw []byte, name string) map[string]any {
	t.Helper()
	var document map[string]any
	if err := toml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("reconciled configuration is not valid TOML: %v\n%s", err, raw)
	}
	servers, _ := document["mcp_servers"].(map[string]any)
	server, _ := servers[name].(map[string]any)
	return server
}

// spec: owned-entry-reconciliation/interleaved-handwritten-tables-survive-owned-entry-sync
func TestRunPreservesHandwrittenTablesInterleavedWithOwnedCodexMCP(t *testing.T) {
	for _, handwritten := range []string{interleavedTable, interleavedArrayTable} {
		for _, change := range []string{"update", "retire"} {
			t.Run(strings.Fields(handwritten)[0]+"/"+change, func(t *testing.T) {
				repo, home := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				server := func(command string) resolver.ResolvedArtifact {
					return codexResource("foo", ir.NewMcpServer("foo", command, nil, map[string]string{"K": "v"}, nil, ir.SourceRef{}), config.RepoCommitted)
				}
				keep := codexResource("keep", ir.NewMcpServer("keep", "inert-keep", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted)
				report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{server("inert-a"), keep})
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, report.Diags)
				abs := filepath.Join(repo, ".codex", codexMCPFile)
				installed, err := os.ReadFile(abs)
				if err != nil {
					t.Fatal(err)
				}
				rewritten := interleaveCodexMCP(t, string(installed), handwritten)
				if err := os.WriteFile(abs, []byte(rewritten), 0o644); err != nil {
					t.Fatal(err)
				}

				next := []resolver.ResolvedArtifact{keep}
				if change == "update" {
					next = append(next, server("inert-b"))
				}
				report, err = Run(context.Background(), repo, next)
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, report.Diags)
				after, err := os.ReadFile(abs)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(after), handwritten) {
					t.Fatalf("handwritten table was not preserved byte-for-byte:\n%s", after)
				}
				foo := codexMCPServerValue(t, after, "foo")
				switch change {
				case "update":
					env, _ := foo["env"].(map[string]any)
					if foo["command"] != "inert-b" || env["K"] != "v" || strings.Contains(string(after), "inert-a") {
						t.Fatalf("owned server update not applied: %#v\n%s", foo, after)
					}
				case "retire":
					if foo != nil || strings.Contains(string(after), `"foo"`) {
						t.Fatalf("owned server not pruned: %#v\n%s", foo, after)
					}
				}
				if keepValue := codexMCPServerValue(t, after, "keep"); keepValue["command"] != "inert-keep" {
					t.Fatalf("sibling owned server changed: %#v\n%s", keepValue, after)
				}
			})
		}
	}
}

func TestReconcileCodexMCPAppendsNewServersInConfiguredOrder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	names := []string{"alpha", "beta", "gamma"}
	var desired []codexMCPEntry
	for _, name := range names {
		entries, err := codexDesiredMCP([]byte("[mcp_servers."+name+"]\ncommand = '"+name+"-binary'\n"), name)
		if err != nil {
			t.Fatal(err)
		}
		desired = append(desired, entries...)
	}
	report := reconcileCodexMCP(context.Background(), &Report{PerTarget: map[targetmeta.Target]TargetReport{}}, outputKey{target: targetmeta.TargetCodex}, "", root, desired, false, false)
	requireNoErrorDiags(t, report.Diags)
	raw, err := os.ReadFile(filepath.Join(root, codexMCPFile))
	if err != nil {
		t.Fatal(err)
	}
	previous := -1
	for _, name := range names {
		at := strings.Index(string(raw), name+"-binary")
		if at <= previous {
			t.Fatalf("new servers are not appended in configured order:\n%s", raw)
		}
		previous = at
	}
}

func codexMCPOwnedKeys(t *testing.T, file *RepairFile) map[string]bool {
	t.Helper()
	record, err := file.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]bool)
	for _, entry := range record.Files[file.Rel].Entries {
		keys[entry.Key] = true
	}
	return keys
}

// spec: owned-entry-reconciliation/interleaved-handwritten-tables-survive-owned-entry-removal-and-replacement
func TestCodexRepairOrphanRemovalPreservesInterleavedHandwrittenTable(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	selection := []resolver.ResolvedArtifact{codexRepairResolved("foo", ir.NewMcpServer("foo", "inert-a", nil, map[string]string{"K": "v"}, nil, ir.SourceRef{}), config.RepoCommitted)}
	if report, err := Run(context.Background(), repoRoot, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Codex MCP: report=%#v err=%v", report, err)
	}
	root, err := ownership.CanonicalRoot(filepath.Join(repoRoot, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, codexMCPFile)
	installed, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(interleaveCodexMCP(t, string(installed), interleavedTable)), 0o644); err != nil {
		t.Fatal(err)
	}
	dropOwnershipState(t, root)

	preview, err := CodexRepairPreview(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	file := codexRepairFileFor(t, preview, root, codexMCPFile)
	if len(file.Orphans) != 1 || file.Orphans[0].Identity() != "foo" {
		t.Fatalf("orphan candidates = %#v, want only foo", file.Orphans)
	}
	if _, err := file.RemoveOrphan(0, true); err != nil {
		t.Fatalf("confirmed orphan removal: %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), interleavedTable) || codexMCPServerValue(t, after, "foo") != nil {
		t.Fatalf("orphan removal did not delete only the server's tables:\n%s", after)
	}
	if codexMCPOwnedKeys(t, file)["foo"] {
		t.Fatal("removed orphan is still recorded as owned")
	}
}

// spec: owned-entry-reconciliation/interleaved-handwritten-tables-survive-owned-entry-removal-and-replacement
func TestCodexRepairConflictReplacementPreservesInterleavedHandwrittenTable(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := filepath.Join(repoRoot, ".codex")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, codexMCPFile)
	original := "[mcp_servers.foo]\ncommand = 'user-binary'\n\n" + interleavedTable + "[mcp_servers.foo.env]\nK = 'user'\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{codexRepairResolved("foo", ir.NewMcpServer("foo", "generated-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted)}
	canonicalRoot, err := ownership.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	file := codexRepairFileFor(t, preview, canonicalRoot, codexMCPFile)
	if len(file.Conflicts) != 1 || file.Conflicts[0].Identity() != "foo" {
		t.Fatalf("conflict candidates = %#v, want only foo", file.Conflicts)
	}
	if _, err := file.ReplaceConflict(0, true); err != nil {
		t.Fatalf("confirmed conflict replacement: %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	foo := codexMCPServerValue(t, after, "foo")
	if !strings.Contains(string(after), interleavedTable) || foo["command"] != "generated-binary" || foo["env"] != nil || strings.Contains(string(after), "user") {
		t.Fatalf("replacement did not replace only the server's tables: %#v\n%s", foo, after)
	}
	if !codexMCPOwnedKeys(t, file)["foo"] {
		t.Fatal("replaced server is not recorded as owned")
	}
}

// spec: owned-entry-reconciliation/interleaved-handwritten-tables-survive-owned-entry-removal-and-replacement
func TestRemoveOwnedCodexMCPPreservesInterleavedHandwrittenTable(t *testing.T) {
	repoRoot, home := devinEnv(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	server := func(name string, env map[string]string) resolver.ResolvedArtifact {
		return resolver.ResolvedArtifact{Entry: catalog.Entry{Name: name, Kind: ir.KindMcpServer, Resource: ir.NewMcpServer(name, name+"-binary", nil, env, nil, ir.SourceRef{})}, Source: config.UserPersonal, Harnesses: []hcc.Target{hcc.TargetCodex}}
	}
	keep := server("keep", nil)
	if report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{server("foo", map[string]string{"K": "v"}), keep}); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Codex MCP: report=%#v err=%v", report, err)
	}
	root, err := filepath.EvalSymlinks(filepath.Join(home, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := CodexRepairPreview(context.Background(), repoRoot, []resolver.ResolvedArtifact{keep})
	if err != nil {
		t.Fatal(err)
	}
	file := codexRepairFileFor(t, preview, root, codexMCPFile)
	installed, err := os.ReadFile(file.AbsFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file.AbsFile(), []byte(interleaveCodexMCP(t, string(installed), interleavedTable)), 0o600); err != nil {
		t.Fatal(err)
	}
	state, diags := file.adapter.read(file.store, outputKey{target: file.Target, machineScope: file.MachineScope}, file.OutDir, file.Rel)
	if state == nil || len(diags) != 0 {
		t.Fatalf("state = %#v diagnostics=%v", state, diags)
	}
	var target ownership.EntryValue
	for _, existing := range state.existing {
		if existing.ID.Key == "foo" {
			target = existing
		}
	}
	if target.ID.Key != "foo" {
		t.Fatalf("owned foo entry missing: %#v", state.existing)
	}
	if _, err := file.RemoveOwned(context.Background(), target, true); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(file.AbsFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), interleavedTable) || codexMCPServerValue(t, after, "foo") != nil || codexMCPServerValue(t, after, "keep")["command"] != "keep-binary" {
		t.Fatalf("cleanup did not remove only the owned server's tables:\n%s", after)
	}
	if owned := codexMCPOwnedKeys(t, file); owned["foo"] || !owned["keep"] {
		t.Fatalf("ownership after cleanup = %v, want keep only", owned)
	}
}

// spec: owned-entry-reconciliation/interleaved-handwritten-tables-survive-owned-entry-removal-and-replacement
func TestCodexMCPRepairEditsPreserveInterleavedHandwrittenTables(t *testing.T) {
	raw := []byte("[mcp_servers.\"foo\"]\ncommand = 'inert-a'\n\n" + interleavedTable + "[mcp_servers.\"foo\".env]\nK = 'v'\n\n[mcp_servers.\"keep\"]\ncommand = 'inert-keep'\n")
	sections, _, err := codexMCPSections(raw)
	if err != nil {
		t.Fatal(err)
	}
	state := &codexMCPRepairState{raw: raw, sections: sections}
	id := ownership.EntryID{Kind: ownership.KindMcpServer, Container: codexMCPContainer, Key: "foo"}

	removed, err := state.remove(id, sections["foo"].value.ValueDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(removed), interleavedTable) || codexMCPServerValue(t, removed, "foo") != nil || codexMCPServerValue(t, removed, "keep") == nil {
		t.Fatalf("removal did not delete only the owned server:\n%s", removed)
	}

	replaced, err := state.replace(id, []byte("[mcp_servers.\"foo\"]\ncommand = 'inert-b'\n"))
	if err != nil {
		t.Fatal(err)
	}
	foo := codexMCPServerValue(t, replaced, "foo")
	if !strings.Contains(string(replaced), interleavedTable) || foo["command"] != "inert-b" || foo["env"] != nil || codexMCPServerValue(t, replaced, "keep") == nil {
		t.Fatalf("replacement did not replace only the owned server: %#v\n%s", foo, replaced)
	}
}
