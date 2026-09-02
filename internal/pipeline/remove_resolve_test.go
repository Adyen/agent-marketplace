package pipeline

import (
	"context"
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

func TestRemoveOrExcludeCollectionDeletesRootSelection(t *testing.T) {
	rootPath := config.ProjectConfigPath(t.TempDir())
	writeFile(t, rootPath, "apiVersion: agent-marketplace/v2\ncollections:\n  include: [root-bundle]\n")
	if err := removeOrExcludeCollection(rootPath, "root-bundle"); err != nil {
		t.Fatal(err)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(rootCfg.Collections.Include, "root-bundle") || slices.Contains(rootCfg.Collections.Exclude, "root-bundle") {
		t.Fatalf("root collections = %#v, want selection removed without exclusion", rootCfg.Collections)
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

func TestDisableCollectionIsRegistrySpecific(t *testing.T) {
	rootPath := config.ProjectConfigPath(t.TempDir())
	writeFile(t, rootPath, "apiVersion: agent-marketplace/v2\nregistries:\n  - path: /registry-a\n    collections: [shared-bundle]\n  - path: /registry-b\n    collections: [shared-bundle]\n")
	if err := disableCollectionInRegistry(rootPath, "/registry-a", "", "", "shared-bundle"); err != nil {
		t.Fatal(err)
	}
	rootCfg, err := config.LoadRootConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCfg.Registries) != 2 || !slices.Contains(rootCfg.Registries[0].Collections.Exclude, "shared-bundle") || !slices.Contains(rootCfg.Registries[1].Collections.Include, "shared-bundle") {
		t.Fatalf("registries = %#v, want only registry-a collection excluded", rootCfg.Registries)
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
