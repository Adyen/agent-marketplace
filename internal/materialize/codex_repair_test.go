package materialize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

func codexRepairResolved(name string, resource ir.Resource, source config.Source) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: name, Resource: resource},
		Source:    source,
		Harnesses: []targetmeta.Target{targetmeta.TargetCodex},
	}
}

func codexRepairFileFor(t *testing.T, preview *RepairPreview, root, rel string) *RepairFile {
	t.Helper()
	for _, file := range preview.Files {
		if file.OutDir == root && file.Rel == rel {
			return file
		}
	}
	t.Fatalf("preview has no repair file for %q under %q", rel, root)
	return nil
}

func TestCodexRepairPreviewNominatesExactMCPMatchWithoutMutation(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	selection := []resolver.ResolvedArtifact{codexRepairResolved(
		"managed",
		ir.NewMcpServer("managed", "npx", []string{"managed"}, nil, nil, ir.SourceRef{}),
		config.RepoCommitted,
	)}
	if report, err := Run(context.Background(), repoRoot, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Codex MCP: report=%#v err=%v", report, err)
	}
	root, err := ownership.CanonicalRoot(filepath.Join(repoRoot, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	dropOwnershipState(t, root)
	configPath := filepath.Join(root, codexMCPFile)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("Codex repair preview: %v", err)
	}
	file := codexRepairFileFor(t, preview, root, codexMCPFile)
	if len(file.Adoptions) != 1 {
		t.Fatalf("adoption candidates = %d, want 1", len(file.Adoptions))
	}
	if got := file.Adoptions[0].Identity(); got != "managed" {
		t.Fatalf("adoption identity = %q, want managed", got)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("preview mutated config.toml:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCodexRepairAdoptionRecordsOwnershipWithoutChangingMCPBytes(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	selection := []resolver.ResolvedArtifact{codexRepairResolved(
		"managed",
		ir.NewMcpServer("managed", "npx", []string{"managed"}, nil, nil, ir.SourceRef{}),
		config.RepoCommitted,
	)}
	if report, err := Run(context.Background(), repoRoot, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Codex MCP: report=%#v err=%v", report, err)
	}
	root, err := ownership.CanonicalRoot(filepath.Join(repoRoot, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	dropOwnershipState(t, root)
	configPath := filepath.Join(root, codexMCPFile)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	file := codexRepairFileFor(t, preview, root, codexMCPFile)

	result, err := file.Adopt([]int{0})
	if err != nil {
		t.Fatalf("adopt exact Codex MCP entry: %v", err)
	}
	if result.Generation == 0 || result.BackupPath == "" {
		t.Fatalf("adoption result = %#v, want committed generation and backup", result)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("adoption changed config.toml:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	second, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	if second.Actionable() {
		t.Fatal("adopted Codex MCP entry remains actionable")
	}
}

func TestCodexRepairOrphanRemovalRequiresEvidenceConfirmationAndFreshBackup(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := filepath.Join(repoRoot, ".codex")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, codexMCPFile)
	original := "# keep\n[mcp_servers.handwritten]\ncommand = 'user-binary'\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{codexRepairResolved(
		"managed",
		ir.NewMcpServer("managed", "npx", []string{"managed"}, nil, nil, ir.SourceRef{}),
		config.RepoCommitted,
	)}
	if report, err := Run(context.Background(), repoRoot, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Codex MCP: report=%#v err=%v", report, err)
	}
	canonicalRoot, err := ownership.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	dropOwnershipState(t, canonicalRoot)

	preview, err := CodexRepairPreview(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	file := codexRepairFileFor(t, preview, canonicalRoot, codexMCPFile)
	if len(file.Orphans) != 1 || file.Orphans[0].Identity() != "managed" {
		t.Fatalf("orphan candidates = %#v, want only managed", file.Orphans)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.RemoveOrphan(0, false); !errors.Is(err, ownership.ErrConfirmationRequired) {
		t.Fatalf("unconfirmed removal error = %v, want confirmation required", err)
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != string(before) {
		t.Fatalf("unconfirmed removal changed config.toml: %s (err=%v)", got, err)
	}

	result, err := file.RemoveOrphan(0, true)
	if err != nil {
		t.Fatalf("confirmed orphan removal: %v", err)
	}
	if result.BackupPath == "" {
		t.Fatal("confirmed orphan removal did not take a fresh backup")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), `mcp_servers."managed"`) || !strings.Contains(string(after), "mcp_servers.handwritten") || !strings.Contains(string(after), "# keep") {
		t.Fatalf("orphan removal did not preserve unrelated TOML:\n%s", after)
	}
}

func TestCodexRepairConflictReplacementRequiresConfirmationAndFreshBackup(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := filepath.Join(repoRoot, ".codex")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, codexMCPFile)
	original := "# keep\nmodel = 'gpt-5'\n\n[mcp_servers.managed]\ncommand = 'user-binary'\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{codexRepairResolved(
		"managed",
		ir.NewMcpServer("managed", "generated-binary", nil, nil, nil, ir.SourceRef{}),
		config.RepoCommitted,
	)}
	canonicalRoot, err := ownership.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	file := codexRepairFileFor(t, preview, canonicalRoot, codexMCPFile)
	if len(file.Conflicts) != 1 || file.Conflicts[0].Identity() != "managed" {
		t.Fatalf("conflict candidates = %#v, want only managed", file.Conflicts)
	}
	if _, err := file.ReplaceConflict(0, false); !errors.Is(err, ownership.ErrConfirmationRequired) {
		t.Fatalf("unconfirmed replacement error = %v, want confirmation required", err)
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != original {
		t.Fatalf("unconfirmed replacement changed config.toml: %s (err=%v)", got, err)
	}

	result, err := file.ReplaceConflict(0, true)
	if err != nil {
		t.Fatalf("confirmed conflict replacement: %v", err)
	}
	if result.BackupPath == "" {
		t.Fatal("confirmed conflict replacement did not take a fresh backup")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# keep", "model = 'gpt-5'", "generated-binary"} {
		if !strings.Contains(string(after), want) {
			t.Errorf("replacement lost %q:\n%s", want, after)
		}
	}
	if strings.Contains(string(after), "user-binary") {
		t.Fatalf("replacement retained old conflicting value:\n%s", after)
	}
	second, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	if second.Actionable() {
		t.Fatal("replaced Codex MCP entry remains actionable")
	}
}

func TestCodexRepairMalformedMCPIsRedactedAndDoesNotBlockHookAdoption(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	selection := []resolver.ResolvedArtifact{codexHookResolved(t, "managed-hook", ir.NewHookWithMatcher("managed-hook", "PreToolUse", "Bash", "./managed", "command", nil, ir.SourceRef{}), config.RepoCommitted)}
	if report, err := Run(context.Background(), repoRoot, selection); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Codex hook: report=%#v err=%v", report, err)
	}
	root, err := ownership.CanonicalRoot(filepath.Join(repoRoot, ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	dropOwnershipState(t, root)
	hooksPath := filepath.Join(root, codexHooksFile)
	beforeHooks, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "Bearer repair-must-not-print-this"
	malformed := "[mcp_servers.bad]\nhttp_headers = { Authorization = \"" + secret + "\"\n"
	configPath := filepath.Join(root, codexMCPFile)
	if err := os.WriteFile(configPath, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.HasErrors() {
		t.Fatalf("malformed config.toml produced no error diagnostics: %#v", preview.Diags)
	}
	hooks := codexRepairFileFor(t, preview, root, codexHooksFile)
	if len(hooks.Adoptions) != 1 {
		t.Fatalf("malformed config.toml blocked independent hooks preview: %#v", hooks.Adoptions)
	}
	for _, diagnostic := range preview.Diags {
		if strings.Contains(diagnostic.Message, secret) {
			t.Fatalf("repair diagnostic leaked an MCP header value: %q", diagnostic.Message)
		}
		if diagnostic.Code == codexRepairMalformedCode && !strings.Contains(diagnostic.Message, "Codex shared configuration is malformed or unsupported") {
			t.Fatalf("malformed MCP diagnostic is not safely summarized: %q", diagnostic.Message)
		}
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != malformed {
		t.Fatalf("preview changed malformed config.toml: %s (err=%v)", got, err)
	}
	if _, err := hooks.Adopt([]int{0}); err != nil {
		t.Fatalf("malformed config.toml blocked independent hooks adoption: %v", err)
	}
	if got, err := os.ReadFile(hooksPath); err != nil || string(got) != string(beforeHooks) {
		t.Fatalf("hook adoption changed hooks.json: %s (err=%v)", got, err)
	}
	second, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	if !second.HasErrors() {
		t.Fatal("malformed config.toml stopped being an independent repair error")
	}
	if got := codexRepairFileFor(t, second, root, codexHooksFile); len(got.Adoptions) != 0 {
		t.Fatalf("independently adopted hook remains actionable: %#v", got.Adoptions)
	}
}

func TestCodexRepairPreviewUsesDefaultAndCustomMachineRoots(t *testing.T) {
	tests := []struct {
		name       string
		customHome bool
	}{
		{name: "default CODEX_HOME"},
		{name: "custom CODEX_HOME", customHome: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			codexHome := filepath.Join(home, ".codex")
			if tt.customHome {
				codexHome = filepath.Join(t.TempDir(), "external-codex-home")
				t.Setenv("CODEX_HOME", codexHome)
			} else {
				t.Setenv("CODEX_HOME", "")
			}
			selection := []resolver.ResolvedArtifact{codexRepairResolved(
				"managed",
				ir.NewMcpServer("managed", "npx", nil, nil, nil, ir.SourceRef{}),
				config.UserPersonal,
			)}
			if report, err := Run(context.Background(), repoRoot, selection); err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("seed machine Codex MCP: report=%#v err=%v", report, err)
			}
			root, err := ownership.CanonicalRoot(codexHome)
			if err != nil {
				t.Fatal(err)
			}
			dropOwnershipState(t, root)

			preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
			if err != nil {
				t.Fatal(err)
			}
			file := codexRepairFileFor(t, preview, root, codexMCPFile)
			if !file.MachineScope || len(file.Adoptions) != 1 {
				t.Fatalf("machine repair file = %#v, want one adoption under %q", file, root)
			}
		})
	}
}

func TestCodexRepairNeverOffersReplacementForHooksWithoutStableKeys(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := filepath.Join(repoRoot, ".codex")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	hooks := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./handwritten"}]}]}}`
	if err := os.WriteFile(filepath.Join(root, codexHooksFile), []byte(hooks), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{codexHookResolved(t, "managed-hook", ir.NewHookWithMatcher("managed-hook", "PreToolUse", "Bash", "./generated", "command", nil, ir.SourceRef{}), config.RepoCommitted)}
	canonicalRoot, err := ownership.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	file := codexRepairFileFor(t, preview, canonicalRoot, codexHooksFile)
	if len(file.Conflicts) != 0 {
		t.Fatalf("hook repair offered destructive replacement without a stable native key: %#v", file.Conflicts)
	}
}

func TestCodexRepairReReadsAndRefusesStaleConflictInsideWriteSession(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := filepath.Join(repoRoot, ".codex")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, codexMCPFile)
	if err := os.WriteFile(configPath, []byte("[mcp_servers.managed]\ncommand = 'previewed'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{codexRepairResolved(
		"managed",
		ir.NewMcpServer("managed", "generated", nil, nil, nil, ir.SourceRef{}),
		config.RepoCommitted,
	)}
	canonicalRoot, err := ownership.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	file := codexRepairFileFor(t, preview, canonicalRoot, codexMCPFile)
	if len(file.Conflicts) != 1 {
		t.Fatalf("conflicts = %d, want 1", len(file.Conflicts))
	}
	changed := "[mcp_servers.managed]\ncommand = 'changed-after-preview'\n"
	if err := os.WriteFile(configPath, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	counts := installRepairLockCounter(t)
	if _, err := file.ReplaceConflict(0, true); !errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("stale conflict error = %v, want ErrRepairCandidateStale", err)
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != changed {
		t.Fatalf("stale replacement changed config.toml: %s (err=%v)", got, err)
	}
	acquired, reentered, released, maxDepth := counts.snapshot()
	if acquired != 1 || reentered != 0 || released != 1 || maxDepth != 1 {
		t.Fatalf("stale repair lock counts = (%d,%d,%d,%d), want (1,0,1,1)", acquired, reentered, released, maxDepth)
	}
}

func codexRepairDuplicateDiag(t *testing.T, preview *RepairPreview, key string, resources ...string) {
	t.Helper()
	for _, d := range preview.Diags {
		if d.Code != diag.CodeMaterializeCodexDuplicateIdentity || !strings.Contains(d.Message, strconv.Quote(key)) {
			continue
		}
		if d.Severity != diag.SeverityError {
			t.Fatalf("duplicate identity %q is not an error: %+v", key, d)
		}
		for _, resource := range resources {
			if !strings.Contains(d.Message, strconv.Quote(resource)) {
				t.Fatalf("duplicate identity %q does not name %q: %+v", key, resource, d)
			}
		}
		return
	}
	t.Fatalf("duplicate desired identity %q not refused like sync: %+v", key, preview.Diags)
}

// spec: owned-entry-reconciliation/duplicate-desired-codex-mcp-identity-is-refused-by-repair
func TestCodexRepairPreviewRefusesDuplicateDesiredMCPIdentity(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := filepath.Join(repoRoot, ".codex")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, codexMCPFile)
	original := "[mcp_servers.exact]\ncommand = 'beta-binary'\n\n[mcp_servers.differs]\ncommand = 'handwritten'\n\n[mcp_servers.adoptable]\ncommand = 'adopt-binary'\n\n[mcp_servers.other]\ncommand = 'user-binary'\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := []resolver.ResolvedArtifact{
		codexRepairResolved("alpha", ir.NewMcpServer("exact", "alpha-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
		codexRepairResolved("beta", ir.NewMcpServer("exact", "beta-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
		codexRepairResolved("gamma", ir.NewMcpServer("differs", "gamma-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
		codexRepairResolved("delta", ir.NewMcpServer("differs", "delta-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
		codexRepairResolved("adoptable", ir.NewMcpServer("adoptable", "adopt-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
		codexRepairResolved("other", ir.NewMcpServer("other", "other-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
	}
	canonicalRoot, err := ownership.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	codexRepairDuplicateDiag(t, preview, "exact", "alpha", "beta")
	codexRepairDuplicateDiag(t, preview, "differs", "gamma", "delta")
	file := codexRepairFileFor(t, preview, canonicalRoot, codexMCPFile)
	if len(file.Adoptions) != 1 || file.Adoptions[0].Identity() != "adoptable" || file.Adoptions[0].Resource != "adoptable" {
		t.Fatalf("adoptions = %#v, want only the unambiguous adoptable entry", file.Adoptions)
	}
	if len(file.Conflicts) != 1 || file.Conflicts[0].Identity() != "other" || file.Conflicts[0].Resource != "other" {
		t.Fatalf("conflicts = %#v, want only the unambiguous other entry", file.Conflicts)
	}
	if len(file.Orphans) != 0 {
		t.Fatalf("duplicate identity offered for removal: %#v", file.Orphans)
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != original {
		t.Fatalf("preview changed config.toml: %s (err=%v)", got, err)
	}
}

// spec: owned-entry-reconciliation/duplicate-desired-codex-mcp-identity-is-refused-by-repair
func TestCodexRepairPreviewRefusesDuplicateDesiredMCPIdentityWithoutConfig(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	selection := []resolver.ResolvedArtifact{
		codexRepairResolved("alpha", ir.NewMcpServer("shared", "alpha-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
		codexRepairResolved("beta", ir.NewMcpServer("shared", "beta-binary", nil, nil, nil, ir.SourceRef{}), config.RepoCommitted),
	}
	preview, err := CodexRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatal(err)
	}
	codexRepairDuplicateDiag(t, preview, "shared", "alpha", "beta")
	if _, err := os.Lstat(filepath.Join(repoRoot, ".codex", codexMCPFile)); !os.IsNotExist(err) {
		t.Fatalf("preview created config.toml: %v", err)
	}
}

func TestRepairReplacementWritesTheDesiredEntryItsConflictDescribes(t *testing.T) {
	id := ownership.EntryID{Kind: ownership.KindMcpServer, Container: "mcpServers", Key: "shared", IdentityDigest: ownership.McpIdentityDigest("shared")}
	desired := func(value string) repairDesiredEntry {
		return repairDesiredEntry{value: ownership.EntryValue{ID: id, ValueDigest: ownership.Digest([]byte(value))}, raw: []byte(value), resource: value}
	}
	file := &RepairFile{desired: []repairDesiredEntry{desired("first"), desired("last")}}
	existing := []ownership.EntryValue{{ID: id, ValueDigest: ownership.Digest([]byte("handwritten"))}}
	conflicts := ownership.ValueConflictCandidates(ownership.Proof{}, existing, file.repairDesiredValues())
	raw, resource, ok := file.repairDesiredEntry(id)
	if len(conflicts) != 1 || !ok {
		t.Fatalf("conflicts = %#v, desired entry found = %t", conflicts, ok)
	}
	if conflicts[0].DesiredValueDigest() != ownership.Digest(raw) || string(raw) != "last" || resource != "last" {
		t.Fatalf("conflict describes digest %s but replacement writes %q from %q", conflicts[0].DesiredValueDigest(), raw, resource)
	}
}
