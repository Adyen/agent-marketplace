package marketplacelock

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestReplacePinsToV4PreservesAuthoredRegistryAlias(t *testing.T) {
	for _, oldSlug := range []string{"old-source", "previous-source"} {
		t.Run(oldSlug, func(t *testing.T) {
			cache := marketplace.NewCache(t.TempDir())
			entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Source: config.RepoCommitted}
			old, oldRecord := cacheRenameBindingPin(t, cache, entry, "src", oldSlug, "review", "before")
			next, nextRecord := cacheRenameBindingPin(t, cache, entry, "src", "renamed-source", "review", "after")
			_, stale := cacheRenameBindingPin(t, cache, entry, "src", "unused-alias", "stale", "remove")
			_, foreign := cacheRenameBindingPin(t, cache, entry, "foreign-id", "unused-foreign", "review", "foreign")
			authored := []byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: https://market.example}
resources:
  hosted:
    - old-source/skill/review: {as: chosen-review}
`)
			v4, err := config.ParseV4(authored)
			if err != nil {
				t.Fatal(err)
			}
			current := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{
				URL: entry.URL, InstanceID: entry.InstanceID,
				RegistryBindings: []lockfile.MarketplaceRegistryBinding{
					{Name: "unused-foreign", RegistryID: "foreign-id"},
					{Name: "unused-alias", RegistryID: "src"},
					{Name: "old-source", RegistryID: "src"},
					{Name: oldSlug, RegistryID: "src"},
				},
				Resources: []lockfile.MarketplaceResourceLock{stale, foreign, oldRecord},
			}}}
			// Normalize only input, never the independently authored output oracle.
			current, err = lockfile.Normalize(current)
			if err != nil {
				t.Fatal(err)
			}
			before, err := lockfile.Encode(current)
			if err != nil {
				t.Fatal(err)
			}
			requestsBefore := append([]config.V4ResourceRequest(nil), v4.Requests...)
			got, err := ReplacePinsToV4(v4, current, entry, old, []config.MarketplaceResourcePin{next, next}, cache)
			if err != nil {
				t.Fatal(err)
			}
			wantBindings := []lockfile.MarketplaceRegistryBinding{{Name: "old-source", RegistryID: "src"}, {Name: "renamed-source", RegistryID: "src"}}
			want := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{
				URL: entry.URL, InstanceID: entry.InstanceID, RegistryBindings: wantBindings,
				Resources: []lockfile.MarketplaceResourceLock{nextRecord},
			}}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("updated lock = %#v, want %#v", got, want)
			}
			wantBytes := fmt.Sprintf(`apiVersion: agent-marketplace-lock/v2
scope: repo
marketplaces:
  - url: https://market.example
    instance_id: mp
    registry_bindings:
      - name: old-source
        registry_id: src
      - name: renamed-source
        registry_id: src
    resources:
      - registry_name: renamed-source
        registry_id: src
        kind: skill
        name: review
        revision: %s
        content_digest: %s
        archive_digest: %s
        acquisition: mirror
`, next.Digest, next.Digest, nextRecord.ArchiveDigest)
			if encoded, err := lockfile.Encode(got); err != nil || string(encoded) != wantBytes {
				t.Fatalf("updated lock bytes = %s, %v; want %s", encoded, err, wantBytes)
			}
			if after, err := lockfile.Encode(current); err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(v4.Requests, requestsBefore) {
				t.Fatalf("replacement mutated its inputs: %v", err)
			}
			loaded := loadRenameBindingConfig(t, authored, got)
			if len(loaded.Marketplaces) != 1 || len(loaded.Marketplaces[0].Resources) != 1 {
				t.Fatalf("strict runtime projection = %#v", loaded.Marketplaces)
			}
			pin := loaded.Marketplaces[0].Resources[0]
			if pin.Name != "review" || !pin.Direct || pin.RegistryID != "src" || pin.RegistrySlug != "renamed-source" || pin.Digest != next.Digest || pin.InstalledName != "chosen-review" {
				t.Fatalf("updated runtime pin = %#v", pin)
			}
			// Rebuilding the same endpoint must retain the alias and the updated
			// revision rather than resurrecting the previous pin or stale records.
			refreshed, err := MergePinsToV4(v4, got, entry, nil, nil)
			if err != nil || !reflect.DeepEqual(refreshed, want) {
				t.Fatalf("endpoint refresh = %#v, %v; want %#v", refreshed, err, want)
			}
		})
	}
}

func TestReplacePinsToV4RejectsRegistryNamespaceConflicts(t *testing.T) {
	for _, tc := range []struct {
		name, replacementID, replacementSlug, bindingID string
	}{
		{name: "replacement reuses authored slug for another ID", replacementID: "foreign-id", replacementSlug: "old-source", bindingID: "src"},
		{name: "authored binding disagrees with owning record", replacementID: "src", replacementSlug: "renamed-source", bindingID: "foreign-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := marketplace.NewCache(t.TempDir())
			entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Source: config.RepoCommitted}
			old, oldRecord := cacheRenameBindingPin(t, cache, entry, "src", "old-source", "review", "before")
			next, _ := cacheRenameBindingPin(t, cache, entry, tc.replacementID, tc.replacementSlug, "review", "after")
			v4, err := config.ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: https://market.example}\nresources:\n  hosted: {include: [old-source/skill/review]}\n"))
			if err != nil {
				t.Fatal(err)
			}
			current := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{
				URL: entry.URL, InstanceID: entry.InstanceID,
				RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "old-source", RegistryID: tc.bindingID}},
				Resources:        []lockfile.MarketplaceResourceLock{oldRecord},
			}}}
			_, err = ReplacePinsToV4(v4, current, entry, old, []config.MarketplaceResourcePin{next}, cache)
			var mismatch *config.V4ValidationError
			if !errors.As(err, &mismatch) || mismatch.Code != "v4.marketplace-lock-mismatch" {
				t.Fatalf("conflicting registry namespace accepted: %v", err)
			}
		})
	}
}

func TestReplacePinsToV4RejectsExplicitRevisionThroughAlias(t *testing.T) {
	cache := marketplace.NewCache(t.TempDir())
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Source: config.RepoCommitted}
	old, oldRecord := cacheRenameBindingPin(t, cache, entry, "src", "previous-source", "review", "before")
	next, _ := cacheRenameBindingPin(t, cache, entry, "src", "renamed-source", "review", "after")
	v4, err := config.ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: https://market.example}\nresources:\n  hosted:\n    - old-source/skill/review: {revision: " + old.Revision + "}\n"))
	if err != nil {
		t.Fatal(err)
	}
	current := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{
		URL: entry.URL, InstanceID: entry.InstanceID,
		RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "old-source", RegistryID: "src"}, {Name: "previous-source", RegistryID: "src"}},
		Resources:        []lockfile.MarketplaceResourceLock{oldRecord},
	}}}
	_, err = ReplacePinsToV4(v4, current, entry, old, []config.MarketplaceResourcePin{next}, cache)
	var mismatch *config.V4ValidationError
	if !errors.As(err, &mismatch) || mismatch.Code != "v4.marketplace-lock-mismatch" {
		t.Fatalf("explicit revision through registry alias updated: %v", err)
	}
}

func TestMergePinsToV4PreservesAliasesOnlyWithinOwningEndpoint(t *testing.T) {
	cache := marketplace.NewCache(t.TempDir())
	primary := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Source: config.RepoCommitted}
	secondary := config.MarketplaceEntry{URL: "https://other.example", InstanceID: "other-mp", Source: config.RepoCommitted}
	_, first := cacheRenameBindingPin(t, cache, primary, "src", "renamed-source", "review", "first")
	_, second := cacheRenameBindingPin(t, cache, secondary, "other-id", "other-current", "review", "second")
	authored := []byte(`apiVersion: agent-marketplace/v4
sources:
  primary: {marketplace: https://market.example}
  secondary: {marketplace: https://other.example}
resources:
  primary: {include: [old-source/skill/review]}
  secondary: {include: [old-source/skill/review]}
`)
	v4, err := config.ParseV4(authored)
	if err != nil {
		t.Fatal(err)
	}
	current := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{
		{URL: primary.URL, InstanceID: primary.InstanceID, RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "old-source", RegistryID: "src"}, {Name: "renamed-source", RegistryID: "src"}}, Resources: []lockfile.MarketplaceResourceLock{first}},
		{URL: secondary.URL, InstanceID: secondary.InstanceID, RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "old-source", RegistryID: "other-id"}, {Name: "other-current", RegistryID: "other-id"}, {Name: "unused", RegistryID: "src"}}, Resources: []lockfile.MarketplaceResourceLock{second}},
	}}
	got, err := MergePinsToV4(v4, current, primary, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{
		{URL: primary.URL, InstanceID: primary.InstanceID, RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "old-source", RegistryID: "src"}, {Name: "renamed-source", RegistryID: "src"}}, Resources: []lockfile.MarketplaceResourceLock{first}},
		{URL: secondary.URL, InstanceID: secondary.InstanceID, RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "old-source", RegistryID: "other-id"}, {Name: "other-current", RegistryID: "other-id"}}, Resources: []lockfile.MarketplaceResourceLock{second}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("refreshed endpoint aliases = %#v, want %#v", got, want)
	}
	loadRenameBindingConfig(t, authored, got)
}

func cacheRenameBindingPin(t *testing.T, cache *marketplace.Cache, entry config.MarketplaceEntry, registryID, slug, name, body string) (config.MarketplaceResourcePin, lockfile.MarketplaceResourceLock) {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(name, "Review", body, nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = registryID
	canonical.Manifest.RegistrySlug = slug
	digest := canonical.Manifest.ContentDigest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	coordinate := store.ResourceCoordinate{Marketplace: store.MarketplaceID(entry.InstanceID), Registry: store.RegistryID(registryID), Kind: "skill", Name: name}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(digest), AnalyzerVersion: "test-analyzer",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)), AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	revision := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: store.RevisionDigest(digest), ArchiveDigest: store.ArchiveDigest(archiveDigest)},
		Assessment:       assessment,
		Occurrence:       store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: store.RevisionDigest(digest), Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64))},
	}
	if _, err := cache.WriteResourceRevision(entry.URL, registryID, "skill", name, digest, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	pin := config.MarketplaceResourcePin{RegistryID: registryID, RegistrySlug: slug, Kind: "skill", Name: name, Revision: digest, Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror}
	// Do not derive expected lock records with VerifiedMarketplaceLockResource.
	record := lockfile.MarketplaceResourceLock{RegistryName: slug, RegistryID: registryID, Kind: "skill", Name: name, Revision: digest, ContentDigest: lockfile.Digest(digest), ArchiveDigest: lockfile.Digest(archiveDigest), Acquisition: lockfile.MarketplaceAcquisitionMirror}
	return pin, record
}

func loadRenameBindingConfig(t *testing.T, authored []byte, doc lockfile.Document) *config.Config {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, authored, 0o600); err != nil {
		t.Fatal(err)
	}
	encoded, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lock.yaml"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(fmt.Errorf("strictly load authored requests with merged lock: %w", err))
	}
	return loaded
}
