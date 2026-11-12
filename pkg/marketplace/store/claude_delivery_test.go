package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: claude-enterprise-delivery/a-renamed-registry-slug-does-not-rename-a-live-plugin
func TestClaudeDeliveryStateKeepsRecordedNameAndBoundsFailures(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	delivery, ok := meta.(store.ClaudeDeliveryStateStore)
	if !ok {
		t.Fatal("metadata store does not implement Claude delivery state")
	}
	coordinate := store.ResourceCoordinate{Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: "guide"}
	now := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	first := store.ClaudeDeliveryPlugin{
		Coordinate: coordinate, PluginName: "am-old-slug-skill-guide", PluginID: "plugin_1",
		TargetMarketplaceID: "marketplace_1", PublishedArchiveDigest: "sha256:archive-one",
		PublishedContentDigest: "sha256:content-one", PublishedVersionID: "version_1",
		LatestVersionID: "version_1", ServedVersionID: "version_1",
		ContentScanStatus: "pass", Reach: "organization", InstallationPreference: "available",
		State: store.ClaudeDeliveryStatePublished, CreatedAt: now, UpdatedAt: now,
	}
	if err := delivery.UpsertClaudeDeliveryPublished(ctx, first); err != nil {
		t.Fatalf("UpsertClaudeDeliveryPublished: %v", err)
	}
	updated := first
	updated.PluginName = "am-new-slug-skill-guide"
	updated.PublishedArchiveDigest = "sha256:archive-two"
	updated.PublishedContentDigest = "sha256:content-two"
	updated.PublishedVersionID = "version_2"
	updated.LatestVersionID = "version_2"
	updated.UpdatedAt = now.Add(time.Minute)
	if err := delivery.UpsertClaudeDeliveryPublished(ctx, updated); err != nil {
		t.Fatalf("UpsertClaudeDeliveryPublished update: %v", err)
	}
	row, err := delivery.GetClaudeDeliveryPlugin(ctx, coordinate)
	if err != nil {
		t.Fatalf("GetClaudeDeliveryPlugin: %v", err)
	}
	if row.PluginName != first.PluginName || row.PublishedVersionID != "version_2" ||
		row.PublishedArchiveDigest != updated.PublishedArchiveDigest {
		t.Fatalf("delivery row after slug change = %+v", row)
	}
	if err := delivery.RecordClaudeDeliveryFailure(ctx, coordinate, row.PluginName, row.TargetMarketplaceID, "upload.failed", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("RecordClaudeDeliveryFailure: %v", err)
	}
	row, err = delivery.GetClaudeDeliveryPlugin(ctx, coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if row.LastErrorCode != "upload.failed" || row.LastErrorMessage == "" ||
		len([]rune(row.LastErrorMessage)) > store.ClaudeDeliveryFailureMessageLimit ||
		strings.Contains(row.LastErrorMessage, "apikey_") || strings.Contains(row.LastErrorMessage, "request body") {
		t.Fatalf("unsafe or unbounded failure: %+v", row)
	}
	if err := delivery.MarkClaudeDeliverySkipped(ctx, coordinate, row.PluginName, "archive.invalid-bin", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("MarkClaudeDeliverySkipped: %v", err)
	}
	row, err = delivery.GetClaudeDeliveryPlugin(ctx, coordinate)
	if err != nil || row.State != store.ClaudeDeliveryStateSkipped || row.LastErrorCode != "archive.invalid-bin" {
		t.Fatalf("skipped delivery = %+v, %v", row, err)
	}
}

// spec: claude-enterprise-delivery/a-rejected-resource-is-never-uploaded
func TestClaudeDeliveryDesiredSetExcludesRejectedResourcesAndCollections(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	if err := meta.PutRegistry(ctx, store.Registry{
		ID: "registry", Slug: "registry-slug", Ref: "main",
		Acquisition: store.AcquisitionIndexOnly, Listed: true,
	}); err != nil {
		t.Fatal(err)
	}
	req := publicationRequest(t, "delivery-publication", "allowed", "rejected", "collection")
	req.Resources[2].Coordinate.Kind = "collection"
	req.ResourceOccurrences[2].Coordinate.Kind = "collection"
	req.Assessments[2].Coordinate.Kind = "collection"
	req.Assessments[2].AssessmentDigest = ""
	collectionAssessment, err := store.FinalizeResourceAssessment(req.Assessments[2])
	if err != nil {
		t.Fatal(err)
	}
	req.Assessments[2] = collectionAssessment
	for i := range req.Assessments {
		if req.Resources[i].Coordinate.Name == "rejected" {
			req.Assessments[i].Decision = store.AssessmentDecisionReject
			req.Assessments[i].AssessmentDigest = ""
			finalized, err := store.FinalizeResourceAssessment(req.Assessments[i])
			if err != nil {
				t.Fatal(err)
			}
			req.Assessments[i] = finalized
		}
	}
	if err := meta.(store.PublicationStore).PublishRegistryPublication(ctx, req); err != nil {
		t.Fatalf("PublishRegistryPublication: %v", err)
	}
	delivery, ok := meta.(store.ClaudeDeliveryStateStore)
	if !ok {
		t.Fatal("metadata store does not implement Claude delivery state")
	}
	desired, err := delivery.ListClaudeDeliveryDesired(ctx, meta.(store.PublicationStore), []store.RegistryID{"registry"})
	if err != nil {
		t.Fatalf("ListClaudeDeliveryDesired: %v", err)
	}
	if len(desired) != 1 || desired[0].Resource.Coordinate.Name != "allowed" ||
		desired[0].PublicationID != req.PublicationID {
		t.Fatalf("desired set = %+v", desired)
	}
}

// spec: claude-enterprise-delivery/a-revocation-withdraws-a-mirrored-resource
func TestClaudeDeliveryDesiredSetExcludesApplicableRevocations(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	if err := meta.PutRegistry(ctx, store.Registry{
		ID: "registry", Slug: "registry-slug", Ref: "main",
		Acquisition: store.AcquisitionIndexOnly, Listed: true,
	}); err != nil {
		t.Fatal(err)
	}
	req := publicationRequest(t, "delivery-revocation", "guide")
	if err := meta.(store.PublicationStore).PublishRegistryPublication(ctx, req); err != nil {
		t.Fatalf("PublishRegistryPublication: %v", err)
	}
	revision := req.Resources[0]
	target := fmt.Sprintf("%s@%s", revision.Coordinate, revision.ContentDigest)
	if err := meta.PutRevocation(ctx, store.Revocation{
		Kind: store.RevocationRevision, TargetID: target, Reason: "withdrawn",
		AppliedAt: time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("PutRevocation: %v", err)
	}
	delivery, ok := meta.(store.ClaudeDeliveryStateStore)
	if !ok {
		t.Fatal("metadata store does not implement Claude delivery state")
	}
	desired, err := delivery.ListClaudeDeliveryDesired(ctx, meta.(store.PublicationStore), []store.RegistryID{"registry"})
	if err != nil {
		t.Fatalf("ListClaudeDeliveryDesired: %v", err)
	}
	if len(desired) != 0 {
		t.Fatalf("revoked desired set = %+v", desired)
	}
}

func TestClaudeDeliveryStateRecordsRetiredConflictAndFailureStates(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	delivery, ok := meta.(store.ClaudeDeliveryStateStore)
	if !ok {
		t.Fatal("metadata store does not implement Claude delivery state")
	}
	coordinate := store.ResourceCoordinate{Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: "guide"}
	now := time.Now().UTC()
	if err := delivery.MarkClaudeDeliveryConflict(ctx, coordinate, "am-guide", "plugin.foreign", now); err != nil {
		t.Fatalf("MarkClaudeDeliveryConflict: %v", err)
	}
	if err := delivery.MarkClaudeDeliveryRetired(ctx, coordinate, now.Add(time.Second)); err != nil {
		t.Fatalf("MarkClaudeDeliveryRetired: %v", err)
	}
	rows, err := delivery.ListClaudeDeliveryPlugins(ctx)
	if err != nil {
		t.Fatalf("ListClaudeDeliveryPlugins: %v", err)
	}
	if len(rows) != 1 || rows[0].State != store.ClaudeDeliveryStateRetired {
		t.Fatalf("delivery rows = %+v", rows)
	}
	if _, err := delivery.GetClaudeDeliveryPlugin(ctx, store.ResourceCoordinate{Registry: "absent"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing row error = %v", err)
	}
}
