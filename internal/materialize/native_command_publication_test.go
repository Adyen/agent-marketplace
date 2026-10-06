package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/native-command-publication-refuses-replacement-generation
func TestRunNativeCommandPublicationRefusesReplacedLeaf(t *testing.T) {
	testNativeCommandPublicationRefusal(t, false)
}

func TestRunNativeCommandPublicationRefusesReplacedStage(t *testing.T) {
	testNativeCommandPublicationRefusal(t, true)
}

func testNativeCommandPublicationRefusal(t *testing.T, replaceStage bool) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "publish-probe", Resource: ir.NewCommand("publish-probe", "publication", "original inert body", nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	initial, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("native command install failed: %+v %v", initial, err)
	}
	leaf := filepath.Join(repo, ".factory", "commands", "publish-probe.md")
	if err := os.Chmod(leaf, 0o640); err != nil {
		t.Fatal(err)
	}
	resolved[0].Resource = ir.NewCommand("publish-probe", "publication", "valid changed inert body", nil, ir.SourceRef{})
	updated, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(updated.Diags) {
		t.Fatalf("valid native command update failed: %+v %v", updated, err)
	}
	before, err := os.ReadFile(leaf)
	if err != nil || !bytes.Contains(before, []byte("valid changed inert body")) {
		t.Fatalf("native update control missing: %q %v", before, err)
	}
	beforeInfo, err := os.Stat(leaf)
	if err != nil || beforeInfo.Mode().Perm() != 0o640 {
		t.Fatalf("valid native update changed mode: %v", err)
	}
	noop, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(noop.Diags) {
		t.Fatalf("native command repeat failed: %+v %v", noop, err)
	}
	if after, err := os.Stat(leaf); err != nil || !os.SameFile(beforeInfo, after) || beforeInfo.Mode() != after.Mode() || !beforeInfo.ModTime().Equal(after.ModTime()) {
		t.Fatalf("native command repeat changed identity/mode/mtime: %v", err)
	}
	resolved[0].Resource = ir.NewCommand("publish-probe", "publication", "next inert body", nil, ir.SourceRef{})
	hit := false
	retained := filepath.Join(filepath.Dir(leaf), "retained.md")
	var foreignStage string
	stagedOriginal := filepath.Join(t.TempDir(), "staged-original.md")
	report, err := Run(t.Context(), repo, resolved, func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != leaf {
				return
			}
			hit = true
			if replaceStage {
				entries, err := os.ReadDir(filepath.Dir(leaf))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".agent-marketplace-portable-") {
						foreignStage = filepath.Join(filepath.Dir(leaf), entry.Name())
					}
				}
				if foreignStage == "" {
					t.Fatal("native staged publication did not prepare a file")
				}
				if err := os.Rename(foreignStage, stagedOriginal); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(foreignStage, []byte("foreign staged bytes"), 0o640); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err := os.Rename(leaf, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(leaf, []byte("handwritten replacement"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !hit {
		t.Fatal("actual native publication fixture did not run")
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("native replacement was not refused: %+v %v", report, err)
	}
	if replaceStage {
		if data, err := os.ReadFile(leaf); err != nil || !bytes.Equal(data, before) {
			t.Fatalf("staged replacement changed admitted output: %q %v", data, err)
		}
		if data, err := os.ReadFile(foreignStage); err != nil || string(data) != "foreign staged bytes" {
			t.Fatalf("attempt cleanup removed foreign stage: %q %v", data, err)
		}
		if data, err := os.ReadFile(stagedOriginal); err != nil || !bytes.Contains(data, []byte("next inert body")) {
			t.Fatalf("staged native payload control missing: %q %v", data, err)
		}
		return
	}
	if data, err := os.ReadFile(leaf); err != nil || string(data) != "handwritten replacement" {
		t.Fatalf("native publisher replaced user bytes: %q %v", data, err)
	}
	if data, err := os.ReadFile(retained); err != nil || !bytes.Equal(data, before) {
		t.Fatalf("admitted native bytes changed: %q %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(leaf))
	if err != nil || len(entries) != 2 {
		t.Fatalf("native refusal staging residue: %v %v", entries, err)
	}
}
