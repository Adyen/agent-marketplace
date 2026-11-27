package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
	"gopkg.in/yaml.v3"
)

func TestIndexedPlaceRequires(t *testing.T) {
	nodes := []yaml.Node{
		placeRequirementNode(t, "invented-app/mod:other"),
		placeRequirementNode(t, "{kind: invented-app/mod, name: second}"),
	}
	got, err := indexedPlaceRequires("invented-app/mod", "sample", nodes)
	want := `[{"kind":"invented-app/mod","name":"other"},{"kind":"invented-app/mod","name":"second"}]`
	if err != nil || got != want {
		t.Fatalf("indexedPlaceRequires = %q, %v; want %q", got, err, want)
	}
	empty, err := indexedPlaceRequires("invented-app/mod", "sample", nil)
	if err != nil || empty != "[]" {
		t.Fatalf("empty indexedPlaceRequires = %q, %v; want []", empty, err)
	}

	for _, test := range []struct {
		name string
		node string
		pkg  string
	}{
		{name: "unsupported scalar", node: "skill:other", pkg: "sample"},
		{name: "cross-target", node: "other-app/mod:other", pkg: "sample"},
		{name: "registry hint", node: "{kind: invented-app/mod, name: other, registry: tools}", pkg: "sample"},
		{name: "path hint", node: "{kind: invented-app/mod, name: other, path: nested/mod}", pkg: "sample"},
		{name: "non-string scalar", node: "{kind: 42, name: other}", pkg: "sample"},
		{name: "duplicate key", node: "{kind: invented-app/mod, kind: invented-app/layout, name: other}", pkg: "sample"},
		{name: "extra key", node: "{kind: invented-app/mod, name: other, alias: mod}", pkg: "sample"},
		{name: "self edge", node: "invented-app/mod:sample", pkg: "sample"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := indexedPlaceRequires(
				"invented-app/mod", test.pkg, []yaml.Node{placeRequirementNode(t, test.node)},
			)
			if err == nil || !strings.Contains(err.Error(), "marketplace.package.prerequisites-invalid") {
				t.Fatalf("indexedPlaceRequires(%q) error = %v", test.node, err)
			}
		})
	}
}

