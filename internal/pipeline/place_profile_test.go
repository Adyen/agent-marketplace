package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

func TestDeployedPlaceProfileReaderUsesExactHostedCache(t *testing.T) {
	fixture := newPlaceAdmissionFixture(t)
	identity := fixture.identity
	identity.SourceID = "git:https%3A%2F%2Fgit.example.com%2Ftools@main#profiles%2Finvented-app.yaml"
	if _, err := targetprofile.Parse(fixture.raw, identity); err != nil {
		t.Fatalf("parse hosted profile fixture: %v", err)
	}
	pin := config.MarketplaceResourcePin{
		RegistryID: "tools-id", RegistrySlug: "tools", Kind: "target-profile", Name: identity.TargetID,
		Revision: identity.RevisionDigest, Digest: identity.RevisionDigest,
		Acquisition: config.MarketplaceAcquisitionMirror, Direct: true, LockProfileIdentity: identity,
	}
	entry := config.MarketplaceEntry{
		URL: "https://market.example", InstanceID: "market-id", Source: config.Organization,
		Resources: []config.MarketplaceResourcePin{pin},
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}
	doc := lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.Scope(entry.Source.LockScope()),
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: entry.URL, InstanceID: entry.InstanceID,
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: pin.RegistrySlug, RegistryID: pin.RegistryID}},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName: pin.RegistrySlug, RegistryID: pin.RegistryID, Kind: pin.Kind, Name: pin.Name,
				Revision: pin.Revision, ContentDigest: lockfile.Digest(pin.Digest),
				ProfileIdentity: &identity, Acquisition: lockfile.MarketplaceAcquisitionMirror,
			}},
		}},
	}
	var evidence config.Evidence
	evidence.RecordDeployedLock(entry.Source, &doc, config.ConfigRead{})

	cacheRoot := t.TempDir()
	key := strings.Join([]string{
		entry.URL, entry.InstanceID, pin.RegistryID, identity.TargetID, identity.SourceID, identity.RevisionDigest,
	}, "\x00")
	sum := sha256.Sum256([]byte(key))
	cachePath := filepath.Join(cacheRoot, "target-profiles", hex.EncodeToString(sum[:])+".yaml")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, fixture.raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cache := marketplace.NewCache(cacheRoot)
	reader := deployedPlaceProfileReader(config.Intent{}, evidence, cfg, cache)
	for _, mode := range []resolver.MarketplaceResolveMode{
		resolver.MarketplaceResolveModeRefresh,
		resolver.MarketplaceResolveModeCachedNoFetch,
	} {
		got, err := reader(context.Background(), identity, mode)
		if err != nil || !bytes.Equal(got, fixture.raw) {
			t.Fatalf("hosted profile read mode %d = %q, %v", mode, got, err)
		}
	}

	wrongScopeDoc := doc
	wrongScopeDoc.Scope = lockfile.ScopeUser
	var wrongScopeEvidence config.Evidence
	wrongScopeEvidence.RecordDeployedLock(entry.Source, &wrongScopeDoc, config.ConfigRead{})
	if _, err := deployedPlaceProfileReader(config.Intent{}, wrongScopeEvidence, cfg, cache)(
		context.Background(), identity, resolver.MarketplaceResolveModeCachedNoFetch,
	); err == nil || !strings.Contains(err.Error(), "marketplace.profile.lock-missing") {
		t.Fatalf("scope-mismatched hosted lock read = %v", err)
	}

	excluded := *cfg
	excluded.MarketplaceExclusions = []config.MarketplaceExclusion{{
		URL: entry.URL, Locator: "tools/target-profile/" + identity.TargetID, Source: config.UserPersonal,
	}}
	if _, err := deployedPlaceProfileReader(config.Intent{}, evidence, &excluded, cache)(
		context.Background(), identity, resolver.MarketplaceResolveModeCachedNoFetch,
	); err == nil || !strings.Contains(err.Error(), "marketplace.profile.lock-missing") {
		t.Fatalf("excluded hosted profile read = %v", err)
	}
}
