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

	"github.com/Adyen/agent-marketplace/internal/config"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type organizationMarketplaceRemovalFixture struct {
	home, repo, orgPath, userPath string
	fake                          *layerMarketplaceServer
}

func newOrganizationMarketplaceRemovalFixture(t *testing.T, direct, required bool) organizationMarketplaceRemovalFixture {
	t.Helper()
	f := organizationMarketplaceRemovalFixture{home: isolateConsumerPipeline(t), repo: t.TempDir()}
	t.Setenv("TMPDIR", t.TempDir())
	t.Chdir(f.repo)
	f.fake = newLayerMarketplaceServer(t)
	registry := f.fake.addRegistry(t, "source")
	var members []hccartifact.CollectionMemberEntry
	for _, name := range []string{"review", "audit"} {
		f.fake.addSkill(t, registry, name)
		members = append(members, hccartifact.CollectionMemberEntry{
			RegistryID: string(registry.ID), Kind: "skill", Name: name,
			Revision: string(f.fake.resources["source/skill/"+name].revision.ContentDigest),
		})
	}
	f.fake.addCollection(t, registry, "bundle", members)
	f.orgPath = filepath.Join(t.TempDir(), "config.yaml")
	config.SetTestOrganizationConfigPath(t, f.orgPath)
	selection := "source/collection/bundle"
	if direct {
		selection = "source/skill/review, source/skill/audit"
	}
	writeFile(t, f.orgPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  org: {marketplace: %s}\nresources:\n  org:\n    include: [%s]\n    required: %t\n", f.fake.URL, selection, required))
	f.userPath = filepath.Join(f.home, ".agents", "config.yaml")
	writeFileMode(t, f.userPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\n# Keep user preferences.\nsources:\n  company: {marketplace: %s}\n", f.fake.URL), 0o600)
	result := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, f.repo), NoBackup: true})
	if len(result.Errors) != 0 {
		t.Fatal(result.Errors)
	}
	for _, name := range []string{"review", "audit"} {
		if _, err := os.Stat(filepath.Join(f.home, ".factory", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("initial member %s missing: %v", name, err)
		}
	}
	return f
}

