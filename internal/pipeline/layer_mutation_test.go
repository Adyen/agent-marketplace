package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestAddResourcesToAllRegistriesIncludesOnlyMatchingProviders(t *testing.T) {
	_, configPath, _ := mutateLayerFixture(t)
	writeMutateLayerConfig(t, configPath, "apiVersion: agent-marketplace/v2\nharnesses: [claude]\nregistries:\n  - url: https://a.example/repo.git\n    ref: main\n  - url: https://b.example/repo.git\n    ref: main\n")
	entries := []config.RegistryEntry{
		{URL: "https://a.example/repo.git", Ref: "main", CatalogKey: "cat-a"},
		{URL: "https://b.example/repo.git", Ref: "main", CatalogKey: "cat-b"},
	}
	catalogs := map[string]catalog.LoadedCatalog{
		"cat-a": {Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "alpha"}}},
		"cat-b": {Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "beta"}}},
	}
	target := config.LayerTarget{Source: config.RepoCommitted, StagedPath: configPath, DeclaringPath: configPath}
	if err := editLayerAtPath(target, func(m *LayerMutation) error {
		return addResourcesToAllRegistries(m, entries, catalogs, ir.KindSkill, []string{"alpha"}, nil)
	}); err != nil {
		t.Fatalf("addResourcesToAllRegistries: %v", err)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("registries = %#v", cfg.Registries)
	}
	if got := cfg.Registries[0].Skills.Include; len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("first registry skills = %v", got)
	}
	if got := cfg.Registries[1].Skills.Include; len(got) != 0 {
		t.Fatalf("unmatched provider gained selection: %v", got)
	}
	assertNoStagingResidue(t)
}

func TestRemovalEditEntry(t *testing.T) {
	base := config.RegistryEntry{Ref: "main", Path: "skills", V4Kind: config.V4SourceDirectory, SourceHarness: "claude", CatalogKey: "cat"}
	got := removalEditEntry(base, "file:///tmp/bundle", "sub/dir", "dev")
	if got.URL != "file:///tmp/bundle" || got.Path != "sub/dir" || got.Ref != "dev" {
		t.Fatalf("fill = %#v", got)
	}
	if got.V4Kind != config.V4SourceDirectory || got.SourceHarness != "claude" || got.CatalogKey != "cat" {
		t.Fatalf("identity fields lost: %#v", got)
	}
	if base.URL != "" || base.Path != "skills" || base.Ref != "main" {
		t.Fatalf("input mutated: %#v", base)
	}
	got = removalEditEntry(config.RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}, "https://other.test/x.git", "", "")
	if got.URL != "https://example.test/repo.git" || got.Ref != "main" || got.Path != "" {
		t.Fatalf("explicit fields overwritten: %#v", got)
	}
}

func mutateLayerFixture(t *testing.T) (repoRoot, configPath, stateDir string) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	base := canonicalTempDir(t)
	home := filepath.Join(base, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	repoRoot = filepath.Join(base, "repo")
	configPath = filepath.Join(repoRoot, ".agents", "config.yaml")
	stateDir = filepath.Join(base, "state")
	return repoRoot, configPath, stateDir
}

func writeMutateLayerConfig(t *testing.T, configPath, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
}

func assertNoStagingResidue(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".agent-marketplace-config-") {
			t.Fatalf("staging residue %s in %s", entry.Name(), os.TempDir())
		}
	}
}

func commitStagedEdits(stateDir string, extra ...dependencytxn.DesiredFile) DependencyReconciler {
	return func(ctx context.Context, _, _ string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
		desired := make([]dependencytxn.DesiredFile, 0, len(edits)+len(extra))
		for _, edit := range edits {
			desired = append(desired, dependencytxn.DesiredFile{
				Path:         edit.Path,
				Expected:     edit.Expected,
				ExpectedMode: edit.Mode,
				Bytes:        edit.Bytes,
				Mode:         edit.Mode,
			})
		}
		desired = append(desired, extra...)
		_, err := dependencytxn.Commit(ctx, stateDir, desired)
		return DependencyLockView{}, err
	}
}

