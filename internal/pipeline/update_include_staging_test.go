package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

func TestUpdateBumpUsesDeclaringRootForIncludes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rootRef bool
		linked  bool
	}{
		{name: "included_only"},
		{name: "root_ref_control", rootRef: true},
		{name: "linked_root_ref_control", rootRef: true, linked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			includedURL := updateIncludeTaggedRegistry(t, "included")
			rootURL := updateIncludeTaggedRegistry(t, "root")
			repo := t.TempDir()
			rootPath := config.ProjectConfigPath(repo)
			fragmentPath := filepath.Join(filepath.Dir(rootPath), "shared.yaml")
			fragment := fmt.Sprintf("# Shared refs are not CLI-editable.\nsources:\n  shared: {git: '%s', ref: v1.0}\nresources:\n  shared: {include: [skill/included]}\n", includedURL)
			root := "apiVersion: agent-marketplace/v4\n# Keep this relative include in its declaring directory.\ninclude: shared.yaml\n"
			if tc.rootRef {
				root += fmt.Sprintf("sources:\n  owned: {git: '%s', ref: v1.0}\nresources:\n  owned: {include: [skill/root]}\n", rootURL)
			}
			writeFileMode(t, fragmentPath, fragment, 0o640)
			authoredPath := rootPath
			if tc.linked {
				authoredPath = filepath.Join(filepath.Dir(rootPath), "authored.yaml")
			}
			writeFileMode(t, authoredPath, root, 0o600)
			if tc.linked {
				if err := os.Symlink("authored.yaml", rootPath); err != nil {
					t.Fatal(err)
				}
			}
			rootBytes, rootStat := layerFreezeFile(t, authoredPath)
			fragmentBytes, fragmentStat := layerFreezeFile(t, fragmentPath)
			intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{RepoRoot: repo, LocalConfigDir: repo})
			if len(intent.Errors) != 0 {
				t.Fatalf("read authored include tree: %v", intent.Errors)
			}
			registries := GitRegistryEntries(intent.Layers[config.RepoCommitted].Sources())
			wantRegistries := 1
			if tc.rootRef {
				wantRegistries++
			}
			if len(registries) != wantRegistries {
				t.Fatalf("registries = %#v", registries)
			}
			staging, err := newConfigStagingManager()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := staging.Close(); err != nil {
					t.Error(err)
				}
			})
			v4, err := config.LoadV4ConfigIfPresent(rootPath, config.RepoCommitted)
			if err != nil || v4 == nil {
				t.Fatalf("load declaring root before staging: %v", err)
			}
			shadow, err := staging.Path(rootPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(shadow), "shared.yaml")); !os.IsNotExist(err) {
				t.Fatalf("fragment must not be copied into staging: %v", err)
			}
			layerTarget := config.LayerTarget{Source: config.RepoCommitted, StagedPath: shadow, DeclaringPath: rootPath}
			bumps, skipped, err := bumpRepoRegistryRefs(context.Background(), layerTarget, v4, &config.Config{Registries: registries}, nil)
			if err != nil {
				t.Fatalf("bump authored include tree through staging: %v", err)
			}
			wantSkipped := []RefBump{{URL: includedURL, OldRef: "v1.0", NewRef: "v1.1"}}
			if !reflect.DeepEqual(skipped, wantSkipped) {
				t.Fatalf("skipped = %#v, want %#v", skipped, wantSkipped)
			}
			var wantBumps []RefBump
			if tc.rootRef {
				wantBumps = []RefBump{{URL: rootURL, OldRef: "v1.0", NewRef: "v1.1"}}
			}
			if len(bumps) != len(wantBumps) || (len(bumps) != 0 && !reflect.DeepEqual(bumps, wantBumps)) {
				t.Fatalf("bumps = %#v, want %#v", bumps, wantBumps)
			}
			edits, err := staging.Edits()
			if err != nil {
				t.Fatal(err)
			}
			canonicalDir, err := filepath.EvalSymlinks(filepath.Dir(rootPath))
			if err != nil {
				t.Fatal(err)
			}
			// Managed publication retains the logical leaf, even when it is
			// an approved config link; the descriptor boundary honors its target.
			canonicalRoot := filepath.Join(canonicalDir, "config.yaml")
			if len(edits) != 1 || edits[0].RequestedPath != rootPath || edits[0].Path != canonicalRoot || edits[0].Expected.Digest != dependencytxn.Digest(rootBytes) || edits[0].Mode != 0o600 {
				t.Fatalf("staged publication lost root provenance: %#v", edits)
			}
			stagedBytes := string(edits[0].Bytes)
			if tc.rootRef {
				if !strings.Contains(stagedBytes, "ref: v1.1") || strings.Contains(stagedBytes, "v1.0") || !strings.Contains(stagedBytes, "include: shared.yaml") || !strings.Contains(stagedBytes, "# Keep this relative include") {
					t.Fatalf("root ref did not advance without relocating its include:\n%s", stagedBytes)
				}
			} else if stagedBytes != root {
				t.Fatalf("included-only bump changed root bytes:\n%s", stagedBytes)
			}
			layerAssertFileUnchanged(t, authoredPath, rootBytes, rootStat)
			layerAssertFileUnchanged(t, fragmentPath, fragmentBytes, fragmentStat)
			edit := edits[0]
			stateDir, err := dependencytxn.StateDir()
			if err != nil {
				t.Fatal(err)
			}
			reads, err := IntentReadAssertions(intent)
			if err != nil {
				t.Fatal(err)
			}
			committed, err := dependencytxn.Commit(context.Background(), stateDir, []dependencytxn.DesiredFile{{
				Path: edit.Path, Expected: edit.Expected, ExpectedMode: edit.Mode, Bytes: edit.Bytes, Mode: edit.Mode,
			}}, dependencytxn.WithReadAssertions(reads))
			if err != nil || (len(committed.Changed) != 0) != tc.rootRef {
				t.Fatalf("publish captured root edit: %#v, %v", committed, err)
			}
			published, err := config.LoadV4ConfigIfPresent(rootPath, config.RepoCommitted)
			if err != nil || published == nil || published.Sources["shared"].Ref != "v1.0" {
				t.Fatalf("published include ref = %#v, %v", published, err)
			}
			if tc.rootRef {
				if published.Sources["owned"].Ref != "v1.1" {
					t.Fatalf("root ref did not advance at publication: %#v", published.Sources)
				}
			} else {
				layerAssertFileUnchanged(t, authoredPath, rootBytes, rootStat)
			}
			layerAssertFileUnchanged(t, fragmentPath, fragmentBytes, fragmentStat)
			if tc.linked {
				if text, err := os.Readlink(rootPath); err != nil || text != "authored.yaml" {
					t.Fatalf("config link changed: %q, %v", text, err)
				}
			}
			if err := staging.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(staging.root); !os.IsNotExist(err) {
				t.Fatalf("staging residue remains: %v", err)
			}
		})
	}
}

