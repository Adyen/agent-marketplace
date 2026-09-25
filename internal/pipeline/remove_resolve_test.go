package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func artifact(kind ir.ResourceKind, name, url, path string, source config.Source) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry:        catalog.Entry{Kind: kind, Name: name},
		RegistryURL:  url,
		RegistryPath: path,
		Source:       source,
	}
}

// A single match resolves cleanly regardless of kind filtering.
func TestResolvedResourceByName_SingleMatch(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "reg", "plugins", config.UserPersonal),
	}
	got, err := resolvedResourceByName(resolved, "sql-queries", ir.KindSkill, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name != "sql-queries" {
		t.Fatalf("expected sql-queries match, got %#v", got)
	}
}

// Same-kind matches from multiple config layers must NOT report a cross-kind
// ambiguity — that message is nonsensical (it repeats the same kind and tells
// the user to pass --kind, which they already did). It should instead give a
// ready-to-run invocation per layer, keyed by --scope.
func TestResolvedResourceByName_SameKindMultipleLayers(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.UserPersonal),
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.RepoPersonal),
	}
	_, err := resolvedResourceByName(resolved, "sql-queries", ir.KindSkill, true, nil)
	if err == nil {
		t.Fatal("expected an ambiguity error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "ambiguous across kinds") {
		t.Fatalf("must not report cross-kind ambiguity for same-kind matches: %q", msg)
	}
	// One line per layer, each keyed by the --scope that makes it effective.
	for _, want := range []string{"--scope user", "--scope local", "--from", "--path", "teams/pm/plugins", "USER", "REPO*"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected message to contain %q, got: %q", want, msg)
		}
	}
}

func TestResolvedResourceByNameInScopeMissingScopeDoesNotFallThrough(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "only-local", "registry", "", config.RepoPersonal),
	}
	got, err := resolvedResourceByNameInScope(resolved, "only-local", ir.KindSkill, true, nil, ScopeRepo, true)
	if err != nil || got != nil {
		t.Fatalf("missing repo occurrence = (%#v, %v), want not-enabled nil", got, err)
	}

	resolved = []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "only-repo", "registry", "", config.RepoCommitted),
	}
	got, err = resolvedResourceByNameInScope(resolved, "only-repo", ir.KindSkill, true, nil, ScopeUser, true)
	if err != nil || got != nil {
		t.Fatalf("missing user occurrence = (%#v, %v), want not-enabled nil", got, err)
	}
}

func TestResolvedResourceByNameInScopeRetainsMachineBoundaryRefusal(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "machine-only", "registry", "", config.UserPersonal),
	}
	got, err := resolvedResourceByNameInScope(resolved, "machine-only", ir.KindSkill, true, nil, ScopeRepo, true)
	if err != nil || got == nil || !got.Source.IsMachineScope() {
		t.Fatalf("machine boundary candidate = (%#v, %v), want machine occurrence", got, err)
	}
}

func TestResolvedResourceByNameInScopeSelectsMatchingLayerBeforeAmbiguity(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.UserPersonal),
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.RepoCommitted),
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.RepoPersonal),
	}

	tests := []struct {
		name  string
		scope Scope
		want  config.Source
	}{
		{name: "user", scope: ScopeUser, want: config.UserPersonal},
		{name: "repo", scope: ScopeRepo, want: config.RepoCommitted},
		{name: "local", scope: ScopeLocal, want: config.RepoPersonal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvedResourceByNameInScope(resolved, "sql-queries", ir.KindSkill, true, nil, tt.scope, true)
			if err != nil {
				t.Fatalf("unexpected ambiguity for --scope %s: %v", tt.scope, err)
			}
			if got == nil || got.Source != tt.want {
				t.Fatalf("scope %s selected %#v, want source %v", tt.scope, got, tt.want)
			}
		})
	}
}

// The suggested --path must be the configured (raw) entry path, not the deeper
// discovered sub-bundle path recorded on the artifact — otherwise the user is
// sent to a --path that matches no config entry ("not found in any config").
func TestResolvedResourceByName_SuggestsRawEntryPath(t *testing.T) {
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "awesome-ai", Ref: "main", Path: "teams/pm/plugins", Source: config.UserPersonal},
		},
	}
	resolved := []resolver.ResolvedArtifact{
		// Artifact resolved from a discovered sub-bundle beneath the entry.
		{
			Entry:        catalog.Entry{Kind: ir.KindSkill, Name: "sql-queries"},
			RegistryURL:  "awesome-ai",
			RegistryPath: "teams/pm/plugins/pm-data-analytics",
			Ref:          "main",
			Source:       config.UserPersonal,
		},
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.RepoPersonal),
	}
	_, err := resolvedResourceByName(resolved, "sql-queries", ir.KindSkill, true, cfg)
	if err == nil {
		t.Fatal("expected an ambiguity error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "pm-data-analytics") {
		t.Fatalf("must suggest the configured parent path, not the discovered sub-bundle path: %q", msg)
	}
	if !strings.Contains(msg, `--path "teams/pm/plugins"`) {
		t.Fatalf("expected the raw entry path in the suggestion, got: %q", msg)
	}
}