// spec: resource-removal/remove-an-optional-organization-marketplace-collection-member
// spec: resource-removal/remove-an-optional-direct-organization-marketplace-resource
func TestRemoveOptionalOrganizationMarketplaceResource(t *testing.T) {
	for _, choice := range []string{"default", "user", "from-user", "direct", "missing-user"} {
		t.Run(choice, func(t *testing.T) {
			f := newOrganizationMarketplaceRemovalFixture(t, choice == "direct", false)
			before, err := os.ReadFile(f.orgPath)
			if err != nil {
				t.Fatal(err)
			}
			if choice == "missing-user" {
				if err := os.Remove(f.userPath); err != nil {
					t.Fatal(err)
				}
			}
			opts := RemoveOptions{RepoRoot: f.repo, ResourceName: "review", Kind: ir.KindSkill, HasKind: true, Scope: ScopeRepo}
			if choice == "user" || choice == "from-user" || choice == "direct" {
				opts.Scope, opts.ScopeSpecified = ScopeUser, true
			}
			if choice == "from-user" {
				opts.FromURL = f.fake.URL
			}
			result, err := Remove(context.Background(), opts)
			if err != nil {
				t.Fatalf("remove optional organization resource: %v", err)
			}
			if result.InferredUserScope != !opts.ScopeSpecified {
				t.Fatalf("inferred user scope = %t", result.InferredUserScope)
			}
			cfg, err := config.LoadRootConfigFromPath(f.userPath)
			if err != nil || len(cfg.MarketplaceExclusions) != 1 || cfg.MarketplaceExclusions[0].Locator != "source/skill/review" {
				t.Fatalf("user exclusions = %#v, %v", cfg, err)
			}
			after, err := os.ReadFile(f.userPath)
			if err != nil {
				t.Fatal(err)
			}
			if choice != "missing-user" && (!strings.Contains(string(after), "company:") || !strings.Contains(string(after), "# Keep user preferences.")) {
				t.Fatalf("user alias or comment lost:\n%s", after)
			}
			if _, err := os.Stat(filepath.Join(f.home, ".factory", "skills", "review")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("excluded member remains: %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.home, ".factory", "skills", "audit", "SKILL.md")); err != nil {
				t.Fatalf("sibling removed: %v", err)
			}
			if after, err := os.ReadFile(f.orgPath); err != nil || string(after) != string(before) {
				t.Fatalf("organization changed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(f.orgPath), "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("organization lock created: %v", err)
			}
			assertNoStagingResidue(t)
		})
	}
}

// spec: resource-removal/remove-an-optional-direct-organization-marketplace-resource
func TestRemoveOrganizationMarketplaceResourceAlias(t *testing.T) {
	for _, name := range []string{"review", "review-copy"} {
		t.Run(name, func(t *testing.T) {
			f := newOrganizationMarketplaceRemovalFixture(t, true, false)
			organization := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  org: {marketplace: %s}\nresources:\n  org:\n    include:\n      - source/skill/review: {as: review-copy}\n      - source/skill/audit\n", f.fake.URL)
			writeFile(t, f.orgPath, organization)
			synced := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, f.repo), NoBackup: true})
			if len(synced.Errors) != 0 {
				t.Fatal(synced.Errors)
			}
			if _, err := os.Stat(filepath.Join(f.home, ".factory", "skills", "review-copy", "SKILL.md")); err != nil {
				t.Fatalf("aliased resource not installed: %v", err)
			}
			if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: f.repo, ResourceName: name}); err != nil {
				t.Fatalf("remove aliased resource: %v", err)
			}
			cfg, err := config.LoadRootConfigFromPath(f.userPath)
			if err != nil || len(cfg.MarketplaceExclusions) != 1 || cfg.MarketplaceExclusions[0].Locator != "source/skill/review" {
				t.Fatalf("canonical exclusion missing: %#v / %v", cfg, err)
			}
			if _, err := os.Stat(filepath.Join(f.home, ".factory", "skills", "review-copy")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("aliased resource remains: %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.home, ".factory", "skills", "audit", "SKILL.md")); err != nil {
				t.Fatalf("sibling removed: %v", err)
			}
			if data, err := os.ReadFile(f.orgPath); err != nil || string(data) != organization {
				t.Fatalf("organization changed: %v", err)
			}
		})
	}
}

// spec: resource-removal/organization-marketplace-opt-out-preserves-required-and-scope-boundaries
// spec: resource-removal/organization-collection-removal-explains-per-member-opt-out
// spec: resource-removal/optional-organization-collection-refusal-is-identifiable
func TestRemoveOrganizationMarketplaceRefusals(t *testing.T) {
	for _, choice := range []string{"required-member", "required-direct", "required-collection", "repo", "local", "collection"} {
		t.Run(choice, func(t *testing.T) {
			f := newOrganizationMarketplaceRemovalFixture(t, choice == "required-direct", strings.HasPrefix(choice, "required"))
			paths := []string{f.orgPath, f.userPath, filepath.Join(f.home, ".factory", "skills", "review", "SKILL.md"), filepath.Join(f.home, ".factory", "skills", "audit", "SKILL.md")}
			before := make(map[string]string)
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = string(data)
			}
			opts := RemoveOptions{RepoRoot: f.repo, ResourceName: "review", Scope: ScopeUser, ScopeSpecified: true}
			switch choice {
			case "repo":
				opts.Scope = ScopeRepo
			case "local":
				opts.Scope = ScopeLocal
			case "collection", "required-collection":
				opts.ResourceName, opts.Kind, opts.HasKind = "bundle", ir.KindCollection, true
			}
			_, err := Remove(context.Background(), opts)
			if err == nil {
				t.Fatal("expected removal refusal")
			}
			if choice == "repo" || choice == "local" {
				var boundary *config.ScopeBoundaryError
				if !errors.As(err, &boundary) || !strings.Contains(err.Error(), "--scope user") {
					t.Fatalf("expected actionable scope boundary, got %v", err)
				}
			}
			if choice == "collection" && !errors.Is(err, ErrMarketplaceRemoveOrganizationCollection) {
				t.Fatalf("collection refusal lacks its stable identity: %v", err)
			}
			if strings.HasPrefix(choice, "required") && errors.Is(err, ErrMarketplaceRemoveOrganizationCollection) {
				t.Fatalf("required refusal misclassified as optional collection refusal: %v", err)
			}
			for path, original := range before {
				if data, err := os.ReadFile(path); err != nil || string(data) != original {
					t.Fatalf("refusal changed %s: %v", path, err)
				}
			}
			for _, path := range []string{config.ProjectConfigPath(f.repo), config.LocalConfigPath(f.repo), filepath.Join(filepath.Dir(f.orgPath), "lock.yaml")} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("refusal created %s: %v", path, err)
				}
			}
			assertNoStagingResidue(t)
		})
	}
}

