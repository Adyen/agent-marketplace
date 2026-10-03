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

// spec: materialization/contained-support-directory-aliases-retain-native-install-and-check
func TestRunContainedSupportDirectoryAlias(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeFile, config.MaterializationModeSymlink} {
		t.Run(string(mode), func(t *testing.T) {
			home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			if err := os.MkdirAll(filepath.Join(source, "scripts", ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{"SKILL.md": "inert source", "scripts/helper.txt": "inert executable data", "scripts/.git/private.txt": "excluded VCS bytes"} {
				if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for name, target := range map[string]string{"references": "scripts", "scripts/file-alias.txt": "helper.txt", "second-alias": "references"} {
				if err := os.Symlink(target, filepath.Join(source, name)); err != nil {
					t.Fatal(err)
				}
			}
			resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "alias-probe", Path: source, Resource: ir.NewSkill("alias-probe", "alias control", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
			modes := config.MaterializationModes{Project: mode, Machine: config.MaterializationModeSymlink}
			if mode == config.MaterializationModeFile {
				check, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes), WithDryRun())
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, check.Diags)
				for _, path := range []string{filepath.Join(repo, ".factory"), filepath.Join(repo, ".agents"), filepath.Join(home, "cache")} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Fatalf("initial check wrote %s: %v", path, err)
					}
				}
			}
			initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, initial.Diags)
			installed := filepath.Join(repo, ".factory", "skills", "alias-probe")
			for _, directory := range []string{"scripts", "references", "second-alias"} {
				for _, leaf := range []string{"helper.txt", "file-alias.txt"} {
					path := filepath.Join(installed, directory, leaf)
					if data, err := os.ReadFile(path); err != nil || string(data) != "inert executable data" {
						t.Fatalf("native alias payload missing: %q %v", data, err)
					}
					if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 {
						t.Fatalf("alias payload is not a regular executable snapshot: %v", err)
					}
				}
				if _, err := os.Lstat(filepath.Join(installed, directory, ".git")); !os.IsNotExist(err) {
					t.Fatalf("directory alias copied VCS internals: %v", err)
				}
			}
			payload := filepath.Join(installed, "references", "helper.txt")
			before, err := os.Stat(payload)
			if err != nil {
				t.Fatal(err)
			}
			check, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes), WithDryRun())
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, check.Diags)
			if !check.InSync() {
				t.Fatalf("contained directory alias drifted: %+v", check)
			}
			if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("read-only check changed the alias payload: %v", err)
			}
			repeat, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, repeat.Diags)
			if after, err := os.Stat(payload); err != nil || before.Mode() != after.Mode() || mode == config.MaterializationModeFile && (!os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime())) {
				t.Fatalf("alias payload repeat changed mode or file-mode identity: %v", err)
			}
			if data, err := os.ReadFile(payload); err != nil || string(data) != "inert executable data" {
				t.Fatalf("repeat changed alias payload: %q %v", data, err)
			}
		})
	}
}

// spec: materialization/unsafe-support-directory-aliases-preserve-the-installed-generation
func TestRunUnsafeSupportDirectoryAlias(t *testing.T) {
	for _, kind := range []string{"external", "dangling", "cycle", "external-return", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			home, repo, source, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			if err := os.Mkdir(filepath.Join(source, "scripts"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "scripts", "helper.txt"), []byte("safe support"), 0o755); err != nil {
				t.Fatal(err)
			}
			foreign := filepath.Join(outside, "foreign.txt")
			if err := os.WriteFile(foreign, []byte("foreign inert bytes"), 0o640); err != nil {
				t.Fatal(err)
			}
			foreignBefore, err := os.Stat(foreign)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(source, "references")
			if err := os.Symlink("scripts", alias); err != nil {
				t.Fatal(err)
			}
			resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "alias-probe", Path: source, Resource: ir.NewSkill("alias-probe", "alias control", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
			modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeSymlink}
			initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, initial.Diags)
			installed := filepath.Join(repo, ".factory", "skills", "alias-probe", "references", "helper.txt")
			before, err := os.Stat(installed)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			target := outside
			switch kind {
			case "dangling":
				target = "missing"
			case "cycle":
				target = "."
			case "external-return":
				target = filepath.Join(outside, "return")
				if err := os.Symlink(filepath.Join(source, "scripts"), target); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				target = "scripts"
				if err := os.Link(filepath.Join(source, "scripts", "helper.txt"), filepath.Join(source, "scripts", "second.txt")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, alias); err != nil {
				t.Fatal(err)
			}
			for _, dryRun := range []bool{true, false} {
				opts := []RunOption{WithMaterializationModes(modes)}
				if dryRun {
					opts = append(opts, WithDryRun())
				}
				report, err := Run(t.Context(), repo, resolved, opts...)
				if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
					t.Fatalf("unsafe directory alias did not refuse: %+v %v", report, err)
				}
				if data, err := os.ReadFile(installed); err != nil || string(data) != "safe support" {
					t.Fatalf("refusal changed native bytes: %q %v", data, err)
				}
				if after, err := os.Stat(installed); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("refusal changed installed generation: %v", err)
				}
			}
			if data, err := os.ReadFile(foreign); err != nil || string(data) != "foreign inert bytes" {
				t.Fatalf("refusal changed foreign bytes: %q %v", data, err)
			}
			if after, err := os.Stat(foreign); err != nil || !os.SameFile(foreignBefore, after) || foreignBefore.Mode() != after.Mode() || !foreignBefore.ModTime().Equal(after.ModTime()) {
				t.Fatalf("refusal changed foreign metadata: %v", err)
			}
			for _, scope := range []string{home, repo} {
				if err := filepath.WalkDir(scope, func(path string, entry os.DirEntry, err error) error {
					if err == nil && strings.HasPrefix(entry.Name(), ".support-staging-") {
						t.Errorf("source refusal left staging residue: %s", path)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
		})
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
