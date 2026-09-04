package config

// Tests in this file verify two YAML-editor invariants:
//
//  1. URL normalization: editor deduplication and matching must use
//     NormalizeGitURL so that equivalent URLs (e.g. "repo" vs "repo.git",
//     or "repo/" vs "repo") are treated as the same registry.
//  2. No inheritance materialization: when writing per-kind selectors
//     to a registry entry, the editor must NOT copy top-level selector
//     values into the entry. Attribute inheritance is a resolver-time
//     concern; persisting it into static YAML creates stale config.

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// EnsureRegistryWithPathsInConfig must treat `repo` and `repo.git` as
// the same registry. Without normalized comparison, the second call
// creates a duplicate entry.
func TestEnsureRegistryWithPathsInConfigDedupsByNormalizedURL(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`))

	if err := EnsureRegistryWithPathsInConfig(
		configPath,
		"git@gitlab.example.com:example-org/example-resources.git",
		"main",
		"teams/example-team/plugins/dev-engineering",
	); err != nil {
		t.Fatalf("first EnsureRegistryWithPathsInConfig: %v", err)
	}

	// Same repo, no .git suffix, same path. Must be deduplicated.
	if err := EnsureRegistryWithPathsInConfig(
		configPath,
		"git@gitlab.example.com:example-org/example-resources",
		"main",
		"teams/example-team/plugins/dev-engineering",
	); err != nil {
		t.Fatalf("second EnsureRegistryWithPathsInConfig: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry (URL normalized dedup), got %d entries: %#v", len(cfg.Registries), cfg.Registries)
	}
}

// Trailing-slash variant: EnsureRegistryWithPathsInConfig must also
// treat "repo" and "repo/" as the same registry.
func TestEnsureRegistryWithPathsInConfigDedupsTrailingSlash(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: https://example.com/owner/repo
    ref: main
`))

	if err := EnsureRegistryWithPathsInConfig(
		configPath,
		"https://example.com/owner/repo/",
		"main",
		"",
	); err != nil {
		t.Fatalf("EnsureRegistryWithPathsInConfig: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry (trailing slash dedup), got %d", len(cfg.Registries))
	}
}

// addValuesToRegistrySubSequence must locate an entry whose URL matches
// after normalization, not exact-string match, so the include-add lands
// on the existing entry instead of failing.
func TestAddSkillsToRegistryInConfigMatchesNormalizedURL(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra
    ref: main
    path: skills
`))

	// Caller passes the .git-suffixed form; entry on disk has bare form.
	if err := AddSkillsToRegistryInConfig(
		configPath,
		"git@example.com:infra.git",
		"skills",
		[]string{"my-skill"},
	); err != nil {
		t.Fatalf("AddSkillsToRegistryInConfig: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	if got := cfg.Registries[0].Skills.Include; !reflect.DeepEqual(got, []string{"my-skill"}) {
		t.Fatalf("expected skill written to existing entry, got %#v", got)
	}
}

// AddResourceToExactRegistryEntry must also normalize URL before
// comparison so that cross-suffix matching works.
func TestAddResourceToExactRegistryEntryMatchesNormalizedURL(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra
    ref: main
`))

	if err := AddResourceToExactRegistryEntry(
		configPath,
		"git@example.com:infra.git",
		"",
		ir.KindSkill,
		"my-skill",
	); err != nil {
		t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Registries[0].Skills.Include; !reflect.DeepEqual(got, []string{"my-skill"}) {
		t.Fatalf("expected skill on bare-URL entry, got %#v", got)
	}
}

