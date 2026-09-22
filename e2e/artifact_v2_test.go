package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestArtifactV2LockUpgradeAndOfflineSync(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("VERIFIED ARTIFACT V2 BODY", false))
	digest := fixture.resourceDigest(t, "skill", "skill-one")
	rev, err := fixture.meta.GetResourceRevision(t.Context(), store.ResourceCoordinate{Marketplace: "behavior-marketplace", Registry: "behavior-source-id", Kind: "skill", Name: "skill-one"}, store.RevisionDigest(digest))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"locked", "check", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			consumer := fixture.newConsumer(t, fixture.server.URL)
			body := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  market:\n    marketplace: %s\nresources:\n  market:\n    include: [behavior-source/skill/skill-one]\n", fixture.server.URL)
			consumer.WriteRepoConfig(body)
			doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{URL: fixture.server.URL, InstanceID: "behavior-marketplace", RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "behavior-source", RegistryID: "behavior-source-id"}}, Resources: []lockfile.MarketplaceResourceLock{{RegistryName: "behavior-source", RegistryID: "behavior-source-id", Kind: "skill", Name: "skill-one", Revision: digest, ContentDigest: lockfile.Digest(digest), ArchiveDigest: lockfile.Digest(rev.ArchiveDigest), Acquisition: lockfile.MarketplaceAcquisitionMirror}}}}}
			before, err := lockfile.Encode(doc)
			if err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(consumer.workspace, ".agents", "lock.yaml")
			if err := os.WriteFile(lockPath, before, 0o644); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "locked":
				consumer.MustRunAM("sync", "--locked")
			case "check":
				consumer.RunAM("sync", "--check")
			case "upgrade":
				consumer.MustRunAM("sync")
			}
			after, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "upgrade" {
				if !bytes.Equal(before, after) {
					t.Fatal("strict sync changed exact lock")
				}
				return
			}
			upgraded, err := lockfile.Decode(after)
			if err != nil {
				t.Fatal(err)
			}
			if upgraded.APIVersion != lockfile.APIVersionV3 || upgraded.Marketplaces[0].Resources[0].Artifact.Schema != "v2" {
				t.Fatalf("missing v2 lock: %s", after)
			}
			consumer.MustRunAM("sync", "--offline", "--locked")
			installed := filepath.Join(consumer.workspace, ".factory", "skills", "skill-one", "SKILL.md")
			raw, err := os.ReadFile(installed)
			if err != nil || !strings.Contains(string(raw), "VERIFIED ARTIFACT V2 BODY") {
				t.Fatalf("materialized payload missing: %s %v", raw, err)
			}
		})
	}
}
