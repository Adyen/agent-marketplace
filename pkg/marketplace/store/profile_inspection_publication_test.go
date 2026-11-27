package store_test

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	consumer "github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: marketplace-publication/sourceid-only-change-preserves-the-original-inspection-manifest
func TestProfileInspectionAddressIsImmutable(t *testing.T) {
	for _, mode := range []string{"legacy", "identified"} {
		t.Run(mode, func(t *testing.T) {
			forEachRepairStore(t, func(t *testing.T, meta store.MetadataStore, blobs store.BlobStore) {
				testProfileInspectionAddress(t, meta, blobs, mode == "identified")
			})
		})
	}
}

func testProfileInspectionAddress(t *testing.T, meta store.MetadataStore, blobs store.BlobStore, identified bool) {
	ctx := t.Context()
	coord := store.ResourceCoordinate{Marketplace: "market", Registry: "tools", Kind: "target-profile", Name: "invented-app"}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: coord.Marketplace, Name: "Market"}); err != nil {
		t.Fatal(err)
	}
	registry := store.Registry{ID: coord.Registry, Slug: "tools", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true,
		Origins: []string{"https://git.example.com/tools"}, Roots: []store.RegistryRoot{{Path: "profiles"}}}
	if err := meta.PutRegistry(ctx, registry); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Instance: config.Instance{ID: string(coord.Marketplace), CanonicalURL: "https://market.example.com"}}
	peer := httptest.NewServer(api.NewServer(cfg, meta, blobs).PublicHandler())
	defer peer.Close()

	profile := []byte(`apiVersion: agent-marketplace.io/target-profile/v1
id: invented-app
label: Invented App
platforms: [macos, linux]
roots: {user: {override: INVENTED_APP_HOME, default: {macos: {base: home, path: .config/invented-app}, linux: {base: home, path: .config/invented-app}}, allowed_bases: [home]}}
kinds: {mod: {deliver: place, into: [mods/]}}
`)
	const original = "profiles/invented-app.yaml"
	const moved = "profiles/invented-app-copy.yaml"
	build := func(path string, body []byte, commit, head string) store.RegistryRevisionPublication {
		t.Helper()
		identity := targetprofile.Identity{TargetID: coord.Name, SourceID: "git:https%3A%2F%2Fgit.example.com%2Ftools@main#" + url.QueryEscape(path), RevisionDigest: inspection.Digest(body)}
		p, err := inspection.BuildTargetProfile(coord, identity, body, 1, 1024, 4096)
		if err != nil {
			t.Fatal(err)
		}
		rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(identity.RevisionDigest)}
		payload, digest, err := artifact.CanonicalRegistryRevisionPayload(artifact.RegistryRevisionManifest{
			SchemaVersion: artifact.RegistryRevisionSchemaV1, Generator: artifact.RegistryRevisionGeneratorV1,
			Resources: []artifact.RevisionResource{{Kind: coord.Kind, Name: coord.Name, ContentDigest: identity.RevisionDigest}},
		})
		if err != nil {
			t.Fatal(err)
		}
		manifest := store.RevisionDigest(digest)
		var publicationID store.PublicationID
		if identified {
			publicationID = store.PublicationID(commit)
		}
		return store.RegistryRevisionPublication{
			PublicationID: publicationID, ExpectedHeadCommit: head,
			Occurrence:          store.RegistryOccurrence{RegistryID: coord.Registry, Ref: registry.Ref, Commit: commit, Acquisition: registry.Acquisition, RegistryRevisionManifestDigest: manifest},
			Resources:           []store.ResourceRevision{rev},
			ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: commit, ManifestDigest: manifest, SourcePath: path}},
			Assessments:         []store.ResourceAssessment{assessmentFor(t, rev, p.Manifest.ManifestDigest)},
			ManifestPayload:     payload,
			InspectionBlobs:     []store.BlobUpload{{Digest: store.ArchiveDigest(p.Manifest.ManifestDigest), Data: p.Payload}, {Digest: store.ArchiveDigest(identity.RevisionDigest), Data: body}},
		}
	}
	fetch := func(rev store.RevisionDigest, pin *targetprofile.Identity) marketplace.TargetProfileEvidence {
		t.Helper()
		client, err := marketplace.NewClient(peer.URL)
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := client.AcquireTargetProfile(ctx, coord, rev, pin)
		if err != nil {
			t.Fatal(err)
		}
		return evidence
	}
	first := build(original, profile, strings.Repeat("a", 40), "")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, first); err != nil {
		t.Fatal(err)
	}
	resolver := &marketplace.Resolver{Cache: marketplace.NewCache(t.TempDir())}
	entry := consumer.MarketplaceEntry{URL: peer.URL, Source: consumer.UserPersonal}
	locator := marketplace.MarketplaceLocator{Registry: registry.Slug, Kind: coord.Kind, Name: coord.Name, Revision: string(first.Resources[0].ContentDigest)}
	_, lock, err := resolver.PrepareTargetProfileLock(ctx, entry, locator, nil)
	if err != nil || lock.ProfileIdentity == nil {
		t.Fatalf("prepare original profile lock: %+v, %v", lock, err)
	}
	attempt := build(moved, profile, strings.Repeat("b", 40), first.Occurrence.Commit)
	if err := store.PublishRegistryRevision(ctx, meta, blobs, attempt); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("SourceID-only publication = %v; want conflict", err)
	}
	head, err := meta.LatestRegistryOccurrence(ctx, coord.Registry)
	if err != nil || head.Commit != first.Occurrence.Commit {
		t.Fatalf("head after refusal = %s, %v", head.Commit, err)
	}
	if _, err := meta.GetResourceAssessment(ctx, attempt.Assessments[0].AssessmentDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rejected assessment lookup = %v; want not found", err)
	}
	if _, err := blobs.Get(ctx, store.ArchiveDigest(attempt.Assessments[0].InspectionManifestDigest)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refused manifest available at its public blob digest: %v", err)
	}
	assertOriginal := func() {
		t.Helper()
		after := fetch(store.RevisionDigest(lock.Revision), lock.ProfileIdentity)
		if !bytes.Equal(after.Manifest, first.InspectionBlobs[0].Data) || !bytes.Equal(after.Bytes, profile) {
			t.Fatal("fresh client did not receive the original manifest and YAML")
		}
	}
	assertOriginal()
	same := build(original, profile, strings.Repeat("c", 40), first.Occurrence.Commit)
	same.Assessments[0].AnalyzerVersion = "test/v2"
	same.Assessments[0].AssessmentDigest = ""
	same.Assessments[0], err = store.FinalizeResourceAssessment(same.Assessments[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishRegistryRevision(ctx, meta, blobs, same); err != nil {
		t.Fatalf("reassessment of unchanged profile: %v", err)
	}
	newProfile := append(bytes.Clone(profile), '\n')
	changed := build(moved, newProfile, strings.Repeat("d", 40), same.Occurrence.Commit)
	if err := store.PublishRegistryRevision(ctx, meta, blobs, changed); err != nil {
		t.Fatalf("new profile revision: %v", err)
	}
	if got := fetch(changed.Resources[0].ContentDigest, nil); !bytes.Equal(got.Bytes, newProfile) || got.Identity.SourceID == lock.ProfileIdentity.SourceID {
		t.Fatal("new revision did not serve the changed profile at the new source")
	}
	assertOriginal()
}
