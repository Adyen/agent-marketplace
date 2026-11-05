package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: hook-resources/handwritten-matches-and-unavailable-proof-cannot-authorize-removal
// spec: hook-resources/remaining-contributors-and-owned-removal-stay-supported
func TestRemoveOrganizationMarketplaceHookOwnership(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprintf("owned=%t", owned), func(t *testing.T) {
			f := newOrganizationMarketplaceRemovalFixture(t, false, false)
			registry := f.fake.registries["source"]
			const payload = "echo organization-hook"
			hook := ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", payload, "command", nil, ir.SourceRef{})
			artifact, err := hccartifact.FromIR(hook)
			if err != nil {
				t.Fatal(err)
			}
			artifact.Manifest.RegistryID = string(registry.ID)
			artifact.Manifest.RegistrySlug = registry.Slug
			artifact.Manifest.Revision = artifact.Manifest.ContentDigest
			raw, archiveDigest, err := hccartifact.Encode(artifact)
			if err != nil {
				t.Fatal(err)
			}
			f.fake.resources["source/hook/guard"] = layerMarketplaceResource{
				kind: "hook", name: "guard", raw: raw, archiveDigest: archiveDigest,
				revision: layerAssessedRevision(store.ResourceRevision{
					Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(f.fake.instanceID), Registry: registry.ID, Kind: "hook", Name: "guard"},
					ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
				}),
			}
			f.fake.addCollection(t, registry, "hook-bundle", []hccartifact.CollectionMemberEntry{
				{RegistryID: string(registry.ID), Kind: "hook", Name: "guard", Revision: artifact.Manifest.ContentDigest},
				{RegistryID: string(registry.ID), Kind: "skill", Name: "review", Revision: string(f.fake.resources["source/skill/review"].revision.ContentDigest)},
			})
			writeFile(t, f.orgPath, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  org: {marketplace: %s}\nresources:\n  org: [source/collection/hook-bundle]\n", f.fake.URL))
			synced := Sync(context.Background(), ResolveOptions{Workspace: mustWorkspace(t, f.repo), NoBackup: true})
			if len(synced.Errors) != 0 {
				t.Fatal(synced.Errors)
			}
			nativePath := filepath.Join(f.home, ".claude", "settings.json")
			nativeBefore, err := os.ReadFile(nativePath)
			if err != nil || !strings.Contains(string(nativeBefore), payload) {
				t.Fatalf("hook missing before removal: %s / %v", nativeBefore, err)
			}
			userBefore, err := os.ReadFile(f.userPath)
			if err != nil {
				t.Fatal(err)
			}
			if !owned {
				t.Setenv("XDG_STATE_HOME", filepath.Join(f.home, "missing-ownership"))
			}
			_, err = Remove(context.Background(), RemoveOptions{
				RepoRoot: f.repo, ResourceName: "guard", Kind: ir.KindHook, HasKind: true,
				Scope: ScopeUser, ScopeSpecified: true,
			})
			nativeAfter, readErr := os.ReadFile(nativePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			userAfter, readErr := os.ReadFile(f.userPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if owned {
				if err != nil || strings.Contains(string(nativeAfter), payload) || !strings.Contains(string(userAfter), "source/hook/guard") {
					t.Fatalf("owned removal did not converge: %v\n%s\n%s", err, nativeAfter, userAfter)
				}
				if _, err := os.Stat(filepath.Join(f.home, ".claude", "skills", "review", "SKILL.md")); err != nil {
					t.Fatalf("sibling removed: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "hook.remove-unowned") || string(nativeAfter) != string(nativeBefore) || string(userAfter) != string(userBefore) {
				t.Fatalf("unowned hook refusal changed state: %v\n%s\n%s", err, nativeAfter, userAfter)
			}
			assertNoStagingResidue(t)
		})
	}
}
