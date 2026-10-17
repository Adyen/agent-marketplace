package pipeline

import (
	"bytes"
	"context"
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

// spec: resource-removal/disambiguate-collections-before-removal
func TestRemoveUndiscoverableCollectionAmbiguity(t *testing.T) {
	for _, from := range []bool{false, true} {
		for _, choice := range []string{"unqualified", "skill", "collection", "single-collection", "single-missing-collection"} {
			t.Run(fmt.Sprintf("from=%t/%s", from, choice), func(t *testing.T) {
				isolateConsumerPipeline(t)
				repo, source := t.TempDir(), t.TempDir()
				t.Chdir(repo)
				writeSkill(t, source, "keep", "Keep selected", nil)
				writeSkill(t, source, "member", "Collection member", nil)
				writeFile(t, filepath.Join(source, "collections", "team", "COLLECTION.yaml"), "name: team\nresources:\n  - name: member\n    kind: skill\n")
				selected := "collection/team, skill/team, skill/keep"
				if strings.HasPrefix(choice, "single-") {
					selected = "collection/team, skill/keep"
				}
				path := config.ProjectConfigPath(repo)
				writeFile(t, path, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  local: {directory: %q, source_harness: factory}\nresources:\n  local: [%s]\n", source, selected))
				synced := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, repo), NoBackup: true})
				if len(synced.Errors) != 0 || len(synced.Resolved) != 2 {
					t.Fatalf("invalid fixture: %v / %#v", synced.Errors, synced.Resolved)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				memberPath := filepath.Join(repo, ".factory", "skills", "member", "SKILL.md")
				memberBefore, err := os.ReadFile(memberPath)
				if err != nil {
					t.Fatal(err)
				}
				if choice == "single-missing-collection" {
					if err := os.Rename(filepath.Join(source, "collections", "team"), filepath.Join(t.TempDir(), "team")); err != nil {
						t.Fatal(err)
					}
				}
				opts := RemoveOptions{RepoRoot: repo, ResourceName: "team", Scope: ScopeRepo}
				if from {
					opts.FromURL = source
				}
				if choice == "skill" || choice == "collection" {
					opts.HasKind = true
					opts.Kind = ir.KindSkill
					if choice == "collection" {
						opts.Kind = ir.KindCollection
					}
				}
				result, err := Remove(context.Background(), opts)
				if choice == "unqualified" {
					if err == nil || !strings.Contains(err.Error(), "--kind") {
						t.Fatalf("expected kind ambiguity, got %#v / %v", result, err)
					}
					after, readErr := os.ReadFile(path)
					memberAfter, memberErr := os.ReadFile(memberPath)
					if readErr != nil || memberErr != nil || !bytes.Equal(after, before) || !bytes.Equal(memberAfter, memberBefore) {
						t.Fatalf("refusal changed configuration or collection output: %v / %v", readErr, memberErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				cfg, err := config.LoadRootConfigFromPath(path)
				if err != nil || len(cfg.Registries) != 1 {
					t.Fatalf("load removal result: %#v / %v", cfg, err)
				}
				entry := cfg.Registries[0]
				wantSkills := []string{"keep", "team"}
				if choice == "skill" || strings.HasPrefix(choice, "single-") {
					wantSkills = []string{"keep"}
				}
				if !slices.Equal(entry.Skills.Include, wantSkills) || result.IsCollection != (choice != "skill") {
					t.Fatalf("wrong selection removed: %#v / %#v", entry, result)
				}
				if choice == "skill" {
					if !slices.Equal(entry.Collections.Include, []string{"team"}) {
						t.Fatalf("collection selection changed: %#v", entry.Collections)
					}
					if _, err := os.Stat(memberPath); err != nil {
						t.Fatalf("collection output removed: %v", err)
					}
				} else if len(entry.Collections.Include) != 0 {
					t.Fatalf("collection selection remains: %#v", entry.Collections)
				}
				if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "keep", "SKILL.md")); err != nil {
					t.Fatalf("unrelated output removed: %v", err)
				}
			})
		}
	}
}

// spec: resource-removal/disambiguate-marketplace-pins-before-removal
func TestRemoveUndiscoverableMarketplaceAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		choice          string
		marketplaceUser bool
		collection      bool
	}{
		{"unqualified", false, false}, {"command", false, false}, {"skill", false, false},
		{"unqualified", true, false}, {"repo", true, false}, {"user", true, false},
		{"unqualified", false, true}, {"command", false, true}, {"collection", false, true},
	} {
		t.Run(fmt.Sprintf("%s/user=%t/collection=%t", tc.choice, tc.marketplaceUser, tc.collection), func(t *testing.T) {
			home := isolateConsumerPipeline(t)
			repo, source := t.TempDir(), t.TempDir()
			t.Chdir(repo)
			writeSkill(t, source, "keep", "Keep selected", nil)
			path := config.ProjectConfigPath(repo)
			writeFile(t, path, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  local: {directory: %q, source_harness: factory}\nresources:\n  local: [command/team, skill/keep]\n", source))
			fake := newLayerMarketplaceServer(t)
			registry := fake.addRegistry(t, "source")
			fake.addSkill(t, registry, "team")
			kind := "skill"
			if tc.collection {
				kind = "collection"
				resource := fake.resources["source/skill/team"]
				fake.addCollection(t, registry, "team", []hccartifact.CollectionMemberEntry{{RegistryID: string(registry.ID), Kind: "skill", Name: "team", Revision: string(resource.revision.ContentDigest)}})
			}
			pins := layerPreparePins(t, fake, kind, "team")
			marketplacePath := path
			marketplaceSource := config.RepoCommitted
			if tc.marketplaceUser {
				marketplacePath = filepath.Join(home, ".agents", "config.yaml")
				marketplaceSource = config.UserPersonal
				writeFile(t, marketplacePath, "apiVersion: agent-marketplace/v4\n")
			}
			if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, marketplacePath, marketplaceSource, func(m *MarketplaceLayerMutation) error {
				return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins)
			}, nil); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			marketplaceBefore, err := os.ReadFile(marketplacePath)
			if err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(filepath.Dir(marketplacePath), "lock.yaml")
			lockBefore, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			opts := RemoveOptions{RepoRoot: repo, ResourceName: "team", Scope: ScopeRepo}
			switch tc.choice {
			case "command":
				opts.Kind, opts.HasKind = ir.KindCommand, true
			case "skill":
				opts.Kind, opts.HasKind = ir.KindSkill, true
			case "collection":
				opts.Kind, opts.HasKind = ir.KindCollection, true
			case "repo":
				opts.ScopeSpecified = true
			case "user":
				opts.Scope, opts.ScopeSpecified = ScopeUser, true
			}
			_, err = Remove(context.Background(), opts)
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.choice == "unqualified" {
				if err == nil || !strings.Contains(err.Error(), "--kind") || !bytes.Equal(after, before) {
					t.Fatalf("expected unchanged config and kind ambiguity, got %v\n%s", err, after)
				}
				marketplaceAfter, err := os.ReadFile(marketplacePath)
				if err != nil || !bytes.Equal(marketplaceAfter, marketplaceBefore) {
					t.Fatalf("refusal changed Marketplace config: %v\n%s", err, marketplaceAfter)
				}
				lockAfter, err := os.ReadFile(lockPath)
				if err != nil || !bytes.Equal(lockAfter, lockBefore) {
					t.Fatalf("refusal changed acquisition lock: %v\n%s", err, lockAfter)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadRootConfigFromPath(path)
			if err != nil || len(cfg.Registries) != 1 {
				t.Fatalf("load removal result: %#v / %v", cfg, err)
			}
			marketplaceCfg, err := config.LoadRootConfigFromPath(marketplacePath)
			if err != nil {
				t.Fatal(err)
			}
			wantCommand := tc.choice == "skill" || tc.choice == "collection" || tc.choice == "user"
			wantMarketplace := tc.choice == "command" || tc.choice == "repo"
			hasMarketplace := false
			for _, entry := range marketplaceCfg.Marketplaces {
				hasMarketplace = hasMarketplace || slices.ContainsFunc(entry.Resources, func(pin config.MarketplaceResourcePin) bool {
					return pin.Kind == kind && pin.Name == "team"
				})
			}
			if (len(cfg.Registries[0].Commands.Include) != 0) != wantCommand ||
				hasMarketplace != wantMarketplace ||
				!slices.Equal(cfg.Registries[0].Skills.Include, []string{"keep"}) {
				t.Fatalf("wrong selection removed:\n%s", after)
			}
			if tc.choice == "repo" {
				marketplaceAfter, err := os.ReadFile(marketplacePath)
				if err != nil || !bytes.Equal(marketplaceAfter, marketplaceBefore) {
					t.Fatalf("repository removal changed user selections: %v\n%s", err, marketplaceAfter)
				}
			} else if tc.choice == "user" && !bytes.Equal(after, before) {
				t.Fatalf("user removal changed repository selections:\n%s", after)
			}
		})
	}
}

