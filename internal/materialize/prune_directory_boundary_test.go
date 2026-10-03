package materialize

import (
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

// spec: materialization/prune-directory-refusal-precedes-foreign-discovery
func TestRunPruneDirectoryRefusesRetargetBeforeDiscovery(t *testing.T) {
	testPruneDirectoryRefusal(t, false)
}

func TestRunPruneDirectoryDryRunRefusesRetarget(t *testing.T) { testPruneDirectoryRefusal(t, true) }

func testPruneDirectoryRefusal(t *testing.T, dryRun bool) {
	home, repo, foreign := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "directory-probe", Resource: ir.NewSkill("directory-probe", "directory", "inert installed payload", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	installed, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(installed.Diags) {
		t.Fatalf("native directory install control failed: %+v %v", installed, err)
	}
	dir := filepath.Join(repo, ".factory", "skills")
	if _, err := os.Readlink(filepath.Join(dir, "directory-probe")); err != nil {
		t.Fatal(err)
	}
	foreignName := "foreign-discovery-canary.md"
	if err := os.WriteFile(filepath.Join(foreign, foreignName), []byte("inert handwritten foreign bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(repo, ".factory", "retained-skills")
	hit := false
	healthy, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(healthy.Diags) {
		t.Fatalf("healthy native repeat failed: %+v %v", healthy, err)
	}
	report, err := Run(t.Context(), repo, resolved, func(cfg *runConfig) {
		cfg.dryRun = dryRun
		cfg.linkPublicationHook = func(path string) {
			if path != dir {
				return
			}
			hit = true
			if err := os.Rename(dir, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(foreign, dir); err != nil {
				t.Fatal(err)
			}
		}
	})
	if !hit {
		t.Fatal("actual native prune-directory fixture did not run")
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("directory retarget did not refuse before discovery: %+v %v", report, err)
	}
	for _, diagnostic := range report.Diags {
		if strings.Contains(diagnostic.Message, foreignName) {
			t.Fatalf("foreign directory member entered native diagnostics: %+v", diagnostic)
		}
	}
	if data, err := os.ReadFile(filepath.Join(foreign, foreignName)); err != nil || string(data) != "inert handwritten foreign bytes" {
		t.Fatalf("foreign bytes changed: %q %v", data, err)
	}
	if _, err := os.Readlink(filepath.Join(retained, "directory-probe")); err != nil {
		t.Fatalf("admitted native link changed: %v", err)
	}
}