// spec: marketplace-publication/unbound-or-inconsistent-place-publication-preserves-the-prior-publication
func TestBuildRegistryRevisionPublishesExactPlaceAssessment(t *testing.T) {
	ctx := context.Background()
	profileBytes := []byte(checkoutProfileYAML)
	profileIdentity := checkoutAssessmentProfileIdentity(profileBytes)
	_, root := openAssessmentProfileCheckout(t, profileBytes)

	packageRoot := filepath.Join(root, "packages", "invented-app", "mod", "sample")
	writeAssessmentProfileFile(t, filepath.Join(packageRoot, "agent-marketplace.yaml"), []byte(`target_id: invented-app
kind: invented-app/mod
name: sample
files:
  - source_path: source.txt
    destination_path: mods/sample.txt
metadata:
  requires:
    - invented-app/mod:other
    - {kind: invented-app/mod, name: second}
`))
	payloadPath := filepath.Join(packageRoot, "payload", "source.txt")
	payload := []byte("plain text package\n")
	writeAssessmentProfileFile(t, payloadPath, payload)
	if err := os.Chmod(payloadPath, 0o644); err != nil {
		t.Fatal(err)
	}

	otherPackageRoot := filepath.Join(root, "packages", "invented-app", "mod", "other")
	writeAssessmentProfileFile(t, filepath.Join(otherPackageRoot, "agent-marketplace.yaml"), []byte(`target_id: invented-app
kind: invented-app/mod
name: other
files:
  - source_path: other.txt
    destination_path: mods/other.txt
`))
	otherPayloadPath := filepath.Join(otherPackageRoot, "payload", "other.txt")
	writeAssessmentProfileFile(t, otherPayloadPath, []byte("other package\n"))
	if err := os.Chmod(otherPayloadPath, 0o644); err != nil {
		t.Fatal(err)
	}

	registry := store.Registry{
		ID: "src-test", Slug: "src-test", Ref: "main",
		Origins: []string{checkoutProfileOrigin}, Acquisition: store.AcquisitionMirror, License: "MIT",
	}
	cfg := testConfig(t, root)
	cfg.HostPackageAssessmentProfiles = []config.HostPackageAssessmentProfile{{
		RegistryID: "src-test", Kind: "invented-app/mod", Name: "sample", ProfileIdentity: profileIdentity,
	}, {
		RegistryID: "src-test", Kind: "invented-app/mod", Name: "other", ProfileIdentity: profileIdentity,
	}}
	const marketplaceID store.MarketplaceID = "mp-test"
	commit := strings.Repeat("e", 40)

	revision, err := BuildRegistryRevision(ctx, cfg, marketplaceID, registry, root, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
	}
	coord := store.ResourceCoordinate{Marketplace: marketplaceID, Registry: registry.ID, Kind: "invented-app/mod", Name: "sample"}
	resourceIndex := -1
	for i, resource := range revision.Resources {
		if resource.Coordinate == coord {
			resourceIndex = i
			break
		}
	}
	if resourceIndex < 0 {
		t.Fatalf("BuildRegistryRevision resources = %#v, want scoped %s", revision.Resources, coord)
	}
	if resourceIndex >= len(revision.Assessments) {
		t.Fatalf("BuildRegistryRevision assessments = %d, missing resource index %d", len(revision.Assessments), resourceIndex)
	}
	if resourceIndex >= len(revision.ResourceOccurrences) {
		t.Fatalf("BuildRegistryRevision resource occurrences = %d, missing resource index %d", len(revision.ResourceOccurrences), resourceIndex)
	}
	resource := revision.Resources[resourceIndex]
	assessment := revision.Assessments[resourceIndex]
	wantRequires := `[{"kind":"invented-app/mod","name":"other"},{"kind":"invented-app/mod","name":"second"}]`
	if resource.Requires != wantRequires {
		t.Fatalf("resource prerequisites = %q, want %q", resource.Requires, wantRequires)
	}
	if revision.ResourceOccurrences[resourceIndex].SourcePath != "packages/invented-app/mod/sample" {
		t.Fatalf("resource source path = %q, want packages/invented-app/mod/sample", revision.ResourceOccurrences[resourceIndex].SourcePath)
	}
	if assessment.HostPackageIdentity == nil {
		t.Fatal("scoped assessment has no host package identity")
	}

	var finalArchive, placeholderArchive []byte
	for _, upload := range revision.ExtraBlobs {
		switch upload.Digest {
		case store.ArchiveDigest(resource.ArchiveDigest):
			finalArchive = upload.Data
		case store.PlaceholderArchiveKey(store.ArchiveDigest(resource.ArchiveDigest)):
			placeholderArchive = upload.Data
		}
	}
	placeholder, err := hccartifact.VerifyPlaceholderArchive(placeholderArchive, string(resource.ArchiveDigest))
	if err != nil {
		t.Fatalf("verify placeholder archive: %v", err)
	}
	verified, err := hccartifact.VerifyFinalAgainstPlaceholder(finalArchive, placeholder)
	if err != nil {
		t.Fatalf("verify final archive: %v", err)
	}
	if verified.Manifest.HostPackageIdentity == nil || *verified.Manifest.HostPackageIdentity != *assessment.HostPackageIdentity {
		t.Fatal("assessment binding differs from verified artifact binding")
	}
	if verified.Manifest.HostPackageIdentity.ProfileIdentity != profileIdentity {
		t.Fatalf("artifact profile identity = %+v, want %+v", verified.Manifest.HostPackageIdentity.ProfileIdentity, profileIdentity)
	}
	files, err := verified.PlaceFiles()
	if err != nil {
		t.Fatalf("read verified place files: %v", err)
	}
	if len(files) != 1 || files[0].SourcePath != "source.txt" || files[0].DestinationPath != "mods/sample.txt" ||
		files[0].Mode != 0o644 || !bytes.Equal(files[0].Bytes, payload) {
		t.Fatalf("verified place files = %+v, want exact source, mapping, mode, and bytes", files)
	}
	for _, upload := range revision.InspectionBlobs {
		if bytes.Contains(upload.Data, payload) {
			t.Fatal("inspection blobs contain package payload bytes")
		}
	}

	meta := sqlite.New(filepath.Join(t.TempDir(), "marketplace.sqlite"))
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := meta.Close(); err != nil {
			t.Errorf("close metadata store: %v", err)
		}
	})
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: marketplaceID, Name: "Test", CanonicalURL: "https://example.invalid"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistry(ctx, registry); err != nil {
		t.Fatal(err)
	}
	blobs := fsblob.New(t.TempDir())
	if err := store.PublishRegistryRevision(ctx, meta, blobs, store.RegistryRevisionPublication{
		Occurrence: revision.Occurrence, Resources: revision.Resources,
		ResourceOccurrences: revision.ResourceOccurrences, Assessments: revision.Assessments,
		ManifestPayload: revision.ManifestPayload, ExtraBlobs: revision.ExtraBlobs,
		InspectionBlobs: revision.InspectionBlobs,
	}); err != nil {
		t.Fatalf("PublishRegistryRevision: %v", err)
	}
	persisted, err := meta.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatalf("GetLatestResourceRevision: %v", err)
	}
	if persisted.Requires != wantRequires {
		t.Fatalf("persisted prerequisites = %q, want %q", persisted.Requires, wantRequires)
	}
	if persisted.Assessment.HostPackageIdentity == nil || *persisted.Assessment.HostPackageIdentity != *verified.Manifest.HostPackageIdentity {
		t.Fatal("persisted assessment binding differs from verified artifact binding")
	}
	profileCoord := store.ResourceCoordinate{Marketplace: marketplaceID, Registry: registry.ID, Kind: "target-profile", Name: "invented-app"}
	profileRevision, err := meta.GetLatestResourceRevision(ctx, profileCoord)
	if err != nil {
		t.Fatalf("get published virtual profile: %v", err)
	}
	if profileRevision.ContentDigest != store.RevisionDigest(profileIdentity.RevisionDigest) ||
		profileRevision.ArchiveDigest != "" || profileRevision.Assessment.HostPackageIdentity != nil ||
		profileRevision.Occurrence.SourcePath != "profiles/invented-app.yaml" || profileRevision.Occurrence.Commit != commit {
		t.Fatalf("virtual profile revision = %#v; want exact identity, provenance, and no archive", profileRevision)
	}
	rawProfile, err := blobs.Get(ctx, store.ArchiveDigest(profileIdentity.RevisionDigest))
	if err != nil || !bytes.Equal(rawProfile, profileBytes) {
		t.Fatalf("published virtual profile bytes differ from pinned profile: %v", err)
	}
	inspectionPayload, err := blobs.Get(ctx, store.ArchiveDigest(profileRevision.Assessment.InspectionManifestDigest))
	if err != nil {
		t.Fatalf("read virtual profile inspection payload: %v", err)
	}
	var evidence struct {
		ProfileIdentity *targetprofile.Identity `json:"profile_identity"`
	}
	if err := json.Unmarshal(inspectionPayload, &evidence); err != nil {
		t.Fatalf("decode virtual profile inspection payload: %v", err)
	}
	if evidence.ProfileIdentity == nil || *evidence.ProfileIdentity != profileIdentity {
		t.Fatalf("virtual profile inspection identity = %#v, want %#v", evidence.ProfileIdentity, profileIdentity)
	}
	profileCount := 0
	for _, resource := range revision.Resources {
		if resource.Coordinate.Kind == "target-profile" && resource.Coordinate.Name == "invented-app" {
			profileCount++
		}
	}
	if profileCount != 1 {
		t.Fatalf("BuildRegistryRevision target-profile resources = %d, want one for the shared profile pin", profileCount)
	}

	missingConfig := cfg
	missingConfig.HostPackageAssessmentProfiles = append(
		[]config.HostPackageAssessmentProfile(nil),
		cfg.HostPackageAssessmentProfiles...,
	)
	missingConfig.HostPackageAssessmentProfiles[0].ProfileIdentity.SourceID = strings.Replace(
		profileIdentity.SourceID,
		"profiles%2Finvented-app.yaml",
		"profiles%2Fmissing.yaml",
		1,
	)
	missingRevision, err := BuildRegistryRevision(
		ctx, missingConfig, marketplaceID, registry, root, strings.Repeat("f", 40),
	)
	if err == nil || !strings.Contains(err.Error(), "marketplace.profile.unavailable") {
		t.Fatalf("BuildRegistryRevision with unavailable exact profile error = %v, want marketplace.profile.unavailable", err)
	}
	if !reflect.DeepEqual(missingRevision, Revision{}) {
		t.Fatalf("BuildRegistryRevision with unavailable exact profile returned a revision to publish: %#v", missingRevision)
	}
	afterUnavailable, err := meta.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatalf("get package revision after unavailable profile: %v", err)
	}
	if !reflect.DeepEqual(afterUnavailable, persisted) {
		t.Fatalf("unavailable profile changed visible package revision:\nbefore: %#v\nafter:  %#v", persisted, afterUnavailable)
	}
	afterUnavailableProfile, err := meta.GetLatestResourceRevision(ctx, profileCoord)
	if err != nil {
		t.Fatalf("get profile revision after unavailable profile: %v", err)
	}
	if !reflect.DeepEqual(afterUnavailableProfile, profileRevision) {
		t.Fatalf("unavailable profile changed visible profile revision:\nbefore: %#v\nafter:  %#v", profileRevision, afterUnavailableProfile)
	}

	alternateProfilePath := filepath.Join(root, "profiles", "invented-app-copy.yaml")
	writeAssessmentProfileFile(t, alternateProfilePath, profileBytes)
	alternateProfileIdentity := profileIdentity
	alternateProfileIdentity.SourceID = strings.Replace(
		profileIdentity.SourceID,
		"profiles%2Finvented-app.yaml",
		"profiles%2Finvented-app-copy.yaml",
		1,
	)
	ambiguousConfig := cfg
	ambiguousConfig.HostPackageAssessmentProfiles = append([]config.HostPackageAssessmentProfile(nil), cfg.HostPackageAssessmentProfiles...)
	ambiguousConfig.HostPackageAssessmentProfiles[1].ProfileIdentity = alternateProfileIdentity
	if _, err := BuildRegistryRevision(ctx, ambiguousConfig, marketplaceID, registry, root, commit); err == nil ||
		!strings.Contains(err.Error(), "marketplace.profile.selection-ambiguous") {
		t.Fatalf("BuildRegistryRevision with multiple profile identities for one target error = %v, want ambiguous selection refusal", err)
	}

	legacy, err := BuildRegistryRevision(ctx, testConfig(t, root), marketplaceID, registry, root, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision without profile entry: %v", err)
	}
	for _, resource := range legacy.Resources {
		if resource.Coordinate.Kind == "invented-app/mod" || resource.Coordinate.Kind == "target-profile" {
			t.Fatalf("unconfigured build produced scoped resource: %+v", resource)
		}
	}

	indexOnly := registry
	indexOnly.Acquisition = store.AcquisitionIndexOnly
	if _, err := BuildRegistryRevision(ctx, cfg, marketplaceID, indexOnly, root, commit); err == nil ||
		!strings.Contains(err.Error(), "marketplace.package.acquisition-unsupported") {
		t.Fatalf("index-only BuildRegistryRevision error = %v, want acquisition refusal", err)
	}

	changedProfile := bytes.Replace(profileBytes, []byte("evidence: fixture"), []byte("evidence: updated"), 1)
	writeAssessmentProfileFile(t, filepath.Join(root, "profiles", "invented-app.yaml"), changedProfile)
	if _, err := BuildRegistryRevision(ctx, cfg, marketplaceID, registry, root, commit); err == nil ||
		!strings.Contains(err.Error(), "marketplace.profile.digest-mismatch") {
		t.Fatalf("BuildRegistryRevision after profile change error = %v, want profile digest refusal", err)
	}
	if got, err := os.ReadFile(payloadPath); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("package payload changed: bytes=%q err=%v", got, err)
	}
}

func placeRequirementNode(t *testing.T, raw string) yaml.Node {
	t.Helper()
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatalf("parse place requirement %q: %v", raw, err)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		t.Fatalf("place requirement %q has invalid YAML node: %#v", raw, document)
	}
	return *document.Content[0]
}

func TestPlaceStoreFindingsOmitsPayloadLocations(t *testing.T) {
	got := placeStoreFindings([]analyze.Finding{{
		Code: analyze.FindingHTMLComment, Severity: analyze.OutcomeWarn,
		PolicyOutcome: analyze.OutcomeWarn, Message: "HTML comment (12 bytes)",
		SourceRange: &analyze.SourceRange{File: "source.txt", Line: 1, Column: 1},
	}})
	if len(got) != 1 || got[0].Code != analyze.FindingHTMLComment ||
		got[0].Severity != analyze.OutcomeWarn || got[0].Policy != analyze.OutcomeWarn ||
		got[0].Message != "HTML comment (12 bytes)" || got[0].Location != nil {
		t.Fatalf("placeStoreFindings() = %+v, want metadata only without payload location", got)
	}
}