func includeSkillAlpha(m *LayerMutation) error {
	binding, err := m.EnsureRegistry(config.RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"})
	if err != nil {
		return err
	}
	return m.Apply(binding, config.SelectionChange{Operation: config.SelectionInclude, Locators: []config.ResourceLocator{{Kind: ir.KindSkill, Name: "alpha"}}})
}

func repoLockDesired(t *testing.T, configPath string) dependencytxn.DesiredFile {
	t.Helper()
	lockPath, err := lockfile.LockPathForConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBytes, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo})
	if err != nil {
		t.Fatal(err)
	}
	generation, mode, err := dependencytxn.ReadGenerationAndMode(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	desired := dependencytxn.DesiredFile{Path: lockPath, Expected: generation, Bytes: lockBytes, Mode: 0o600}
	if generation.Exists {
		desired.ExpectedMode = mode
		desired.Mode = mode
	}
	return desired
}

func TestMutateLayerPublishesConfigAndLockAtomically(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture string
		v4      bool
	}{
		{name: "v2", fixture: "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n"},
		{name: "v4", fixture: "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n", v4: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, configPath, stateDir := mutateLayerFixture(t)
			writeMutateLayerConfig(t, configPath, tc.fixture)

			lockPath, err := lockfile.LockPathForConfig(configPath)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			reconcile := commitStagedEdits(stateDir, repoLockDesired(t, configPath))
			wrapped := func(ctx context.Context, root, local string, locked bool, edits []StagedConfigEdit) (DependencyLockView, error) {
				calls++
				if root != repoRoot {
					t.Fatalf("reconcile root = %q", root)
				}
				if len(edits) != 1 {
					t.Fatalf("edits = %#v", edits)
				}
				if edits[0].Path != configPath || edits[0].RequestedPath != configPath {
					t.Fatalf("edit paths = %q %q", edits[0].Path, edits[0].RequestedPath)
				}
				if edits[0].Mode != 0o640 {
					t.Fatalf("edit mode = %o", edits[0].Mode)
				}
				return reconcile(ctx, root, local, locked, edits)
			}
			err = MutateLayer(context.Background(), LayerMutationTarget{
				RepoRoot:   repoRoot,
				ConfigPath: configPath,
				Source:     config.RepoCommitted,
				Reconcile:  wrapped,
			}, includeSkillAlpha)
			if err != nil {
				t.Fatalf("MutateLayer: %v", err)
			}
			if calls != 1 {
				t.Fatalf("reconcile calls = %d", calls)
			}

			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.v4 {
				cfg, err := config.LoadV4ConfigFromPath(configPath)
				if err != nil {
					t.Fatalf("published config invalid: %v\n%s", err, data)
				}
				var locators []string
				for _, r := range cfg.Groups["one"].Include {
					locators = append(locators, r.Locator)
				}
				if len(locators) != 1 || locators[0] != "skill/alpha" {
					t.Fatalf("v4 include = %v\n%s", locators, data)
				}
			} else {
				cfg, err := config.LoadConfigFromPath(configPath)
				if err != nil {
					t.Fatalf("published config invalid: %v\n%s", err, data)
				}
				if len(cfg.Registries) != 1 {
					t.Fatalf("registries = %#v", cfg.Registries)
				}
				selector := cfg.Registries[0].SelectorForKind(ir.KindSkill)
				if len(selector.Include) != 1 || selector.Include[0] != "alpha" {
					t.Fatalf("legacy include = %v\n%s", selector.Include, data)
				}
			}
			info, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o640 {
				t.Fatalf("config mode = %o", info.Mode().Perm())
			}
			lockData, err := lockfile.ReadBytes(lockPath)
			if err != nil {
				t.Fatalf("lock: %v", err)
			}
			lock, err := lockfile.Decode(lockData)
			if err != nil {
				t.Fatalf("lock decode: %v", err)
			}
			if lock.APIVersion != lockfile.APIVersionV2 || lock.Scope != lockfile.ScopeRepo {
				t.Fatalf("lock = %v %v", lock.APIVersion, lock.Scope)
			}
			assertNoStagingResidue(t)
		})
	}
}

