package materialize

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func mcpResolvedForFactory(t *testing.T, name string) resolver.ResolvedArtifact {
	t.Helper()
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: name, Resource: ir.NewMcpServer(name, name+"-binary", []string{"serve"}, nil, nil, ir.SourceRef{File: "mcp.json", Line: 1})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}
}

func mcpServerNames(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v (%s)", path, err, data)
	}
	names := make([]string, 0, len(doc.MCPServers))
	for name := range doc.MCPServers {
		names = append(names, name)
	}
	return names
}

func hasName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// A merge destination is the user's own harness config. Rewriting it must not
// widen its permissions, and a sync that changes nothing must not rewrite it at
// all: these files hold MCP env values, headers, and OAuth tokens.
func TestConfinedFSReconcilingWriteKeepsModeAndSkipsUnchangedBytes(t *testing.T) {
	root := t.TempDir()
	fsys, err := openConfinedFS(root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()

	secret := filepath.Join(root, "mcp.json")
	if err := os.WriteFile(secret, []byte(`{"token":"a"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(secret, stale, stale); err != nil {
		t.Fatal(err)
	}

	wrote, err := fsys.AtomicWriteReconciling(secret, []byte(`{"token":"a"}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("identical bytes were written back")
	}
	info, err := os.Lstat(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stale) {
		t.Fatalf("unchanged file was replaced: mtime %v, want %v", info.ModTime(), stale)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %o", info.Mode().Perm())
	}

	wrote, err = fsys.AtomicWriteReconciling(secret, []byte(`{"token":"b"}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("changed bytes were not written")
	}
	info, err = os.Lstat(secret)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %o after a real write", info.Mode().Perm())
	}
	data, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"token":"b"}` {
		t.Fatalf("unexpected content %q", data)
	}

	// A destination that does not exist yet takes the fallback mode, masked by
	// umask like any other freshly created output.
	fresh := filepath.Join(root, "fresh.json")
	if _, err := fsys.AtomicWriteReconciling(fresh, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	freshInfo, err := os.Lstat(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if freshInfo.Mode().Perm() != 0o600 {
		t.Fatalf("fresh file mode %o, want 0600", freshInfo.Mode().Perm())
	}
}

func TestRunKeepsCredentialMergeFileModeAndDoesNotRewriteItUnchanged(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	repoRoot := t.TempDir()

	mergePath := filepath.Join(repoRoot, ".factory", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(mergePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mergePath, []byte(`{"mcpServers":{"handwritten":{"command":"secret","env":{"TOKEN":"hunter2"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved := []resolver.ResolvedArtifact{mcpResolvedForFactory(t, "generated")}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if diag.HasErrors(report.Diags) {
		t.Fatalf("merge failed: %+v", report.Diags)
	}
	names := mcpServerNames(t, mergePath)
	if !hasName(names, "handwritten") || !hasName(names, "generated") {
		t.Fatalf("merge lost an entry: %v", names)
	}
	info, err := os.Lstat(mergePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sync widened the credential file to %o", info.Mode().Perm())
	}

	// A second sync has nothing to change, so it must not replace the file.
	stale := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(mergePath, stale, stale); err != nil {
		t.Fatal(err)
	}
	report, err = Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if diag.HasErrors(report.Diags) {
		t.Fatalf("second merge failed: %+v", report.Diags)
	}
	info, err = os.Lstat(mergePath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stale) {
		t.Fatalf("a no-op sync rewrote the credential file: mtime %v, want %v", info.ModTime(), stale)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("second sync widened the credential file to %o", info.Mode().Perm())
	}
}

// A merge destination that is a symlink to nothing has no content to preserve
// and no content to leak. Refusing it left the harness config unreconcilable
// forever: the file could only be fixed by hand, and every sync failed the whole
// directory until someone did.
func TestRunReplacesDanglingMergeSymlink(t *testing.T) {
	cases := []struct {
		name   string
		target func(t *testing.T, repoRoot string) string
	}{
		{
			name: "in-root target",
			target: func(t *testing.T, repoRoot string) string {
				return "./gone.json"
			},
		},
		{
			name: "external target",
			target: func(t *testing.T, repoRoot string) string {
				return filepath.Join(t.TempDir(), "never-created.json")
			},
		},
		{
			name: "target under a removed cache directory",
			target: func(t *testing.T, repoRoot string) string {
				return filepath.Join(t.TempDir(), "compiled", "factory", "mcp.json")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
			repoRoot := t.TempDir()

			mergePath := filepath.Join(repoRoot, ".factory", "mcp.json")
			if err := os.MkdirAll(filepath.Dir(mergePath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(tc.target(t, repoRoot), mergePath); err != nil {
				t.Fatal(err)
			}

			report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{mcpResolvedForFactory(t, "generated")})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if diag.HasErrors(report.Diags) {
				t.Fatalf("a dangling symlink failed the merge: %+v", report.Diags)
			}
			if !hasDiagCode(report.Diags, codeMergeSymlinkDangling) {
				t.Fatalf("expected %s, got %+v", codeMergeSymlinkDangling, report.Diags)
			}
			info, err := os.Lstat(mergePath)
			if err != nil {
				t.Fatalf("merge destination missing: %v", err)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("merge destination is still %v, want a regular file", info.Mode())
			}
			if names := mcpServerNames(t, mergePath); !hasName(names, "generated") {
				t.Fatalf("generated entry missing: %v", names)
			}
		})
	}
}

// A merge destination pointing at a file that really exists outside every root
// we may read is the user's deliberate redirection AND an injection vector:
// reading it would seed the merged output with content the repository chose. It
// is refused per file, with a hint, and the link is left untouched.
func TestRunRefusesLiveExternalMergeSymlinkPerFileWithHint(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	repoRoot := t.TempDir()

	factoryDir := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "mcp.json")
	originalBody := []byte(`{"mcpServers":{"outside":{"command":"outside"}}}`)
	if err := os.WriteFile(external, originalBody, 0o600); err != nil {
		t.Fatal(err)
	}
	mergePath := filepath.Join(factoryDir, "mcp.json")
	if err := os.Symlink(external, mergePath); err != nil {
		t.Fatal(err)
	}

	sourceDir := t.TempDir()
	hooksJSON := filepath.Join(sourceDir, "hooks.json")
	if err := os.WriteFile(hooksJSON, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{
		mcpResolvedForFactory(t, "generated"),
		{
			Entry:     catalog.Entry{Name: "sibling-hook", Path: hooksJSON, Resource: ir.NewHookWithMatcher("sibling-hook", "PostToolUse", "Edit", "echo hi", "command", nil, ir.SourceRef{File: hooksJSON})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetFactory},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var refusal *diag.Diagnostic
	for i := range report.Diags {
		if report.Diags[i].Code == "materialize.write.error" {
			refusal = &report.Diags[i]
			break
		}
	}
	if refusal == nil {
		t.Fatalf("expected the external merge symlink to be refused, got %+v", report.Diags)
	}
	if refusal.Hint == "" {
		t.Fatalf("refusal carries no hint: %+v", *refusal)
	}
	if refusal.Resource == "" {
		t.Fatalf("refusal names no file: %+v", *refusal)
	}
	if target, err := os.Readlink(mergePath); err != nil || target != external {
		t.Fatalf("the symlink was replaced: target=%q err=%v", target, err)
	}
	assertUnchangedFile(t, external, originalBody, 0o600)

	// Per FILE: the refused mcp.json must not stop the hook aggregate in the
	// same directory from being reconciled.
	if _, err := os.Stat(filepath.Join(factoryDir, "hooks", "hooks.json")); err != nil {
		t.Fatalf("sibling merge artifact was not written: %v", err)
	}
	for _, tr := range report.TargetReports() {
		for _, written := range tr.Written {
			if filepath.ToSlash(written) == "mcp.json" {
				t.Fatalf("report claims mcp.json was written: %+v", tr)
			}
		}
	}
}

// os.ReadDir sorts; File.ReadDir does not. Prune decisions, report rows, and
// diagnostics are all derived from this order.
func TestConfinedFSReadDirReturnsSortedEntries(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "middle", "beta"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fsys, err := openConfinedFS(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fsys.Close() }()

	entries, err := fsys.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	want := []string{"alpha", "beta", "middle", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// The materialization manifest is a managed write like any other, so it goes
// through a confinement root: a symlinked .agents must not decide where the
// state file lands, nor which file is read back as our own state.
func TestPortableManifestWriteIsConfinedToItsOwnDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	repoRoot := t.TempDir()

	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(repoRoot, ".agents")); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "copied", Resource: ir.NewSkill("copied", "Copied", "Body", nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}}, WithMaterializationModes(config.MaterializationModes{
		Project: config.MaterializationModeFile,
		Machine: config.MaterializationModeFile,
	}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("expected the symlinked manifest directory to be refused, got %+v", report.Diags)
	}
	if _, err := os.Lstat(filepath.Join(external, "materialization-manifest.yaml")); err == nil {
		t.Fatal("the manifest was written through the .agents symlink")
	}
	entries, err := os.ReadDir(external)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("something was written outside the repository: %v", entries)
	}
}