// Genuinely different kinds sharing a name is the one case where --kind helps.
func TestResolvedResourceByName_DifferentKinds(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "deploy", "reg", "", config.UserPersonal),
		artifact(ir.KindCommand, "deploy", "reg", "", config.UserPersonal),
	}
	_, err := resolvedResourceByName(resolved, "deploy", 0, false, nil)
	if err == nil {
		t.Fatal("expected an ambiguity error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ambiguous across kinds") || !strings.Contains(msg, "rerun with --kind") {
		t.Fatalf("expected cross-kind ambiguity message, got: %q", msg)
	}
	if !strings.Contains(msg, "command") || !strings.Contains(msg, "skill") {
		t.Fatalf("expected both kind labels in message, got: %q", msg)
	}
}

// The --from path narrows the lookup to a single registry entry so the same
// resource resolving at multiple scopes no longer trips the ambiguity guard.
func TestResolvedResourceInRegistry_FiltersByURLAndPath(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.UserPersonal),
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.RepoCommitted),
		artifact(ir.KindSkill, "sql-queries", "other-reg", "elsewhere", config.RepoCommitted),
	}
	got, err := resolvedResourceInRegistry(resolved, "sql-queries", ir.KindSkill, true, "awesome-ai", "teams/pm/plugins")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a match for the named registry entry")
	}
	if got.RegistryURL != "awesome-ai" || got.RegistryPath != "teams/pm/plugins" {
		t.Fatalf("matched the wrong entry: %#v", got)
	}
}

// With --kind omitted, a single registry entry that contains a skill and a
// command sharing the same name is ambiguous: removeFromAdHoc must be told
// which kind to disable rather than silently picking the first match.
func TestResolvedResourceInRegistry_SameRegistryCrossKindRequiresKind(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "deploy", "awesome-ai", "teams/pm/plugins", config.RepoCommitted),
		artifact(ir.KindCommand, "deploy", "awesome-ai", "teams/pm/plugins", config.RepoCommitted),
	}
	// hasKind == false → the two kinds cannot be disambiguated; expect an error.
	got, err := resolvedResourceInRegistry(resolved, "deploy", 0, false, "awesome-ai", "teams/pm/plugins")
	if err == nil {
		t.Fatalf("expected a cross-kind ambiguity error, got match %#v", got)
	}
	if got != nil {
		t.Fatalf("expected no match on ambiguity, got %#v", got)
	}
	if msg := err.Error(); !strings.Contains(msg, "ambiguous across kinds") || !strings.Contains(msg, "--kind") {
		t.Fatalf("expected cross-kind ambiguity message mentioning --kind, got: %q", msg)
	}
	// hasKind == true → the caller named the kind, so the matching entry from
	// the requested registry is returned instead of erroring.
	got, err = resolvedResourceInRegistry(resolved, "deploy", ir.KindCommand, true, "awesome-ai", "teams/pm/plugins")
	if err != nil {
		t.Fatalf("unexpected error once --kind narrows the lookup: %v", err)
	}
	if got == nil || got.Kind != ir.KindCommand {
		t.Fatalf("expected the command from the requested registry, got %#v", got)
	}
}

func TestResolvedResourceInRegistry_NoMatch(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins", config.UserPersonal),
	}
	got, err := resolvedResourceInRegistry(resolved, "sql-queries", ir.KindSkill, true, "awesome-ai", "different/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for a non-matching path, got %#v", got)
	}
}

func TestAdHocPathMatchingNormalizesIdentityButRejectsDescendantEntry(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:  "git@example.com:infra.git",
		Ref:  "main",
		Path: "./foo//bar/",
	}}}
	entry, found, err := selectAdHocRegistryEntry(cfg, "git@example.com:infra", "foo\\bar", "main", "")
	if err != nil || !found || entry.Path != "./foo//bar/" {
		t.Fatalf("normalized exact path lookup = (%#v, %v, %v), want matching entry", entry, found, err)
	}
	if _, found, err := selectAdHocRegistryEntry(cfg, "git@example.com:infra", "foo/bar/child", "main", ""); err != nil || found {
		t.Fatalf("descendant path matched configured parent: found=%v err=%v", found, err)
	}

	resolved := []resolver.ResolvedArtifact{artifact(ir.KindSkill, "nested", "git@example.com:infra", "foo\\bar\\child", config.RepoCommitted)}
	got, err := resolvedResourceInRegistry(resolved, "nested", ir.KindSkill, true, "git@example.com:infra.git", "./foo//bar/")
	if err != nil || got == nil {
		t.Fatalf("named resource subtree lookup = (%#v, %v), want descendant resource", got, err)
	}
}

// The resolver records the discovered sub-bundle path on an artifact, which can
// be deeper than the configured entry path. A --path naming the entry must
// still own artifacts materialized from beneath it.
func TestResolvedResourceInRegistry_EntryPathOwnsDiscoveredSubPath(t *testing.T) {
	resolved := []resolver.ResolvedArtifact{
		artifact(ir.KindSkill, "sql-queries", "awesome-ai", "teams/pm/plugins/pm-data-analytics", config.UserPersonal),
	}
	got, err := resolvedResourceInRegistry(resolved, "sql-queries", ir.KindSkill, true, "awesome-ai", "teams/pm/plugins")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("entry path should own the deeper discovered sub-bundle path")
	}
}

