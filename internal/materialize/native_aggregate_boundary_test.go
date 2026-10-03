package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/native-marker-aggregate-refuses-replacement
func TestRunNativeMarkerAggregateRefusesReplacedLeaf(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "repository", true: "machine"}[machine], func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			source, file := config.RepoCommitted, filepath.Join(repo, ".mcp.json")
			if machine {
				source, file = config.UserPersonal, filepath.Join(home, ".claude.json")
			}
			if err := os.WriteFile(file, []byte("{\n // handwritten comment\n \"custom\": true,\n \"mcpServers\": {\"user\": {\"command\": \"inert\"}}\n}\n"), 0o640); err != nil {
				t.Fatal(err)
			}
			desired := nativeMCPResources(hcc.TargetClaude, source)
			initial, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(initial.Diags) {
				t.Fatalf("native aggregate install failed: %+v %v", initial, err)
			}
			desired[0].Resource = ir.NewMcpServer("first", "inert-update", []string{"updated"}, nil, nil, ir.SourceRef{})
			updated, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(updated.Diags) {
				t.Fatalf("native aggregate positive update failed: %+v %v", updated, err)
			}
			before, err := os.ReadFile(file)
			if err != nil || !bytes.Contains(before, []byte("inert-update")) || !bytes.Contains(before, []byte("handwritten comment")) {
				t.Fatalf("aggregate positive control absent: %q %v", before, err)
			}
			document := nativeMCPDocument(t, before)
			if document["custom"] != true || document["mcpServers"].(map[string]any)["user"].(map[string]any)["command"] != "inert" {
				t.Fatalf("native aggregate lost handwritten settings: %v", document)
			}
			beforeInfo, err := os.Stat(file)
			if err != nil || beforeInfo.Mode().Perm() != 0o640 {
				t.Fatalf("aggregate update mode changed: %v", err)
			}
			noop, err := Run(t.Context(), repo, desired)
			if err != nil || diag.HasErrors(noop.Diags) {
				t.Fatalf("aggregate repeat failed: %+v %v", noop, err)
			}
			if after, err := os.Stat(file); err != nil || !os.SameFile(beforeInfo, after) || beforeInfo.Mode() != after.Mode() || !beforeInfo.ModTime().Equal(after.ModTime()) {
				t.Fatalf("aggregate no-op metadata changed: %v", err)
			}
			desired[0].Resource = ir.NewMcpServer("first", "next-inert-update", nil, nil, nil, ir.SourceRef{})
			retained := filepath.Join(t.TempDir(), "retained.json")
			hit := false
			report, err := Run(t.Context(), repo, desired, func(cfg *runConfig) {
				cfg.linkPublicationHook = func(path string) {
					if path != file {
						return
					}
					hit = true
					if err := os.Rename(file, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte("{\"foreign\":\"inert user bytes\"}\n"), 0o640); err != nil {
						t.Fatal(err)
					}
				}
			})
			if !hit {
				t.Fatal("actual native aggregate publication fixture did not run")
			}
			if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
				t.Fatalf("native aggregate generation replaced without refusal: %+v %v", report, err)
			}
			if data, err := os.ReadFile(file); err != nil || string(data) != "{\"foreign\":\"inert user bytes\"}\n" {
				t.Fatalf("native aggregate replaced foreign bytes: %q %v", data, err)
			}
			if data, err := os.ReadFile(retained); err != nil || !bytes.Equal(data, before) {
				t.Fatalf("admitted aggregate changed: %q %v", data, err)
			}
		})
	}
}
