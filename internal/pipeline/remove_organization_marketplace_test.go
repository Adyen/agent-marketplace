package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// spec: resource-removal/organization-marketplace-opt-out-preserves-required-and-scope-boundaries
// spec: resource-removal/organization-collection-removal-explains-per-member-opt-out
func TestRemoveOrganizationMarketplaceRefusals(t *testing.T) {
	for _, choice := range []string{"required-member", "required-direct", "repo", "local", "collection"} {
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
			case "collection":
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
			if choice == "collection" && !strings.Contains(err.Error(), "member") {
				t.Fatalf("collection refusal lacks member opt-out guidance: %v", err)
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
func TestRemoveOrganizationMarketplaceMemberScopeAmbiguity(t *testing.T) {
	f := newOrganizationMarketplaceRemovalFixture(t, false, false)
	source := t.TempDir()
	writeSkill(t, source, "review", "Repository review", nil)
	repoPath := config.ProjectConfigPath(f.repo)
	writeFile(t, repoPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: %q, source_harness: factory}\nresources:\n  local: [skill/review]\n", source))
	before, err := os.ReadFile(f.userPath)
	if err != nil {
		t.Fatal(err)
	}
	opts := RemoveOptions{RepoRoot: f.repo, ResourceName: "review", Scope: ScopeRepo}
	if _, err := Remove(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "--scope") {
		t.Fatalf("expected scope ambiguity, got %v", err)
	}
	if after, err := os.ReadFile(f.userPath); err != nil || string(after) != string(before) {
		t.Fatalf("ambiguous removal changed user config: %v", err)
	}
	opts.Scope, opts.ScopeSpecified = ScopeUser, true
	if _, err := Remove(context.Background(), opts); err != nil {
		t.Fatalf("user-scoped opt-out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.repo, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("repository resource removed by user opt-out: %v", err)
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