func TestRemoveExistingRelativeUserRegistry(t *testing.T) {
	repoRoot, home := t.TempDir(), t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", home)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := filepath.Join(repoRoot, "project-registry")
	writeSkill(t, registryRoot, "review", "review skill", nil)
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	initial := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: ./project-registry\n    skills: {include: [review]}\n"
	writeFile(t, userPath, initial)
	result, err := Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, Scope: ScopeUser, ScopeSpecified: true, FromURL: "./project-registry",
	})
	if err != nil {
		t.Fatalf("remove existing relative user registry: %v", err)
	}
	if result == nil || result.ExcludedRegistry {
		t.Fatalf("remove result = %#v", result)
	}
	data, err := os.ReadFile(userPath)
	if err != nil || strings.Contains(string(data), "project-registry") || !strings.Contains(string(data), "harnesses: [factory]") {
		t.Fatalf("existing registry not removed: %q err=%v", data, err)
	}
}

func TestRemoveNamedExistingRelativeUserRegistry(t *testing.T) {
	for _, tc := range []struct {
		name, selection, resource string
		from                      bool
		collection                bool
	}{
		{name: "named from", selection: "skills: {include: [review]}", resource: "review", from: true},
		{name: "named unscoped", selection: "skills: {include: [review]}", resource: "review"},
		{name: "collection from", selection: "collections: [bundle]", resource: "bundle", from: true, collection: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, home := t.TempDir(), t.TempDir()
			t.Chdir(repoRoot)
			t.Setenv("HOME", home)
			config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
			registryRoot := filepath.Join(repoRoot, "project-registry")
			writeSkill(t, registryRoot, "review", "review skill", nil)
			if tc.collection {
				writeFile(t, filepath.Join(registryRoot, "collections", "bundle", "COLLECTION.yaml"), "name: bundle\nskills:\n  - name: review\n")
			}
			userPath, err := config.GlobalConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			initial := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: ./project-registry\n    " + tc.selection + "\n"
			writeFile(t, userPath, initial)
			opts := RemoveOptions{RepoRoot: repoRoot, ResourceName: tc.resource, Scope: ScopeUser, ScopeSpecified: true}
			if tc.from {
				opts.FromURL = "./project-registry"
			}
			result, err := Remove(context.Background(), opts)
			if err != nil {
				t.Fatalf("remove existing relative user selection: %v", err)
			}
			if result == nil || result.RemovedName != tc.resource || result.IsCollection != tc.collection {
				t.Fatalf("remove result = %#v", result)
			}
			cfg, err := config.LoadRootConfigFromPath(userPath)
			if err != nil || len(cfg.Registries) != 1 || cfg.Registries[0].URL != "./project-registry" {
				t.Fatalf("existing entry was lost: %#v err=%v", cfg, err)
			}
			entry := cfg.Registries[0]
			if tc.collection {
				if !slices.Contains(entry.Collections.Exclude, tc.resource) || slices.Contains(entry.Collections.Include, tc.resource) {
					t.Fatalf("collection cleanup missing: %#v", entry.Collections)
				}
			} else if slices.Contains(entry.Skills.Include, tc.resource) && !slices.Contains(entry.Skills.Exclude, tc.resource) {
				t.Fatalf("skill cleanup missing: %#v", entry.Skills)
			}
		})
	}
}

func TestRemoveFromRelativeRepoRegistryRejectsNewUserShadow(t *testing.T) {
	repoRoot, home := t.TempDir(), t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", home)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := filepath.Join(repoRoot, "project-registry")
	writeSkill(t, registryRoot, "review", "review skill", nil)
	repoPath := config.ProjectConfigPath(repoRoot)
	repoInitial := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: ./project-registry\n    skills: {include: [review]}\n"
	writeFile(t, repoPath, repoInitial)
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	userInitial := "apiVersion: agent-marketplace/v2\nregistries: []\n"
	writeFile(t, userPath, userInitial)
	_, err = Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "review", FromURL: "./project-registry", Scope: ScopeUser, ScopeSpecified: true,
	})
	var boundary *config.GlobalRelativeRegistryError
	if !errors.As(err, &boundary) {
		t.Fatalf("new relative user shadow error = %v, want GlobalRelativeRegistryError", err)
	}
	data, err := os.ReadFile(userPath)
	if err != nil || string(data) != userInitial {
		t.Fatalf("rejected shadow changed user config: %q err=%v", data, err)
	}
	data, err = os.ReadFile(repoPath)
	if err != nil || string(data) != repoInitial {
		t.Fatalf("rejected shadow changed repo config: %q err=%v", data, err)
	}
}