// spec: resource-removal/disambiguate-marketplace-pins-before-removal
func TestRemoveUndiscoverableMarketplaceSourceAmbiguity(t *testing.T) {
	isolateConsumerPipeline(t)
	repo, source := t.TempDir(), t.TempDir()
	t.Chdir(repo)
	writeSkill(t, source, "keep", "Keep selected", nil)
	path := config.ProjectConfigPath(repo)
	writeFile(t, path, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  local: {directory: %q, source_harness: factory}\nresources:\n  local: [skill/team, skill/keep]\n", source))
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "team")
	pins := layerPreparePins(t, fake, "skill", "team")
	if _, err := mutateRollbackMarketplace(path, config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins, nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opts := RemoveOptions{RepoRoot: repo, ResourceName: "team", Scope: ScopeRepo}
	if _, err := Remove(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("expected source ambiguity, got %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("refusal changed selections: %v\n%s", err, after)
	}
	opts.FromURL = source
	if _, err := Remove(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadRootConfigFromPath(path)
	if err != nil || len(cfg.Registries) != 1 || !slices.Equal(cfg.Registries[0].Skills.Include, []string{"keep"}) ||
		len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != 1 || cfg.Marketplaces[0].Resources[0].Name != "team" {
		t.Fatalf("wrong source removed: %#v / %v", cfg, err)
	}
}

// spec: resource-removal/undiscoverable-removal-respects-scope-and-required-policy
func TestRemoveUndiscoverableIgnoresExcludedCollection(t *testing.T) {
	isolateConsumerPipeline(t)
	repo, source := t.TempDir(), t.TempDir()
	t.Chdir(repo)
	writeSkill(t, source, "keep", "Keep selected", nil)
	path := config.ProjectConfigPath(repo)
	writeFile(t, path, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  local: {directory: %q, source_harness: factory}\nresources:\n  local: {include: [collection/team, skill/team, skill/keep], exclude: [collection/team]}\n", source))
	result, err := Remove(context.Background(), RemoveOptions{RepoRoot: repo, ResourceName: "team", Scope: ScopeRepo})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadRootConfigFromPath(path)
	if err != nil || len(cfg.Registries) != 1 {
		t.Fatalf("load removal result: %#v / %v", cfg, err)
	}
	entry := cfg.Registries[0]
	after, readErr := os.ReadFile(path)
	if result.IsCollection || !slices.Equal(entry.Skills.Include, []string{"keep"}) ||
		len(entry.Collections.Include) != 0 || !slices.Equal(entry.Collections.Exclude, []string{"team"}) ||
		readErr != nil || strings.Count(string(after), "collection/team") != 2 {
		t.Fatalf("excluded collection interfered with resource removal: %#v / %#v", entry, result)
	}
}
