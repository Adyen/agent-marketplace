package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/read-only-portable-source-hash-refuses-foreign-aliases
func TestRunPortableSourceHashRefusesForeignAlias(t *testing.T) {
	home, repo, source, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("inert source"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(outside, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("foreign inert bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(source, "reference.txt")
	if err := os.Symlink(foreign, alias); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "hash-probe", Path: source, Resource: ir.NewSkill("hash-probe", "hash control", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}), WithDryRun())
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("read-only source hash accepted foreign alias: %+v", report.Diags)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "foreign inert bytes" {
		t.Fatalf("source hash changed foreign data: %q %v", data, err)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("source hash changed foreign metadata: %v", err)
	}
	if target, err := os.Readlink(alias); err != nil || target != foreign {
		t.Fatalf("source hash changed alias: %q %v", target, err)
	}
	for _, path := range []string{filepath.Join(repo, ".factory"), filepath.Join(repo, ".agents"), filepath.Join(home, "cache")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("read-only source refusal created state at %s: %v", path, err)
		}
	}
}

// spec: materialization/contained-portable-source-hashes-retain-native-ownership
func TestRunPortableSourceHashContainedAliasRetainsNativeOwnership(t *testing.T) {
	home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("inert source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(source, "scripts", "helper.txt")
	if err := os.WriteFile(payload, []byte("inert executable data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("scripts/helper.txt", filepath.Join(source, "reference.txt")); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "hash-probe", Path: source, Resource: ir.NewSkill("hash-probe", "hash control", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
	initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, initial.Diags)
	installed := filepath.Join(repo, ".factory", "skills", "hash-probe", "reference.txt")
	if data, err := os.ReadFile(installed); err != nil || string(data) != "inert executable data" {
		t.Fatalf("native positive control missing: %q %v", data, err)
	}
	if info, err := os.Stat(installed); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("native support mode changed: %v", err)
	}
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	before, err := os.Stat(manifest)
	if err != nil {
		t.Fatal(err)
	}
	check, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes), WithDryRun())
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, check.Diags)
	if !check.InSync() {
		t.Fatalf("contained source hash drifted from native ownership: %+v", check)
	}
	if after, err := os.Stat(manifest); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("read-only source hash changed manifest: %v", err)
	}
}