func TestRemoveFromAdHocShadowsIncludedRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "included-skill", "included skill", nil)

	includedPath := filepath.Join(repoRoot, ".agents", "team.yaml")
	includedData := fmt.Sprintf("registries:\n  - path: %q\n    source_harness: factory\n    skills: [included-skill]\n", registryRoot)
	writeFile(t, includedPath, includedData)
	rootPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeFile(t, rootPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: team.yaml\n")

	result, err := Remove(context.Background(), RemoveOptions{
		RepoRoot:       repoRoot,
		Scope:          ScopeRepo,
		ScopeSpecified: true,
		FromURL:        registryRoot,
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || !result.ExcludedRegistry || result.ExcludedURL != registryRoot || result.ExcludedScope != ScopeRepo {
		t.Fatalf("result = %#v, want repo exclude_all shadow", result)
	}

	gotIncluded, err := os.ReadFile(includedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotIncluded) != includedData {
		t.Fatalf("included config changed:\n%s", gotIncluded)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCfg.Registries) != 1 || !rootCfg.Registries[0].ExcludeAll || rootCfg.Registries[0].URL != registryRoot {
		t.Fatalf("root registries = %#v, want registry shadow", rootCfg.Registries)
	}
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootData), "include: team.yaml") {
		t.Fatalf("root include directive lost:\n%s", rootData)
	}
	assertResourceNotResolved(t, repoRoot, "included-skill")
}

func TestRemoveNamedResourceFromIncludedAdHocRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "included-named-skill", "included named skill", nil)
	includedPath := filepath.Join(repoRoot, ".agents", "team.yaml")
	includedData := fmt.Sprintf("registries:\n  - path: %q\n    source_harness: factory\n    skills: [included-named-skill]\n", registryRoot)
	writeFile(t, includedPath, includedData)
	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: team.yaml\n")

	result, err := Remove(context.Background(), RemoveOptions{
		RepoRoot:       repoRoot,
		ResourceName:   "included-named-skill",
		Scope:          ScopeRepo,
		ScopeSpecified: true,
		FromURL:        registryRoot,
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || result.RemovedName != "included-named-skill" {
		t.Fatalf("result = %#v", result)
	}
	gotIncluded, err := os.ReadFile(includedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotIncluded) != includedData {
		t.Fatalf("included config changed:\n%s", gotIncluded)
	}
	assertResourceNotResolved(t, repoRoot, "included-named-skill")
}

func TestRemoveDuplicateRootAndIncludedSkillRetainsExclusion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		adHoc bool
	}{
		{name: "configured"},
		{name: "ad-hoc", adHoc: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			t.Chdir(repoRoot)
			config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
			registryRoot := t.TempDir()
			writeSkill(t, registryRoot, "duplicate-explicit-skill", "duplicate explicit skill", nil)
			includedPath := filepath.Join(repoRoot, ".agents", "team.yaml")
			includedData := fmt.Sprintf("registries:\n  - path: %q\n    source_harness: factory\n    skills: [duplicate-explicit-skill]\n", registryRoot)
			writeFile(t, includedPath, includedData)
			rootPath := config.ProjectConfigPath(repoRoot)
			writeFile(t, rootPath, fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: team.yaml\nregistries:\n  - path: %q\n    source_harness: factory\n    skills: [duplicate-explicit-skill]\n", registryRoot))

			opts := RemoveOptions{RepoRoot: repoRoot, ResourceName: "duplicate-explicit-skill", Scope: ScopeRepo, ScopeSpecified: true}
			if tc.adHoc {
				opts.FromURL = registryRoot
			}
			result, err := Remove(context.Background(), opts)
			if err != nil {
				t.Fatalf("Remove: %v", err)
			}
			if result == nil || result.RemovedName != "duplicate-explicit-skill" {
				t.Fatalf("result = %#v", result)
			}
			gotIncluded, err := os.ReadFile(includedPath)
			if err != nil || string(gotIncluded) != includedData {
				t.Fatalf("included config changed: %v\n%s", err, gotIncluded)
			}
			rootCfg, err := config.LoadRootConfigFromPath(rootPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(rootCfg.Registries) != 1 || !slices.Contains(rootCfg.Registries[0].Skills.Exclude, "duplicate-explicit-skill") || slices.Contains(rootCfg.Registries[0].Skills.Include, "duplicate-explicit-skill") {
				t.Fatalf("root registries = %#v", rootCfg.Registries)
			}
			rootData, err := os.ReadFile(rootPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(rootData), "include: team.yaml") {
				t.Fatalf("root include directive lost:\n%s", rootData)
			}
			assertResourceNotResolved(t, repoRoot, "duplicate-explicit-skill")
		})
	}
}

func TestRemoveFromAdHocReplacesRootDuplicateWithShadow(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "duplicate-skill", "duplicate skill", nil)
	includedPath := filepath.Join(repoRoot, ".agents", "team.yaml")
	includedData := fmt.Sprintf("registries:\n  - path: %q\n    source_harness: factory\n    skills: [duplicate-skill]\n", registryRoot)
	writeFile(t, includedPath, includedData)
	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: team.yaml\nregistries:\n  - path: %q\n    source_harness: factory\n    skills: [duplicate-skill]\n", registryRoot))

	result, err := Remove(context.Background(), RemoveOptions{RepoRoot: repoRoot, Scope: ScopeRepo, ScopeSpecified: true, FromURL: registryRoot})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || !result.ExcludedRegistry {
		t.Fatalf("result = %#v, want exclusion shadow", result)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCfg.Registries) != 1 || !rootCfg.Registries[0].ExcludeAll {
		t.Fatalf("root registries = %#v, want retained exclude_all shadow", rootCfg.Registries)
	}
	gotIncluded, err := os.ReadFile(includedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotIncluded) != includedData {
		t.Fatalf("included config changed:\n%s", gotIncluded)
	}
	assertResourceNotResolved(t, repoRoot, "duplicate-skill")
}

