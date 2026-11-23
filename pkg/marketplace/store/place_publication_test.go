package store_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestPlacePublicationPreservesPriorBinding(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)
	marketplace := store.Marketplace{
		ID:           "place-marketplace",
		Name:         "Place Marketplace",
		CanonicalURL: "https://place.test",
	}
	if err := meta.PutMarketplace(ctx, marketplace); err != nil {
		t.Fatalf("PutMarketplace: %v", err)
	}
	registry := store.Registry{
		ID:          "place-registry",
		Slug:        "place",
		Ref:         "main",
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
	}
	if err := meta.PutRegistry(ctx, registry); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}

	coordinate := store.ResourceCoordinate{
		Marketplace: marketplace.ID,
		Registry:    registry.ID,
		Kind:        "herdr-app/mod",
		Name:        "sample",
	}
	profile := targetprofile.Identity{
		TargetID:       "herdr-app",
		SourceID:       "builtin:agent-marketplace",
		RevisionDigest: sha256Text("e"),
	}
	primary := []byte(`{"name":"sample"}`)
	buildPublication := func(destination, commit, expectedHead string, assessmentOverride *store.ResourceAssessment) (store.RegistryRevisionPublication, hccartifact.CanonicalArtifact, []byte, store.ResourceRevision, store.ResourceAssessment) {
		t.Helper()
		artifact, err := hccartifact.NewPlaceArtifact(coordinate.Kind, coordinate.Name, profile, []targetprofile.PackageFile{{
			SourcePath: "src.bin", DestinationPath: destination, Mode: 0o644, Bytes: []byte{0, 1},
		}})
		if err != nil {
			t.Fatalf("NewPlaceArtifact: %v", err)
		}
		artifact.Manifest.RegistryID = string(coordinate.Registry)
		final, placeholder, archiveDigest, err := hccartifact.EncodeArchives(artifact)
		if err != nil {
			t.Fatalf("EncodeArchives: %v", err)
		}
		revision := store.ResourceRevision{
			Coordinate:    coordinate,
			ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}
		manifestPayload, manifestDigest, err := marketplaceartifact.CanonicalRegistryRevisionPayload(marketplaceartifact.RegistryRevisionManifest{
			SchemaVersion: marketplaceartifact.RegistryRevisionSchemaV1,
			Generator:     marketplaceartifact.RegistryRevisionGeneratorV1,
			Resources: []marketplaceartifact.RevisionResource{{
				Kind: coordinate.Kind, Name: coordinate.Name,
				ContentDigest: string(revision.ContentDigest), ArchiveDigest: string(revision.ArchiveDigest),
			}},
		})
		if err != nil {
			t.Fatalf("CanonicalRegistryRevisionPayload: %v", err)
		}
		projection, err := inspection.Build(coordinate, string(revision.ContentDigest), primary, nil, 10, 1024, 4096)
		if err != nil {
			t.Fatalf("inspection.Build: %v", err)
		}
		assessment := validResourceAssessment()
		assessment.Coordinate = coordinate
		assessment.ContentDigest = revision.ContentDigest
		assessment.HostPackageIdentity = artifact.Manifest.HostPackageIdentity
		assessment.InspectionManifestDigest = store.RevisionDigest(projection.Manifest.ManifestDigest)
		assessment, err = store.FinalizeResourceAssessment(assessment)
		if err != nil {
			t.Fatalf("FinalizeResourceAssessment: %v", err)
		}
		if assessmentOverride != nil {
			assessment = *assessmentOverride
		}
		registryManifestDigest := store.RevisionDigest(manifestDigest)
		request := store.RegistryRevisionPublication{
			Occurrence: store.RegistryOccurrence{
				RegistryID: registry.ID, Ref: "main", Commit: commit,
				RegistryRevisionManifestDigest: registryManifestDigest,
				Acquisition:                    store.AcquisitionMirror,
				MirroredArtifactDigest:         revision.ArchiveDigest,
			},
			ExpectedHeadCommit: expectedHead,
			Resources:          []store.ResourceRevision{revision},
			ResourceOccurrences: []store.ResourceOccurrence{{
				Coordinate: coordinate, ContentDigest: revision.ContentDigest,
				Commit: commit, ManifestDigest: registryManifestDigest,
			}},
			Assessments:     []store.ResourceAssessment{assessment},
			ManifestPayload: manifestPayload,
			ExtraBlobs:      []store.BlobUpload{{Digest: revision.ArchiveDigest, Data: final}},
			InspectionBlobs: []store.BlobUpload{
				{Digest: store.PlaceholderArchiveKey(revision.ArchiveDigest), Data: placeholder},
				{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload},
				{Digest: store.ArchiveDigest(inspection.Digest(primary)), Data: primary},
			},
		}
		return request, artifact, final, revision, assessment
	}

	first, firstArtifact, firstArchive, firstRevision, firstAssessment := buildPublication("mods/one.bin", "place-first-commit", "", nil)
	if err := store.PublishRegistryRevision(ctx, meta, blobs, first); err != nil {
		t.Fatalf("first PublishRegistryRevision: %v", err)
	}
	before, err := meta.GetLatestResourceRevision(ctx, coordinate)
	if err != nil {
		t.Fatalf("GetLatestResourceRevision after first publication: %v", err)
	}
	if before.ArchiveDigest != firstRevision.ArchiveDigest || before.Assessment.AssessmentDigest != firstAssessment.AssessmentDigest || before.Occurrence.Commit != first.Occurrence.Commit {
		t.Fatalf("first publication metadata does not match request: %#v", before)
	}
	if before.Assessment.HostPackageIdentity == nil || *before.Assessment.HostPackageIdentity != *firstArtifact.Manifest.HostPackageIdentity {
		t.Fatalf("first publication binding = %#v, want %#v", before.Assessment.HostPackageIdentity, firstArtifact.Manifest.HostPackageIdentity)
	}

	second, _, _, secondRevision, _ := buildPublication("mods/two.bin", "place-second-commit", first.Occurrence.Commit, &firstAssessment)
	if secondRevision.ContentDigest != firstRevision.ContentDigest || secondRevision.ArchiveDigest == firstRevision.ArchiveDigest {
		t.Fatalf("second revision digests = %q/%q, want same content and different archive", secondRevision.ContentDigest, secondRevision.ArchiveDigest)
	}
	if err := store.PublishRegistryRevision(ctx, meta, blobs, second); err == nil {
		t.Fatal("publication with a different package mapping and prior assessment succeeded")
	}
	if _, err := blobs.Get(ctx, secondRevision.ArchiveDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("new final archive blob lookup error = %v, want ErrNotFound", err)
	}
	after, err := meta.GetLatestResourceRevision(ctx, coordinate)
	if err != nil {
		t.Fatalf("GetLatestResourceRevision after rejected publication: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected publication changed visible revision:\nbefore: %#v\nafter:  %#v", before, after)
	}
	priorArchive, err := blobs.Get(ctx, firstRevision.ArchiveDigest)
	if err != nil {
		t.Fatalf("get prior archive: %v", err)
	}
	if !bytes.Equal(priorArchive, firstArchive) {
		t.Fatal("prior archive bytes changed after rejected publication")
	}
}