func TestMutateLayerNoOpPreservesConfigBytesModeAndMtime(t *testing.T) {
	repoRoot, configPath, stateDir := mutateLayerFixture(t)
	fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: [skill/alpha]\n"
	writeMutateLayerConfig(t, configPath, fixture)
	before, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := lockfile.LockPathForConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockBytes, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	err = MutateLayer(context.Background(), LayerMutationTarget{
		RepoRoot:   repoRoot,
		ConfigPath: configPath,
		Source:     config.RepoCommitted,
		Reconcile:  commitStagedEdits(stateDir, repoLockDesired(t, configPath)),
	}, includeSkillAlpha)
	if err != nil {
		t.Fatalf("MutateLayer: %v", err)
	}
	after, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != fixture || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("no-op changed destination: mode %o->%o mtime %v->%v", before.Mode(), after.Mode(), before.ModTime(), after.ModTime())
	}
	lockAfter, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !lockAfter.ModTime().Equal(lockBefore.ModTime()) {
		t.Fatal("identical lock rewritten")
	}
	assertNoStagingResidue(t)
}

func TestMutateLayerCallbackFailureDoesNotReconcileOrPublish(t *testing.T) {
	repoRoot, configPath, _ := mutateLayerFixture(t)
	fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n"
	writeMutateLayerConfig(t, configPath, fixture)
	sentinel := errors.New("callback failed")
	called := false
	err := MutateLayer(context.Background(), LayerMutationTarget{
		RepoRoot:   repoRoot,
		ConfigPath: configPath,
		Source:     config.RepoCommitted,
		Reconcile: func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error) {
			called = true
			return DependencyLockView{}, nil
		},
	}, func(m *LayerMutation) error {
		if _, err := m.EnsureRegistry(config.RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}); err != nil {
			t.Fatal(err)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("reconciler called after callback failure")
	}
	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != fixture {
		t.Fatalf("config changed: %v", err)
	}
	lockPath, err := lockfile.LockPathForConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock exists after failure: %v", err)
	}
	assertNoStagingResidue(t)
}

func TestMutateLayerReconcileFailureDoesNotPublish(t *testing.T) {
	repoRoot, configPath, _ := mutateLayerFixture(t)
	fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n"
	writeMutateLayerConfig(t, configPath, fixture)
	sentinel := errors.New("reconcile failed")
	err := MutateLayer(context.Background(), LayerMutationTarget{
		RepoRoot:   repoRoot,
		ConfigPath: configPath,
		Source:     config.RepoCommitted,
		Reconcile: func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error) {
			return DependencyLockView{}, sentinel
		},
	}, includeSkillAlpha)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != fixture {
		t.Fatalf("config changed: %v", err)
	}
	assertNoStagingResidue(t)
}

func TestMutateLayerGenerationConflictPreservesConcurrentWriter(t *testing.T) {
	repoRoot, configPath, stateDir := mutateLayerFixture(t)
	fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n"
	writeMutateLayerConfig(t, configPath, fixture)
	concurrent := []byte("apiVersion: agent-marketplace/v4\n# external writer\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n")
	reconcile := func(ctx context.Context, _, _ string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
		if len(edits) != 1 {
			t.Fatalf("edits = %#v", edits)
		}
		if err := os.WriteFile(configPath, concurrent, 0o640); err != nil {
			t.Fatal(err)
		}
		desired := []dependencytxn.DesiredFile{{
			Path:         edits[0].Path,
			Expected:     edits[0].Expected,
			ExpectedMode: edits[0].Mode,
			Bytes:        edits[0].Bytes,
			Mode:         edits[0].Mode,
		}, repoLockDesired(t, configPath)}
		_, err := dependencytxn.Commit(ctx, stateDir, desired)
		return DependencyLockView{}, err
	}
	err := MutateLayer(context.Background(), LayerMutationTarget{
		RepoRoot:   repoRoot,
		ConfigPath: configPath,
		Source:     config.RepoCommitted,
		Reconcile:  reconcile,
	}, includeSkillAlpha)
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("error = %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != string(concurrent) {
		t.Fatalf("concurrent bytes lost: %v %s", err, data)
	}
	lockPath, err := lockfile.LockPathForConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock published on conflict: %v", err)
	}
	assertNoStagingResidue(t)
}