func TestRemoveLocalResourceShadowsIncludedRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "included-local-skill", "included local skill", nil)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")

	includedPath := filepath.Join(repoRoot, ".agents", "local-team.yaml")
	includedData := fmt.Sprintf("registries:\n  - path: %q\n    source_harness: factory\n    skills: [included-local-skill]\n", registryRoot)
	writeFile(t, includedPath, includedData)
	rootPath := config.LocalConfigPath(repoRoot)
	writeFile(t, rootPath, "include: local-team.yaml\n")

	result, err := Remove(context.Background(), RemoveOptions{
		RepoRoot:       repoRoot,
		ResourceName:   "included-local-skill",
		Scope:          ScopeLocal,
		ScopeSpecified: true,
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || result.RemovedName != "included-local-skill" {
		t.Fatalf("result = %#v", result)
	}
	gotIncluded, err := os.ReadFile(includedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotIncluded) != includedData {
		t.Fatalf("included config changed:\n%s", gotIncluded)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCfg.Registries) != 1 || !slices.Contains(rootCfg.Registries[0].Skills.Exclude, "included-local-skill") {
		t.Fatalf("local root registries = %#v, want resource exclusion", rootCfg.Registries)
	}
	resolved := Resolve(context.Background(), ResolveOptions{RepoRoot: repoRoot, SkipInventory: true})
	if len(resolved.Errors) != 0 {
		t.Fatalf("resolve errors = %v", resolved.Errors)
	}
	for _, item := range resolved.Resolved {
		if item.Name == "included-local-skill" {
			t.Fatalf("included local skill remained enabled: %#v", item)
		}
	}
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootData), "include: local-team.yaml") {
		t.Fatalf("local root include directive lost:\n%s", rootData)
	}
}

func TestRemoveCollectionMemberShadowsIncludedRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "included-member", "included member", nil)
	writeFile(t, filepath.Join(registryRoot, "collections", "included-bundle", "COLLECTION.yaml"), "name: included-bundle\nskills:\n  - name: included-member\n")

	includedPath := filepath.Join(repoRoot, ".agents", "registries.yaml")
	includedData := fmt.Sprintf("registries:\n  - url: %q\n    source_harness: factory\n    collections: [included-bundle]\n", registryRoot)
	writeFile(t, includedPath, includedData)
	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: registries.yaml\n")

	result, err := Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "included-member", Scope: ScopeRepo, ScopeSpecified: true,
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || result.RemovedName != "included-member" || result.CollectionVia == "" {
		t.Fatalf("result = %#v, want collection member removal", result)
	}
	gotIncluded, err := os.ReadFile(includedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotIncluded) != includedData {
		t.Fatalf("included config changed:\n%s", gotIncluded)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCfg.Registries) != 1 || !slices.Contains(rootCfg.Registries[0].Skills.Exclude, "included-member") {
		t.Fatalf("root registries = %#v, want member exclusion shadow", rootCfg.Registries)
	}
	assertResourceNotResolved(t, repoRoot, "included-member")
}

func TestRemoveCollectionExcludesIncludedSelection(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	includedPath := filepath.Join(repoRoot, ".agents", "collections.yaml")
	includedData := "collections:\n  include: [included-bundle]\n"
	writeFile(t, includedPath, includedData)
	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: collections.yaml\n")

	result, err := Remove(context.Background(), RemoveOptions{
		RepoRoot:       repoRoot,
		ResourceName:   "included-bundle",
		Scope:          ScopeRepo,
		ScopeSpecified: true,
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || !result.IsCollection || result.RemovedName != "included-bundle" {
		t.Fatalf("result = %#v", result)
	}
	gotIncluded, err := os.ReadFile(includedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotIncluded) != includedData {
		t.Fatalf("included config changed:\n%s", gotIncluded)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rootCfg.Collections.Exclude, "included-bundle") {
		t.Fatalf("root collections = %#v, want collection exclusion", rootCfg.Collections)
	}
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootData), "include: collections.yaml") {
		t.Fatalf("root include directive lost:\n%s", rootData)
	}
}

func TestRemoveCollectionDuplicateInRootAndIncludeRetainsExclusion(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	includedPath := filepath.Join(repoRoot, ".agents", "collections.yaml")
	includedData := "collections:\n  include: [duplicate-bundle]\n"
	writeFile(t, includedPath, includedData)
	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: collections.yaml\ncollections:\n  include: [duplicate-bundle]\n")
	result, err := Remove(context.Background(), RemoveOptions{RepoRoot: repoRoot, ResourceName: "duplicate-bundle", Scope: ScopeRepo, ScopeSpecified: true})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsCollection || result.RemovedName != "duplicate-bundle" {
		t.Fatalf("result = %#v, want removed collection", result)
	}
	gotIncluded, err := os.ReadFile(includedPath)
	if err != nil || string(gotIncluded) != includedData {
		t.Fatalf("included config changed: %v\n%s", err, gotIncluded)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(rootCfg.Collections.Include, "duplicate-bundle") || !slices.Contains(rootCfg.Collections.Exclude, "duplicate-bundle") {
		t.Fatalf("root collections = %#v, want exclusion without selection", rootCfg.Collections)
	}
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootData), "include: collections.yaml") {
		t.Fatalf("root include directive lost:\n%s", rootData)
	}
	ec := config.LoadEffective(context.Background(), config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: repoRoot})
	if len(ec.Errors) != 0 {
		t.Fatalf("effective config errors = %v", ec.Errors)
	}
	if hasTopLevelCollectionSelection(ec.Merged, "duplicate-bundle") {
		t.Fatalf("collection remained installed: %#v", ec.Merged.Collections)
	}
}

