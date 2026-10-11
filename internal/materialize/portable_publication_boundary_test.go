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

// spec: materialization/portable-file-publication-refuses-replaced-leaf-generation
func TestRunPortableFilePublicationRefusesReplacedLeaf(t *testing.T) {
	testRunPortableFilePublicationRefusal(t, false)
}

func TestRunPortableFilePublicationRefusesReplacedParent(t *testing.T) {
	testRunPortableFilePublicationRefusal(t, true)
}

func testRunPortableFilePublicationRefusal(t *testing.T, replaceParent bool) {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resource := ir.NewSubagent("publication-probe", "native publication", "original inert body", "sonnet", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "publication-probe", Resource: resource}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
	initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("portable native control failed: %+v %v", initial, err)
	}
	leaf := filepath.Join(repo, ".factory", "droids", "publication-probe.md")
	original, err := os.ReadFile(leaf)
	if err != nil || !bytes.Contains(original, []byte("original inert body")) {
		t.Fatalf("native positive control missing: %q %v", original, err)
	}
	resource = ir.NewSubagent("publication-probe", "native publication", "valid updated inert body", "sonnet", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved[0].Resource = resource
	updated, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(updated.Diags) {
		t.Fatalf("valid native update failed: %+v %v", updated, err)
	}
	original, err = os.ReadFile(leaf)
	if err != nil || !bytes.Contains(original, []byte("valid updated inert body")) {
		t.Fatalf("valid content update did not occur: %q %v", original, err)
	}
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	manifestBefore, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	resource = ir.NewSubagent("publication-probe", "native publication", "refused update inert body", "sonnet", []string{"Read"}, "", nil, "", nil, ir.SourceRef{})
	resolved[0].Resource = resource
	hit := false
	retained := filepath.Join(filepath.Dir(leaf), "retained.md")
	retainedParent := filepath.Join(filepath.Dir(filepath.Dir(leaf)), "retained-droids")
	foreignParent := t.TempDir()
	foreignLeaf := filepath.Join(foreignParent, filepath.Base(leaf))
	if err := os.WriteFile(foreignLeaf, []byte("foreign handwritten control"), 0o640); err != nil {
		t.Fatal(err)
	}
	report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes), func(cfg *runConfig) {
		cfg.linkPublicationHook = func(path string) {
			if path != leaf {
				return
			}
			hit = true
			if replaceParent {
				if err := os.Rename(filepath.Dir(leaf), retainedParent); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreignParent, filepath.Dir(leaf)); err != nil {
					t.Fatal(err)
				}
				retained = filepath.Join(retainedParent, filepath.Base(leaf))
			} else {
				if err := os.Rename(leaf, retained); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(leaf, []byte("handwritten replacement"), 0o640); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
	if !hit {
		t.Fatal("native portable staged-publication fixture did not run")
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("portable replacement did not refuse: %+v %v", report, err)
	}
	want := "handwritten replacement"
	if replaceParent {
		want = "foreign handwritten control"
	}
	if data, err := os.ReadFile(leaf); err != nil || string(data) != want {
		t.Fatalf("publication overwrote replacement: %q %v", data, err)
	}
	if data, err := os.ReadFile(retained); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("publication changed admitted original: %q %v", data, err)
	}
	if data, err := os.ReadFile(foreignLeaf); err != nil || string(data) != "foreign handwritten control" {
		t.Fatalf("publication changed foreign control: %q %v", data, err)
	}
	if data, err := os.ReadFile(manifest); err != nil || !bytes.Equal(data, manifestBefore) {
		t.Fatalf("refusal changed manifest: %q %v", data, err)
	}
	for _, parent := range []string{filepath.Dir(leaf), filepath.Dir(retained)} {
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".agent-marketplace-portable-") {
				t.Fatalf("staged residue in %s: %s", parent, entry.Name())
			}
		}
	}
}