func TestMutateLayerRejectsInvalidInputs(t *testing.T) {
	repoRoot, configPath, _ := mutateLayerFixture(t)
	fixture := "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n"
	writeMutateLayerConfig(t, configPath, fixture)
	reconcile := commitStagedEdits(t.TempDir())

	for name, target := range map[string]LayerMutationTarget{
		"nil reconciler":      {RepoRoot: repoRoot, ConfigPath: configPath, Source: config.RepoCommitted},
		"blank repo root":     {ConfigPath: configPath, Source: config.RepoCommitted, Reconcile: reconcile},
		"blank config path":   {RepoRoot: repoRoot, Source: config.RepoCommitted, Reconcile: reconcile},
		"non-writable source": {RepoRoot: repoRoot, ConfigPath: configPath, Source: config.Organization, Reconcile: reconcile},
	} {
		t.Run(name, func(t *testing.T) {
			if err := MutateLayer(context.Background(), target, includeSkillAlpha); err == nil {
				t.Fatal("accepted invalid target")
			}
		})
	}
	t.Run("nil mutate", func(t *testing.T) {
		if err := MutateLayer(context.Background(), LayerMutationTarget{RepoRoot: repoRoot, ConfigPath: configPath, Source: config.RepoCommitted, Reconcile: reconcile}, nil); err == nil {
			t.Fatal("accepted nil mutate")
		}
	})
	t.Run("nil mutation wrapper", func(t *testing.T) {
		var m *LayerMutation
		if _, err := m.EnsureRegistry(config.RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}); err == nil {
			t.Fatal("nil receiver EnsureRegistry accepted")
		}
		if err := m.Apply(config.SourceBinding{}, config.SelectionChange{}); err == nil {
			t.Fatal("nil receiver Apply accepted")
		}
		if err := m.RemoveSource(config.SourceBinding{}); err == nil {
			t.Fatal("nil receiver RemoveSource accepted")
		}
		if err := m.ClearUnboundExclude(config.ResourceLocator{Kind: ir.KindSkill, Name: "x"}); err == nil {
			t.Fatal("nil receiver ClearUnboundExclude accepted")
		}
		if err := m.RemoveUnboundInclude(config.ResourceLocator{Kind: ir.KindSkill, Name: "x"}); err == nil {
			t.Fatal("nil receiver RemoveUnboundInclude accepted")
		}
		if err := m.SetUnboundExclude(config.ResourceLocator{Kind: ir.KindSkill, Name: "x"}, true); err == nil {
			t.Fatal("nil receiver SetUnboundExclude accepted")
		}
		if _, err := m.UnboundIncludeInherited(config.ResourceLocator{Kind: ir.KindSkill, Name: "x"}); err == nil {
			t.Fatal("nil receiver UnboundIncludeInherited accepted")
		}
		if _, _, err := m.LookupRegistry(config.RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}); err == nil {
			t.Fatal("nil receiver LookupRegistry accepted")
		}
	})

	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != fixture {
		t.Fatalf("invalid inputs changed config: %v", err)
	}
	assertNoStagingResidue(t)
}

func TestMutateLayerRemoveSource(t *testing.T) {
	repoRoot, configPath, stateDir := mutateLayerFixture(t)
	fixture := "apiVersion: agent-marketplace/v4\nsources:\n  main: {git: https://example.test/repo.git, ref: main}\n  dev: {git: https://example.test/repo.git, ref: dev}\nresources:\n  dev: [skill/alpha]\n"
	writeMutateLayerConfig(t, configPath, fixture)
	reconcile := func(ctx context.Context, root, local string, locked bool, edits []StagedConfigEdit) (DependencyLockView, error) {
		if len(edits) != 1 {
			t.Fatalf("edits = %#v", edits)
		}
		if strings.Contains(string(edits[0].Bytes), "ref: dev") {
			t.Fatalf("dev source still staged:\n%s", edits[0].Bytes)
		}
		return commitStagedEdits(stateDir, repoLockDesired(t, configPath))(ctx, root, local, locked, edits)
	}
	err := MutateLayer(context.Background(), LayerMutationTarget{
		RepoRoot:   repoRoot,
		ConfigPath: configPath,
		Source:     config.RepoCommitted,
		Reconcile:  reconcile,
	}, func(m *LayerMutation) error {
		binding, err := m.EnsureRegistry(config.RegistryEntry{URL: "https://example.test/repo.git", Ref: "dev"})
		if err != nil {
			return err
		}
		return m.RemoveSource(binding)
	})
	if err != nil {
		t.Fatalf("MutateLayer: %v", err)
	}
	cfg, err := config.LoadV4ConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("published config invalid: %v", err)
	}
	if _, ok := cfg.Sources["main"]; !ok || len(cfg.Sources) != 1 {
		t.Fatalf("sources = %#v", cfg.Sources)
	}
	lockPath, err := lockfile.LockPathForConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockfile.ReadBytes(lockPath); err != nil {
		t.Fatalf("lock: %v", err)
	}
	assertNoStagingResidue(t)
}