func TestRemoveCollectionUsesRequestedScopeAndPreservesOtherRoot(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", t.TempDir())
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	writeFile(t, config.ProjectConfigPath(repoRoot), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ncollections:\n  include: [repo-bundle]\n")
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, userPath, "apiVersion: agent-marketplace/v2\ncollections:\n  include: [user-bundle]\n")

	if _, err := Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "user-bundle", Scope: ScopeUser, ScopeSpecified: true,
	}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	userCfg, err := config.LoadRootConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(userCfg.Collections.Include, "user-bundle") {
		t.Fatalf("user selection remained: %#v", userCfg.Collections)
	}
	repoCfg, err := config.LoadRootConfigFromPath(config.ProjectConfigPath(repoRoot))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(repoCfg.Collections.Include, "repo-bundle") {
		t.Fatalf("repo selection was changed by user-scope removal: %#v", repoCfg.Collections)
	}
}

func TestRemoveCollectionClearsShorthandSelection(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", t.TempDir())
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ncollections: [shorthand-bundle]\n")

	if _, err := Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "shorthand-bundle", Scope: ScopeRepo, ScopeSpecified: true,
	}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCfg.Collections.Include) != 0 || len(rootCfg.Collections.Exclude) != 0 {
		t.Fatalf("root collections = %#v, want shorthand removed without exclusion", rootCfg.Collections)
	}
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rootData), "shorthand-bundle") || strings.Contains(string(rootData), "collections") {
		t.Fatalf("shorthand selection masked rather than removed:\n%s", rootData)
	}
}

func TestRemoveTopLevelMachineCollectionRejectsRepoScope(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", t.TempDir())
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	config.SetTestV3OrganizationConfigPath(t, orgPath)
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v2\ncollections:\n  include: [machine-bundle]\n")
	writeFile(t, config.ProjectConfigPath(repoRoot), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")

	_, err := Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "machine-bundle", Scope: ScopeRepo, ScopeSpecified: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot exclude collection") {
		t.Fatalf("error = %v, want machine-to-repo collection boundary rejection", err)
	}
}

func TestRemoveTopLevelMachineCollectionRejectsLocalScope(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", t.TempDir())
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	config.SetTestV3OrganizationConfigPath(t, orgPath)
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v2\ncollections:\n  include: [machine-bundle]\n")
	writeFile(t, config.ProjectConfigPath(repoRoot), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")

	_, err := Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "machine-bundle", Scope: ScopeLocal, ScopeSpecified: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot exclude collection") {
		t.Fatalf("error = %v, want machine-to-local collection boundary rejection", err)
	}
}

func TestRemoveTopLevelOrganizationCollectionInfersUserScope(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", t.TempDir())
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	config.SetTestV3OrganizationConfigPath(t, orgPath)
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "machine-skill", "machine skill", nil)
	orgData := "apiVersion: agent-marketplace/v2\nregistries:\n  - path: \"" + registryRoot + "\"\n    source_harness: factory\n    collections: [machine-bundle]\ncollections:\n  include: [machine-bundle]\n"
	writeFile(t, orgPath, orgData)
	writeFile(t, config.ProjectConfigPath(repoRoot), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")

	result, err := Remove(context.Background(), RemoveOptions{RepoRoot: repoRoot, ResourceName: "machine-bundle"})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || !result.IsCollection || !result.InferredUserScope {
		t.Fatalf("result = %#v, want collection removal inferred to user scope", result)
	}
	userPath, pathErr := config.GlobalConfigPath()
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	userData, pathErr := os.ReadFile(userPath)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if !strings.Contains(string(userData), "apiVersion: agent-marketplace/v4") {
		t.Fatalf("missing user root should become canonical v4:\n%s", userData)
	}
	if strings.Contains(string(userData), "registries:") || strings.Contains(string(userData), "collections:") {
		t.Fatalf("v4 user config carries legacy selector keys:\n%s", userData)
	}
	userCfg, pathErr := config.LoadRootConfigFromPath(userPath)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if len(userCfg.Registries) == 0 {
		t.Fatalf("v4 user config has no copied provider source:\n%s", userData)
	}
	excluded := false
	for _, entry := range userCfg.Registries {
		if entry.V4Kind != config.V4SourceGit && entry.V4Kind != config.V4SourceDirectory {
			t.Fatalf("copied provider is not a git/directory source: %#v", entry)
		}
		if slices.Contains(entry.Collections.Exclude, "machine-bundle") {
			excluded = true
		}
	}
	if !excluded {
		t.Fatalf("no source-bound collection exclusion written:\n%s", userData)
	}
	gotOrg, pathErr := os.ReadFile(orgPath)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if string(gotOrg) != orgData {
		t.Fatalf("organization config changed:\n%s", gotOrg)
	}
}