// spec: resource-removal/organization-member-opt-out-disambiguates-kind-and-scope
// spec: resource-removal/catalog-selected-repository-resources-require-scope-disambiguation
func TestRemoveOrganizationMarketplaceMemberScopeAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name, selection string
		direct, from    bool
	}{
		{name: "explicit", selection: "[skill/review]"},
		{name: "all", selection: "{all: true}"},
		{name: "tags", selection: "{tags: [team]}"},
		{name: "collection", selection: "[collection/team]"},
		{name: "direct-all", selection: "{all: true}", direct: true},
		{name: "endpoint-all", selection: "{all: true}", from: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOrganizationMarketplaceRemovalFixture(t, tc.direct, false)
			source := t.TempDir()
			writeSkill(t, source, "review", "Repository review", []string{"team"})
			if tc.name == "collection" {
				writeFile(t, filepath.Join(source, "collections", "team", "COLLECTION.yaml"), "name: team\nresources:\n  - name: review\n    kind: skill\n")
			}
			repoPath := config.ProjectConfigPath(f.repo)
			writeFile(t, repoPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: %q, source_harness: factory}\nresources:\n  local: %s\n", source, tc.selection))
			synced := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, f.repo), NoBackup: true})
			if len(synced.Errors) != 0 {
				t.Fatal(synced.Errors)
			}
			repoSkill := filepath.Join(f.repo, ".factory", "skills", "review", "SKILL.md")
			orgSkill := filepath.Join(f.home, ".factory", "skills", "review", "SKILL.md")
			auditSkill := filepath.Join(f.home, ".factory", "skills", "audit", "SKILL.md")
			locks := []string{filepath.Join(f.repo, ".agents", "lock.yaml"), filepath.Join(f.home, ".agents", "lock.yaml"), filepath.Join(filepath.Dir(f.orgPath), "lock.yaml")}
			paths := append([]string{repoPath, f.userPath, f.orgPath, repoSkill, orgSkill, auditSkill}, locks...)
			before := make(map[string][]byte)
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if errors.Is(err, os.ErrNotExist) && slices.Contains(locks, path) {
					before[path] = nil
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				before[path] = data
			}
			opts := RemoveOptions{RepoRoot: f.repo, ResourceName: "review", Scope: ScopeRepo}
			if _, err := Remove(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "--scope") {
				t.Fatalf("expected scope ambiguity, got %v", err)
			}
			for path, original := range before {
				data, err := os.ReadFile(path)
				if original == nil {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("ambiguous removal created %s: %v", path, err)
					}
					continue
				}
				if err != nil || string(data) != string(original) {
					t.Fatalf("ambiguous removal changed %s: %v", path, err)
				}
			}
			opts.Scope, opts.ScopeSpecified = ScopeUser, true
			if tc.from {
				opts.FromURL = f.fake.URL
			}
			if _, err := Remove(context.Background(), opts); err != nil {
				t.Fatalf("user-scoped opt-out: %v", err)
			}
			cfg, err := config.LoadRootConfigFromPath(f.userPath)
			if err != nil || len(cfg.MarketplaceExclusions) != 1 || cfg.MarketplaceExclusions[0].Locator != "source/skill/review" || cfg.MarketplaceExclusions[0].URL != f.fake.URL {
				t.Fatalf("user exclusions = %#v, %v", cfg, err)
			}
			if _, err := os.Stat(filepath.Dir(orgSkill)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("organization member remains: %v", err)
			}
			for _, path := range []string{repoPath, f.orgPath, repoSkill, auditSkill} {
				if data, err := os.ReadFile(path); err != nil || string(data) != string(before[path]) {
					t.Fatalf("user opt-out changed %s: %v", path, err)
				}
			}
			assertNoStagingResidue(t)
		})
	}
}