func TestAdHocEntryMatchesBinding(t *testing.T) {
	bound := config.RegistryEntry{URL: "https://example.test/repo", Ref: "main", Path: "skills"}

	t.Run("normalized git url match", func(t *testing.T) {
		candidate := config.RegistryEntry{URL: "https://example.test/repo.git", Ref: "main", Path: "skills/"}
		if !adHocEntryMatchesBinding(candidate, bound) {
			t.Fatal("equivalent git identity did not match")
		}
	})
	t.Run("refusal on wrong ref", func(t *testing.T) {
		if adHocEntryMatchesBinding(config.RegistryEntry{URL: bound.URL, Ref: "dev", Path: bound.Path}, bound) {
			t.Fatal("wrong ref matched")
		}
	})
	t.Run("refusal on wrong path", func(t *testing.T) {
		if adHocEntryMatchesBinding(config.RegistryEntry{URL: bound.URL, Ref: bound.Ref, Path: "other"}, bound) {
			t.Fatal("wrong path matched")
		}
	})
	t.Run("directory catalog key match", func(t *testing.T) {
		dirBound := config.RegistryEntry{URL: filepath.Join(t.TempDir(), "skills"), V4Kind: config.V4SourceDirectory}
		candidate := config.RegistryEntry{URL: "./skills", CatalogKey: dirBound.URL}
		if !adHocEntryMatchesBinding(candidate, dirBound) {
			t.Fatal("relative local source did not match resolved catalog root")
		}
	})
	t.Run("directory refusal on different root", func(t *testing.T) {
		dirBound := config.RegistryEntry{URL: filepath.Join(t.TempDir(), "skills"), V4Kind: config.V4SourceDirectory}
		candidate := config.RegistryEntry{URL: "./other", CatalogKey: filepath.Join(t.TempDir(), "other")}
		if adHocEntryMatchesBinding(candidate, dirBound) {
			t.Fatal("different directory root matched")
		}
	})
	t.Run("mixed local and git never match", func(t *testing.T) {
		dir := t.TempDir()
		for name, tc := range map[string]struct {
			candidate, bound config.RegistryEntry
		}{
			"local binding vs git candidate": {
				candidate: config.RegistryEntry{URL: filepath.Join(dir, "repo.git"), Ref: "main", CatalogKey: filepath.Join(dir, "skills")},
				bound:     config.RegistryEntry{URL: filepath.Join(dir, "skills"), V4Kind: config.V4SourceDirectory},
			},
			"git binding vs local candidate": {
				candidate: config.RegistryEntry{URL: filepath.Join(dir, "skills"), CatalogKey: filepath.Join(dir, "repo")},
				bound:     config.RegistryEntry{URL: filepath.Join(dir, "repo.git"), Ref: "main"},
			},
		} {
			t.Run(name, func(t *testing.T) {
				if adHocEntryMatchesBinding(tc.candidate, tc.bound) {
					t.Fatalf("mixed local/git identity matched: %#v vs %#v", tc.candidate, tc.bound)
				}
			})
		}
	})
	t.Run("multiple physical matches each return true", func(t *testing.T) {
		first := config.RegistryEntry{URL: bound.URL, Ref: bound.Ref, Path: bound.Path, SourceHarness: "claude"}
		second := config.RegistryEntry{URL: bound.URL, Ref: bound.Ref, Path: bound.Path, SourceHarness: "cursor"}
		if !adHocEntryMatchesBinding(first, bound) || !adHocEntryMatchesBinding(second, bound) {
			t.Fatal("duplicate physical occurrences must each match so the caller can count them")
		}
	})
}