func TestCollectionRequiredInConfigScopesRequirednessToProvider(t *testing.T) {
	// Without a provider identity, only an explicit required collection include
	// can be attributed safely. A required auto/tag provider must not make an
	// unrelated optional collection immutable.
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: "git@example.com:required", Ref: "main", Required: true},
		{URL: "git@example.com:required", Ref: "main", Required: true, Tags: []string{"security"}},
		{URL: "git@example.com:other", Ref: "main", Required: true, Collections: config.KindSelector{Include: []string{"bundle"}}},
	}}
	if collectionRequiredInConfig(cfg, "bundle") != true {
		t.Fatal("explicit required collection selection should remain required")
	}

	selected := config.RegistryEntry{URL: "git@example.com:optional", Ref: "main", Source: config.RepoCommitted}
	if collectionRequiredInRegistry(cfg, "bundle", selected) {
		t.Fatal("required policy from an unrelated provider made optional collection immutable")
	}

	requiredAuto := selected
	requiredAuto.Required = true
	if !collectionRequiredInRegistry(&config.Config{Registries: []config.RegistryEntry{requiredAuto}}, "bundle", selected) {
		t.Fatal("selected required provider must remain required")
	}

	requiredTags := config.RegistryEntry{URL: selected.URL, Ref: selected.Ref, Source: selected.Source, Required: true, Tags: []string{"security"}}
	tagCfg := &config.Config{Registries: []config.RegistryEntry{requiredTags}}
	if !collectionRequiredInRegistry(tagCfg, "bundle", selected) {
		t.Fatal("tag policy without collection catalog tags must preserve required immunity")
	}
	if !collectionRequiredInRegistry(tagCfg, "bundle", selected, []string{"security", "audit"}) {
		t.Fatal("matching collection catalog tags must preserve required policy")
	}
	if collectionRequiredInRegistry(tagCfg, "bundle", selected, []string{"docs"}) {
		t.Fatal("non-matching collection catalog tags must not preserve required policy")
	}
}

func TestCollectionRequiredInRegistryMatchesPhysicalLocationAcrossScopes(t *testing.T) {
	physical := config.RegistryEntry{URL: "git@example.com:policy.git", Path: "collections", Ref: "main"}
	for _, requiredSource := range []config.Source{config.Organization, config.UserPersonal} {
		t.Run(requiredSource.String(), func(t *testing.T) {
			required := physical
			required.Source = requiredSource
			required.Required = true
			required.Collections.Include = []string{"security-bundle"}
			selected := physical
			selected.Source = config.RepoCommitted
			if !collectionRequiredInRegistry(&config.Config{Registries: []config.RegistryEntry{required}}, "security-bundle", selected) {
				t.Fatalf("required %s provider at the same physical location was ignored", requiredSource)
			}
		})
	}

	unrelated := physical
	unrelated.URL = "git@example.com:other.git"
	unrelated.Required = true
	unrelated.Collections.Include = []string{"security-bundle"}
	if collectionRequiredInRegistry(&config.Config{Registries: []config.RegistryEntry{unrelated}}, "security-bundle", physical) {
		t.Fatal("required provider at an unrelated physical location made collection immutable")
	}
}

func TestRemoveRequiredMachineCollectionRejectsRepoScope(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	t.Setenv("HOME", t.TempDir())
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	writeFile(t, config.ProjectConfigPath(repoRoot), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, userPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: /machine\n    required: true\n    collections: [required-bundle]\n")

	_, err = Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "required-bundle", Scope: ScopeRepo, ScopeSpecified: true,
	})
	if err == nil || !strings.Contains(err.Error(), "required and cannot be removed") {
		t.Fatalf("error = %v, want required machine collection rejection", err)
	}
}

func assertResourceNotResolved(t *testing.T, repoRoot, name string) {
	t.Helper()
	resolved := Resolve(context.Background(), ResolveOptions{RepoRoot: repoRoot, SkipInventory: true})
	if len(resolved.Errors) != 0 {
		t.Fatalf("resolve errors = %v", resolved.Errors)
	}
	for _, item := range resolved.Resolved {
		if item.Name == name {
			t.Fatalf("resource %q remained enabled: %#v", name, item)
		}
	}
}

