package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: claude-enterprise-delivery/publication-completion-enqueues-delivery
func TestConformanceClaudeDeliveryLeaseScopesJobsToInstance(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	jobs, ok := meta.(store.ClaudeDeliveryJobStore)
	if !ok {
		t.Fatal("metadata store does not implement Claude delivery job leasing")
	}
	payloadA, err := json.Marshal(store.ClaudeDeliveryJobPayload{Marketplace: "instance-a"})
	if err != nil {
		t.Fatal(err)
	}
	payloadB, err := json.Marshal(store.ClaudeDeliveryJobPayload{Marketplace: "instance-b"})
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Truncate(time.Microsecond)
	queued := []store.Job{
		{ID: "job-a-first", Kind: store.ClaudeDeliveryJobKind, Payload: payloadA, MaxAttempts: 3, CreatedAt: created, UpdatedAt: created},
		{ID: "job-b-second", Kind: store.ClaudeDeliveryJobKind, Payload: payloadA, MaxAttempts: 3, CreatedAt: created.Add(time.Second), UpdatedAt: created.Add(time.Second)},
		{ID: "job-c-other", Kind: store.ClaudeDeliveryJobKind, Payload: payloadB, MaxAttempts: 3, CreatedAt: created.Add(2 * time.Second), UpdatedAt: created.Add(2 * time.Second)},
	}
	for _, job := range queued {
		if err := meta.EnqueueJob(ctx, job); err != nil {
			t.Fatalf("enqueue %s: %v", job.ID, err)
		}
	}

	first, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-a", "", "worker-a", time.Minute)
	if err != nil || first.ID != queued[0].ID {
		t.Fatalf("first instance-a lease = %+v, %v", first, err)
	}
	if _, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-a", queued[1].ID, "worker-a2", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second instance-a lease while blocked = %v, want ErrNotFound", err)
	}
	if _, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-a", queued[2].ID, "worker-a3", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("instance-b job leased as instance-a = %v, want ErrNotFound", err)
	}
	other, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-b", queued[2].ID, "worker-b", time.Minute)
	if err != nil || other.ID != queued[2].ID {
		t.Fatalf("instance-b lease = %+v, %v", other, err)
	}
	second, err := meta.GetJob(ctx, queued[1].ID)
	if err != nil || second.Attempt != 0 {
		t.Fatalf("blocked instance-a job attempt = %d, %v, want zero", second.Attempt, err)
	}
	if err := meta.CompleteJob(ctx, first.ID, "worker-a"); err != nil {
		t.Fatalf("complete first instance-a job: %v", err)
	}
	remaining, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-a", "", "worker-a2", time.Minute)
	if err != nil || remaining.ID != queued[1].ID {
		t.Fatalf("remaining instance-a lease = %+v, %v", remaining, err)
	}
}

// spec: claude-enterprise-delivery/publication-completion-enqueues-delivery
func TestConformanceClaudeDeliveryFailureCooldownScopesJobsToInstance(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	jobs, ok := meta.(store.ClaudeDeliveryJobStore)
	if !ok {
		t.Fatal("metadata store does not implement Claude delivery job leasing")
	}
	payloadA, err := json.Marshal(store.ClaudeDeliveryJobPayload{Marketplace: "instance-a"})
	if err != nil {
		t.Fatal(err)
	}
	payloadB, err := json.Marshal(store.ClaudeDeliveryJobPayload{Marketplace: "instance-b"})
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Truncate(time.Microsecond)
	queued := []store.Job{
		{ID: "job-a-exhausted", Kind: store.ClaudeDeliveryJobKind, Payload: payloadA, MaxAttempts: 1, CreatedAt: created, UpdatedAt: created},
		{ID: "job-a-operator", Kind: store.ClaudeDeliveryJobKind, Payload: payloadA, MaxAttempts: 3, CreatedAt: created.Add(time.Second), UpdatedAt: created.Add(time.Second)},
		{ID: "job-b-other", Kind: store.ClaudeDeliveryJobKind, Payload: payloadB, MaxAttempts: 3, CreatedAt: created.Add(2 * time.Second), UpdatedAt: created.Add(2 * time.Second)},
	}
	for _, job := range queued {
		if err := meta.EnqueueJob(ctx, job); err != nil {
			t.Fatalf("enqueue %s: %v", job.ID, err)
		}
	}

	active, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-a", queued[0].ID, "worker-a", time.Minute)
	if err != nil || active.ID != queued[0].ID || active.Attempt != 1 {
		t.Fatalf("active instance-a lease = %+v, %v", active, err)
	}
	const cooldown = time.Hour
	if err := meta.FailJob(ctx, active.ID, "worker-a", cooldown); err != nil {
		t.Fatalf("fail active instance-a job: %v", err)
	}
	exhausted, err := meta.GetJob(ctx, active.ID)
	if err != nil || exhausted.Status != store.JobStatusFailed ||
		exhausted.Attempt != exhausted.MaxAttempts || !exhausted.LeaseUntil.After(time.Now()) {
		t.Fatalf("failed job does not retain its exhausted-attempt cooldown: %+v, %v", exhausted, err)
	}

	if _, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-a", queued[1].ID, "worker-a-operator", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("explicit instance-a lease during cooldown = %v, want ErrNotFound", err)
	}
	if _, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-a", "", "worker-a-spontaneous", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("spontaneous instance-a lease during cooldown = %v, want ErrNotFound", err)
	}
	other, err := jobs.LeaseClaudeDeliveryJob(ctx, "instance-b", queued[2].ID, "worker-b", time.Minute)
	if err != nil || other.ID != queued[2].ID {
		t.Fatalf("instance-b lease during instance-a cooldown = %+v, %v", other, err)
	}
	operator, err := meta.GetJob(ctx, queued[1].ID)
	if err != nil || operator.Attempt != 0 {
		t.Fatalf("blocked operator job attempt = %d, %v, want zero", operator.Attempt, err)
	}
}

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
	desired, err := delivery.ListClaudeDeliveryDesired(ctx, meta.(store.PublicationStore), "managed-test", []store.RegistryID{"registry"})
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
	desired, err := delivery.ListClaudeDeliveryDesired(ctx, meta.(store.PublicationStore), "managed-test", []store.RegistryID{"registry"})
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
