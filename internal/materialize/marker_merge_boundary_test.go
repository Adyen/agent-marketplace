package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/marker-merge-refuses-an-external-alias-before-reading
func TestRunMarkerMergeRefusesExternalAlias(t *testing.T) {
	home, repo, external := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	root := filepath.Join(repo, ".factory")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(external, "private.json")
	const body = `{"mcpServers":{"foreign-entry":{"command":"inert foreign value"}}}`
	if err := os.WriteFile(payload, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(root, "mcp.json")
	if err := os.Symlink(payload, leaf); err != nil {
		t.Fatal(err)
	}
	linkBefore, err := os.Lstat(leaf)
	if err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "safe-server", Resource: ir.NewMcpServer("safe-server", "inert-command", nil, nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	report, err := Run(t.Context(), repo, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("external marker alias was not refused: %+v", report.Diags)
	}
	if after, err := os.Lstat(leaf); err != nil || !os.SameFile(linkBefore, after) || after.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("refusal detached external alias: %v", err)
	}
	if raw, err := os.ReadFile(payload); err != nil || string(raw) != body {
		t.Fatalf("refusal changed external content: %q %v", raw, err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("refusal changed external metadata: %v", err)
	}
	control, err := Run(t.Context(), t.TempDir(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, control.Diags)
	if control.TotalWritten() == 0 {
		t.Fatal("native MCP control did not publish desired entry")
	}
}

// spec: materialization/contained-marker-aliases-preserve-native-content-and-no-ops
func TestRunContainedMarkerAliasesPreserveNativeContent(t *testing.T) {
	for _, tc := range []struct {
		target                 hcc.Target
		project, machine, file string
	}{
		{hcc.TargetFactory, ".factory", ".factory", "mcp.json"},
		{hcc.TargetCursor, ".cursor", ".cursor", "mcp.json"},
		{hcc.TargetWindsurf, ".windsurf", ".codeium/windsurf", "mcp_config.json"},
	} {
		for _, machine := range []bool{false, true} {
			t.Run(string(tc.target)+"/machine="+fmt.Sprint(machine), func(t *testing.T) {
				home, repo := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				scope, root, source := repo, filepath.Join(repo, tc.project), config.RepoCommitted
				if machine {
					scope, root, source = home, filepath.Join(home, tc.machine), config.UserPersonal
				}
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatal(err)
				}
				payload := filepath.Join(scope, "handwritten.json")
				const original = `{"mcpServers":{"manual":{"command":"inert manual value"}},"theme":"preserve"}`
				if err := os.WriteFile(payload, []byte(original), 0o640); err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(payload)
				if err != nil {
					t.Fatal(err)
				}
				leaf := filepath.Join(root, tc.file)
				if err := os.Symlink(payload, leaf); err != nil {
					t.Fatal(err)
				}
				resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "safe-server", Resource: ir.NewMcpServer("safe-server", "inert-command", nil, nil, nil, ir.SourceRef{})}, Source: source, Harnesses: []hcc.Target{tc.target}}}
				report, err := Run(t.Context(), repo, resolved)
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, report.Diags)
				raw, err := os.ReadFile(leaf)
				if err != nil || !strings.Contains(string(raw), `"safe-server"`) || !strings.Contains(string(raw), `"manual"`) || !strings.Contains(string(raw), `"theme"`) {
					t.Fatalf("native merge lost desired or handwritten content: %q %v", raw, err)
				}
				outputBefore, err := os.Lstat(leaf)
				if err != nil || !outputBefore.Mode().IsRegular() || outputBefore.Mode().Perm() != 0o640 {
					t.Fatalf("native detached alias has wrong shape/mode: %v %v", outputBefore, err)
				}
				if raw, err := os.ReadFile(payload); err != nil || string(raw) != original {
					t.Fatalf("migration changed target bytes: %q %v", raw, err)
				}
				if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("migration changed target metadata: %v", err)
				}
				repeat, err := Run(t.Context(), repo, resolved)
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, repeat.Diags)
				if after, err := os.Stat(leaf); err != nil || !os.SameFile(outputBefore, after) || !outputBefore.ModTime().Equal(after.ModTime()) {
					t.Fatalf("repeat rewrote native merge: %v", err)
				}
			})
		}
	}
}

// spec: materialization/read-only-marker-merge-refuses-an-external-alias
func TestRunReadOnlyMarkerMergeRefusesExternalAlias(t *testing.T) {
	home, repo, external := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	root := filepath.Join(repo, ".factory")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(external, "private.json")
	if err := os.WriteFile(payload, []byte(`{"mcpServers":{}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(root, "mcp.json")
	if err := os.Symlink(payload, leaf); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(leaf)
	if err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "safe-server", Resource: ir.NewMcpServer("safe-server", "inert-command", nil, nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	report, err := Run(t.Context(), repo, resolved, WithDryRun())
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("read-only native merge bypassed alias admission: %+v", report.Diags)
	}
	if after, err := os.Lstat(leaf); err != nil || !os.SameFile(before, after) {
		t.Fatalf("read-only admission changed alias: %v", err)
	}
}