func TestRemovePathOnlyLocalResourceUsesOnlyRequestedRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	localA, localB := t.TempDir(), t.TempDir()
	writeSkill(t, localA, "selected", "selected local skill", nil)
	writeSkill(t, localB, "selected", "other local skill", nil)
	writeFile(t, config.ProjectConfigPath(repoRoot), fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - path: %q\n    skills: [selected]\n  - path: %q\n    skills: [selected]\n", localA, localB))

	result, err := Remove(context.Background(), RemoveOptions{
		RepoRoot: repoRoot, ResourceName: "selected", FromURL: localA,
		Scope: ScopeRepo, ScopeSpecified: true,
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if result == nil || result.RemovedName != "selected" {
		t.Fatalf("result = %#v", result)
	}
	cfg, err := config.LoadRootConfigFromPath(config.ProjectConfigPath(repoRoot))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("registries = %#v, want both local aliases preserved", cfg.Registries)
	}
	if len(cfg.Registries[0].Skills.Include) != 0 || len(cfg.Registries[0].Skills.Exclude) != 0 {
		t.Fatalf("selected alias = %#v, want its explicit selection removed", cfg.Registries[0])
	}
	if !slices.Contains(cfg.Registries[1].Skills.Include, "selected") || slices.Contains(cfg.Registries[1].Skills.Exclude, "selected") {
		t.Fatalf("other alias = %#v, want untouched", cfg.Registries[1])
	}
	resolved := Resolve(context.Background(), ResolveOptions{RepoRoot: repoRoot, SkipInventory: true})
	if len(resolved.Errors) != 0 || len(resolved.Resolved) != 1 || resolved.Resolved[0].RegistryURL != localB {
		t.Fatalf("resolved = %#v, errors = %v; want only the other local registry", resolved.Resolved, resolved.Errors)
	}
}

func TestRemoveAdHocRefTargetsOnlyNamedRef(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "shared", "shared skill", nil)
	rootPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, rootPath, fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: %q\n    ref: main\n    path: skills\n    skills: [shared]\n  - url: %q\n    ref: release\n    path: skills\n    skills: [shared]\n", registryRoot, registryRoot))

	if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: repoRoot, ResourceName: "shared", FromURL: registryRoot, FromRef: "release", FromPath: "skills", Scope: ScopeRepo, ScopeSpecified: true}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	cfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 2 || !slices.Contains(cfg.Registries[0].Skills.Include, "shared") || slices.Contains(cfg.Registries[0].Skills.Exclude, "shared") || slices.Contains(cfg.Registries[1].Skills.Include, "shared") || !slices.Contains(cfg.Registries[1].Skills.Exclude, "shared") {
		t.Fatalf("registries = %#v, want only release changed", cfg.Registries)
	}
	resolved := Resolve(context.Background(), ResolveOptions{RepoRoot: repoRoot, SkipInventory: true})
	if len(resolved.Errors) != 0 || len(resolved.Resolved) != 1 || resolved.Resolved[0].Ref != "main" {
		t.Fatalf("resolved after release exclusion = %#v, errors = %v; want main only", resolved.Resolved, resolved.Errors)
	}

	if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: repoRoot, FromURL: registryRoot, FromRef: "release", FromPath: "skills", Scope: ScopeRepo, ScopeSpecified: true}); err != nil {
		t.Fatalf("whole registry Remove: %v", err)
	}
	cfg, err = config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 1 || cfg.Registries[0].Ref != "main" {
		t.Fatalf("registries after whole removal = %#v, want main only", cfg.Registries)
	}
}

func TestRemoveNamedFromCollectionRejectsMachineToRepoBoundary(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	config.SetTestV3OrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	registryRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, config.ProjectConfigPath(repoRoot), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - path: %q\n    collections: [machine-bundle]\n", registryRoot))

	_, err := Remove(context.Background(), RemoveOptions{RepoRoot: repoRoot, ResourceName: "machine-bundle", FromURL: registryRoot, Scope: ScopeRepo, ScopeSpecified: true})
	if err == nil || !strings.Contains(err.Error(), "cannot exclude registry") {
		t.Fatalf("error = %v, want machine-to-repo boundary rejection", err)
	}
}

func TestRegistryPathOwns(t *testing.T) {
	cases := []struct {
		entry, artifact string
		want            bool
	}{
		{"", "anything/at/all", true},                          // URL-only targeting
		{"teams/pm/plugins", "teams/pm/plugins", true},         // exact
		{"teams/pm/plugins", "teams/pm/plugins/pm-data", true}, // artifact beneath entry
		{"teams/pm/plugins", "teams/pm/plugins-other", false},  // not a component boundary
		{"teams/pm/plugins", "teams/pm", false},                // entry deeper than artifact
		{"teams/pm/plugins", "other/tree", false},              // unrelated
	}
	for _, c := range cases {
		if got := registryPathOwns(c.entry, c.artifact); got != c.want {
			t.Errorf("registryPathOwns(%q, %q) = %v, want %v", c.entry, c.artifact, got, c.want)
		}
	}
}

func TestRegistryEntryForResolved(t *testing.T) {
	target := resolver.ResolvedArtifact{
		Entry:        catalog.Entry{Kind: ir.KindSkill, Name: "review", SourceHarness: "claude"},
		RegistryURL:  " https://example.test/repo.git ",
		RegistryPath: "plugins/team",
		Ref:          " release ",
		V4Kind:       config.V4SourceGit,
		Source:       config.RepoCommitted,
	}
	entry := registryEntryForResolved(&target)
	if entry.URL != "https://example.test/repo.git" || entry.Ref != "release" || entry.Path != "plugins/team" {
		t.Fatalf("entry = %#v", entry)
	}
	if entry.V4Kind != config.V4SourceGit || entry.SourceHarness != "claude" {
		t.Fatalf("v4 identity lost: %#v", entry)
	}
}