// spec: resource-removal/catalog-selected-repository-resources-require-scope-disambiguation
func TestRemoveOrganizationMarketplaceCollisionRepoScope(t *testing.T) {
	f := newOrganizationMarketplaceRemovalFixture(t, false, false)
	source := t.TempDir()
	writeSkill(t, source, "review", "Repository review", nil)
	repoPath := config.ProjectConfigPath(f.repo)
	writeFile(t, repoPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: %q, source_harness: factory}\nresources:\n  local: {all: true}\n", source))
	synced := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, f.repo), NoBackup: true})
	if len(synced.Errors) != 0 {
		t.Fatal(synced.Errors)
	}
	before := make(map[string][]byte)
	for _, path := range []string{f.orgPath, f.userPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: f.repo, ResourceName: "review", Scope: ScopeRepo, ScopeSpecified: true}); err != nil {
		t.Fatalf("repository-scoped removal: %v", err)
	}
	cfg, err := config.LoadRootConfigFromPath(repoPath)
	if err != nil || len(cfg.Registries) != 1 || !cfg.Registries[0].V4All || !slices.Equal(cfg.Registries[0].Skills.Exclude, []string{"review"}) {
		t.Fatalf("repository exclusion = %#v / %v", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(f.repo, ".factory", "skills", "review")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository resource remains: %v", err)
	}
	for _, name := range []string{"review", "audit"} {
		if _, err := os.Stat(filepath.Join(f.home, ".factory", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("organization member removed: %v", err)
		}
	}
	for path, original := range before {
		if data, err := os.ReadFile(path); err != nil || string(data) != string(original) {
			t.Fatalf("repository removal changed %s: %v", path, err)
		}
	}
}

func TestRemoveOrganizationMarketplaceStagesBeforeReconciliation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			f := newOrganizationMarketplaceRemovalFixture(t, false, false)
			before, err := os.ReadFile(f.userPath)
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				if err := os.Remove(f.userPath); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			refusal := errors.New("reconciliation refused")
			_, err = Remove(context.Background(), RemoveOptions{
				RepoRoot: f.repo, ResourceName: "review", Scope: ScopeUser, ScopeSpecified: true,
				DependencyReconcile: func(_ context.Context, _, _ string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
					called = true
					if len(edits) != 1 || !strings.Contains(string(edits[0].Bytes), "source/skill/review") {
						t.Fatalf("missing staged exclusion: %#v", edits)
					}
					return DependencyLockView{}, refusal
				},
			})
			if !called || !errors.Is(err, refusal) {
				t.Fatalf("reconciliation = called %t, error %v", called, err)
			}
			after, err := os.ReadFile(f.userPath)
			if missing {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("refusal created user root: %v", err)
				}
			} else if err != nil || string(after) != string(before) {
				t.Fatalf("refusal changed user root: %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.home, ".factory", "skills", "review", "SKILL.md")); err != nil {
				t.Fatalf("refusal removed member: %v", err)
			}
			assertNoStagingResidue(t)
		})
	}
}