func TestUpdateBumpStagingRevalidatesIncludedFragments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	rootPath := config.ProjectConfigPath(repo)
	fragmentPath := filepath.Join(filepath.Dir(rootPath), "shared.yaml")
	root := "apiVersion: agent-marketplace/v4\ninclude: shared.yaml\nsources:\n  owned: {git: https://example.test/root.git, ref: v1.0}\nresources:\n  owned: {include: [skill/root]}\n"
	writeFileMode(t, rootPath, root, 0o600)
	writeFileMode(t, fragmentPath, "sources: {}\n", 0o640)
	if _, err := config.LoadV4ConfigIfPresent(rootPath, config.RepoCommitted); err != nil {
		t.Fatal(err)
	}
	staging, err := newConfigStagingManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := staging.Close(); err != nil {
			t.Error(err)
		}
	})
	shadow, err := staging.Path(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, fragmentPath, "apiVersion: agent-marketplace/v3\nsources: {}\n", 0o640)
	rootBytes, rootStat := layerFreezeFile(t, rootPath)
	fragmentBytes, fragmentStat := layerFreezeFile(t, fragmentPath)
	err = stageRootRegistryRefs(config.LayerTarget{Source: config.RepoCommitted, StagedPath: shadow, DeclaringPath: rootPath}, map[string]RefBump{
		"owned": {URL: "https://example.test/root.git", OldRef: "v1.0", NewRef: "v1.1"},
	})
	var validation *config.V4ValidationError
	canonicalFragment, pathErr := filepath.EvalSymlinks(fragmentPath)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if !errors.As(err, &validation) || validation.Code != "v4.api-version" || validation.Path != canonicalFragment {
		t.Fatalf("included-fragment refusal = %v, want v4.api-version at %s", err, canonicalFragment)
	}
	stagedBytes, err := os.ReadFile(shadow)
	if err != nil || string(stagedBytes) != root {
		t.Fatalf("invalid include changed staged root: %q, %v", stagedBytes, err)
	}
	layerAssertFileUnchanged(t, rootPath, rootBytes, rootStat)
	layerAssertFileUnchanged(t, fragmentPath, fragmentBytes, fragmentStat)
}

func updateIncludeTaggedRegistry(t *testing.T, name string) string {
	t.Helper()
	origin := t.TempDir()
	writeFileMode(t, filepath.Join(origin, "skills", name, "SKILL.md"), "---\nname: "+name+"\ndescription: "+name+"\n---\n", 0o644)
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"add", "."},
		{"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed"},
		{"tag", "v1.0"},
		{"tag", "v1.1"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = origin
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	return "file://" + origin
}