// Top-level skills (mapping form) must NOT leak into a registry entry's
// skills.include on first write. The resolver handles attribute
// inheritance at read time; persisting it creates stale config.
func TestAddSkillsToRegistryInConfigDoesNotMaterializeTopLevelInheritance(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: main

skills:
  include:
    - crr-kafka-ingestion
    - code-review-coderabbit
`))

	if err := AddSkillsToRegistryInConfig(
		configPath,
		"git@example.com:infra.git",
		"",
		[]string{"helm-chart"},
	); err != nil {
		t.Fatalf("AddSkillsToRegistryInConfig: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.Registries[0].Skills.Include
	want := []string{"helm-chart"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry entry must NOT inherit top-level skills into YAML; got %#v want %#v", got, want)
	}
}

// Same no-inheritance constraint when top-level is the legacy
// `skills: [a, b]` flat list form.
func TestAddSkillsToRegistryInConfigDoesNotInheritLegacyFlatTopLevel(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: main

skills:
  - crr-kafka-ingestion
  - code-review-coderabbit
`))

	if err := AddSkillsToRegistryInConfig(
		configPath,
		"git@example.com:infra.git",
		"",
		[]string{"helm-chart"},
	); err != nil {
		t.Fatalf("AddSkillsToRegistryInConfig: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.Registries[0].Skills.Include
	want := []string{"helm-chart"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry entry must NOT inherit legacy flat skills into YAML; got %#v want %#v", got, want)
	}
}

// Same no-inheritance guarantee for AddResourceToExactRegistryEntry,
// the editor used by `install --from`.
func TestAddResourceToExactRegistryEntryDoesNotInherit(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: main

skills:
  include:
    - crr-kafka-ingestion
    - code-review-coderabbit
`))

	if err := AddResourceToExactRegistryEntry(
		configPath,
		"git@example.com:infra.git",
		"",
		ir.KindSkill,
		"gitlab-info",
	); err != nil {
		t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.Registries[0].Skills.Include
	want := []string{"gitlab-info"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install --from must not materialize top-level inheritance; got %#v want %#v", got, want)
	}
}

// removeValuesFromExactRegistryEntry must also normalize URLs so that
// removing an exclude by a .git-suffixed URL works against a bare-URL entry.
func TestRemoveValuesFromExactRegistryEntryMatchesNormalizedURL(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra
    ref: main
    skills:
      exclude:
        - stale-skill
`))

	// Caller uses .git-suffixed URL; entry on disk uses bare form.
	if err := removeValuesFromExactRegistryEntry(
		configPath,
		"git@example.com:infra.git",
		"",
		ir.KindSkill,
		"exclude",
		[]string{"stale-skill"},
	); err != nil {
		t.Fatalf("removeValuesFromExactRegistryEntry: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Registries[0].Skills.Exclude; len(got) != 0 {
		t.Fatalf("expected exclude list to be empty after remove, got %#v", got)
	}
}

func TestRegistryPathIdentityNormalizesSeparatorsAndDotSegments(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "dot and repeated separators", path: "./foo//bar/", want: "foo/bar"},
		{name: "mixed separators", path: `foo\bar`, want: "foo/bar"},
		{name: "root", path: ".", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeRegistryPath(tc.path); got != tc.want {
				t.Fatalf("NormalizeRegistryPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestAddSkillsToRegistryRejectsRequestedDescendantOfConfiguredParent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra
    ref: main
    path: foo
    skills:
      include:
        - parent-skill
`))

	if err := AddSkillsToRegistryInConfig(configPath, "git@example.com:infra", "foo/bar", []string{"child-skill"}); err == nil {
		t.Fatal("requested descendant path unexpectedly matched configured parent")
	}
	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 || cfg.Registries[0].Path != "foo" || !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"parent-skill"}) {
		t.Fatalf("configured parent changed after descendant request: %#v", cfg.Registries)
	}
}

func TestRemoveRegistryEntryRejectsDescendantOfConfiguredParent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra
    ref: main
    path: foo
`))
	if err := RemoveRegistryEntryFromConfig(configPath, "git@example.com:infra.git", "foo/bar"); err != nil {
		t.Fatalf("RemoveRegistryEntryFromConfig: %v", err)
	}
	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 || cfg.Registries[0].Path != "foo" {
		t.Fatalf("parent entry was removed by descendant request: %#v", cfg.Registries)
	}
}

// Combined E2E: EnsureRegistry + AddResourceToExactRegistryEntry against
// a repo URL whose normalized form already exists in config, with an
// existing top-level skills selector. Must not produce a spurious
// duplicate entry or bake top-level skills into the entry.
func TestInstallFromDoesNotDuplicateOrInherit(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@gitlab.example.com:example-org/example-resources.git
    ref: main
    path: teams/example-team/plugins/dev-engineering
    skills:
      include:
        - vm-lifecycle

skills:
  include:
    - crr-kafka-ingestion
    - code-review-coderabbit
`))

	// install --from <bare URL> --path teams/example-team/plugins/dev-engineering gitlab-info
	if err := EnsureRegistryWithPathsInConfig(
		configPath,
		"git@gitlab.example.com:example-org/example-resources", // bare form
		"main",
		"teams/example-team/plugins/dev-engineering",
	); err != nil {
		t.Fatalf("EnsureRegistryWithPathsInConfig: %v", err)
	}
	if err := AddResourceToExactRegistryEntry(
		configPath,
		"git@gitlab.example.com:example-org/example-resources", // bare form
		"teams/example-team/plugins/dev-engineering",
		ir.KindSkill,
		"gitlab-info",
	); err != nil {
		t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
	}

	cfg, err := loadConfig(configPath, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry entry (no spurious duplicate), got %d: %#v", len(cfg.Registries), cfg.Registries)
	}
	got := cfg.Registries[0].Skills.Include
	want := []string{"vm-lifecycle", "gitlab-info"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected only the existing + newly requested skill, got %#v", got)
	}
}
