package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

func managedRuntimeRequest(parent int64, key string) store.ManagedChangeRequest {
	req := managedRequest(parent, key)
	req.Change.Snapshot = publicationSnapshot(1, 1, false, false, false)
	req.Jobs[0].Payload = []byte(fmt.Sprintf(`{"marketplace_id":"managed-test","registry_id":"registry","revision":%d}`, parent+1))
	return req
}

func mutateRuntimeSnapshot(t testing.TB, raw json.RawMessage, change func(map[string]any, []any)) json.RawMessage {
	t.Helper()
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	registries, ok := snapshot["registries"].([]any)
	if !ok {
		t.Fatal("fixture has no registry array")
	}
	change(snapshot, registries)
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func enqueueRuntimeRefresh(t testing.TB, meta store.MetadataStore, id store.JobID, revision, generation, policy int64, parent store.JobID, maxAttempts int) (store.Job, store.ManagedFence) {
	t.Helper()
	ctx := context.Background()
	operation := "scheduled"
	if parent != "" {
		operation = "reconcile"
	}
	payload := []byte(fmt.Sprintf(`{"marketplace_id":"managed-test","registry_id":"registry","registry_generation":%d,"policy_revision":%d,"revision":%d,"parent_job_id":%q,"operation":%q,"raw_git_output":"credential=never-expose-this"}`, generation, policy, revision, parent, operation))
	if err := meta.EnqueueJob(ctx, store.Job{ID: id, Kind: "managed-refresh", Payload: payload, MaxAttempts: maxAttempts, IdempotencyKey: string(id)}); err != nil {
		t.Fatal(err)
	}
	job, err := meta.LeaseJobByID(ctx, id, "worker-"+string(id), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fence := store.ManagedFence{Marketplace: "managed-test", RegistryGeneration: generation, PolicyRevision: policy, JobID: id, WorkerID: job.WorkerID, Attempt: job.Attempt}
	current, err := meta.(store.PublicationStore).CurrentRegistryPublication(ctx, "registry")
	if err == nil {
		fence.ExpectedPublicationID = current.ID
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	return job, fence
}

func TestManagedAttemptsPublicationAndCompletionAtomic(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	request := managedRuntimeRequest(0, "adopt")
	accepted, err := managed.ApplyManagedConfiguration(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if registry, err := meta.GetRegistry(ctx, "registry"); err != nil || registry.ID != "registry" {
		t.Fatalf("adoption did not project identity: %+v %v", registry, err)
	}
	recorder := meta.(store.ManagedRefreshAttemptStore)
	job, fence := enqueueRuntimeRefresh(t, meta, "refresh", 1, 1, 1, accepted.JobIDs[0], 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
		t.Fatalf("started retry: %v", err)
	}
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	publication, _ := blobOwnershipPublication(t, "registry", "publication-one", "# Managed attempt")
	publication.ManagedFence = &fence
	abort := errors.New("abort outer worker transaction")
	commit := func(aborted bool) error {
		return meta.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
			if err := store.PublishRegistryRevision(txCtx, tx, blobs, publication); err != nil {
				return err
			}
			if err := tx.(store.ManagedRefreshAttemptStore).RecordManagedRefreshAttempt(txCtx, fence, "succeeded", publication.PublicationID); err != nil {
				return err
			}
			if err := tx.CompleteJob(txCtx, job.ID, job.WorkerID); err != nil {
				return err
			}
			if aborted {
				return abort
			}
			return nil
		})
	}
	if err := commit(true); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if _, err := meta.(store.PublicationStore).CurrentRegistryPublication(ctx, "registry"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("outer rollback leaked publication: %v", err)
	}
	history, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 10})
	if err != nil || len(history) != 1 || history[0].Outcome != "started" {
		t.Fatalf("outer rollback leaked success: %+v %v", history, err)
	}
	raw, err := meta.GetJob(ctx, job.ID)
	if err != nil || raw.Status != store.JobStatusLeased {
		t.Fatalf("outer rollback completed job: %+v %v", raw, err)
	}
	if err := commit(false); err != nil {
		t.Fatal(err)
	}
	state, err := recorder.GetManagedRegistryRefreshState(ctx, "managed-test", "registry")
	if err != nil || state.LastSuccess == nil || state.LastSuccess.PublicationID != "publication-one" || state.LastAttempt.Outcome != "succeeded" || state.CurrentState != "succeeded" {
		t.Fatalf("success state: %+v %v", state, err)
	}
	history, err = recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", JobID: job.ID, Limit: 1})
	if err != nil || len(history) != 1 || history[0].Outcome != "succeeded" || history[0].Actor != request.Change.Actor || history[0].RequestID != request.Change.RequestID || history[0].ParentJobID != accepted.JobIDs[0] {
		t.Fatalf("request attribution: %+v %v", history, err)
	}
	older, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Registry: "registry", Before: history[0].Sequence, Limit: 10})
	if err != nil || len(older) != 1 || older[0].Outcome != "started" {
		t.Fatalf("bounded event cursor: %+v %v", older, err)
	}
	view, err := recorder.GetManagedRefreshJob(ctx, "managed-test", job.ID)
	if err != nil || view.State != "completed" || !view.Terminal {
		t.Fatalf("completed job view: %+v %v", view, err)
	}
	encoded, _ := json.Marshal([]any{state, history, older, view})
	if strings.Contains(string(encoded), "never-expose-this") || strings.Contains(string(encoded), "raw_git_output") {
		t.Fatalf("raw execution payload leaked: %s", encoded)
	}
	if !strings.Contains(string(raw.Payload), "never-expose-this") {
		t.Fatal("redaction fixture had no sensitive data")
	}
	if _, err := recorder.GetManagedRefreshJob(ctx, "other-marketplace", job.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("job scope crossed: %v", err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "failed", ""); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("completed lease appended new outcome: %v", err)
	}
}

func TestManagedAttemptsRejectForeignOrStaleLeaseAndPublication(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	recorder := meta.(store.ManagedRefreshAttemptStore)
	_, fence := enqueueRuntimeRefresh(t, meta, "one", 1, 1, 1, "", 2)
	for _, scenario := range []string{"worker", "attempt", "generation", "policy", "outcome"} {
		changed := fence
		outcome := "started"
		want := store.ErrLeaseExpired
		switch scenario {
		case "worker":
			changed.WorkerID = "other"
		case "attempt":
			changed.Attempt++
		case "generation":
			changed.RegistryGeneration++
			want = store.ErrPublicationFence
		case "policy":
			changed.PolicyRevision++
			want = store.ErrPublicationFence
		case "outcome":
			outcome = "failed: raw git secret"
			want = store.ErrManagedAttemptInvalid
		}
		if err := recorder.RecordManagedRefreshAttempt(ctx, changed, outcome, ""); !errors.Is(err, want) {
			t.Fatalf("%s: %v", scenario, err)
		}
	}
	events, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 100})
	if err != nil || len(events) != 0 {
		t.Fatalf("rejected claims appended events: %+v %v", events, err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
		t.Fatal(err)
	}
	_, other := enqueueRuntimeRefresh(t, meta, "other", 1, 1, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, other, "started", ""); err != nil {
		t.Fatal(err)
	}
	publication := publicationRequest(t, "foreign-publication", "resource")
	publication.ManagedFence = &fence
	err = meta.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		if err := tx.(store.PublicationStore).PublishRegistryPublication(txCtx, publication); err != nil {
			return err
		}
		return tx.(store.ManagedRefreshAttemptStore).RecordManagedRefreshAttempt(txCtx, other, "succeeded", publication.PublicationID)
	})
	if !errors.Is(err, store.ErrPublicationFence) {
		t.Fatalf("another job claimed publication: %v", err)
	}
	if _, err := meta.(store.PublicationStore).CurrentRegistryPublication(ctx, "registry"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign claim failure leaked publication: %v", err)
	}
	if err := meta.ExtendLease(ctx, fence.JobID, fence.WorkerID, -time.Second); err != nil {
		t.Fatal(err)
	}
	replacement, err := meta.LeaseJobByID(ctx, fence.JobID, "replacement", time.Minute)
	if err != nil || replacement.Attempt != 2 {
		t.Fatalf("re-lease control: %+v %v", replacement, err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "failed", ""); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("lease loser appended failure: %v", err)
	}
	if err := meta.CompleteJob(ctx, fence.JobID, fence.WorkerID); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("lease loser completed replacement: %v", err)
	}
}

func TestManagedAttemptsSupersededCannotReplaceNewAttemptOrSuccess(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	recorder := meta.(store.ManagedRefreshAttemptStore)
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	_, first := enqueueRuntimeRefresh(t, meta, "first", 1, 1, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, first, "started", ""); err != nil {
		t.Fatal(err)
	}
	publication := publicationRequest(t, "successful", "resource")
	publication.ManagedFence = &first
	if err := meta.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		if err := tx.(store.PublicationStore).PublishRegistryPublication(txCtx, publication); err != nil {
			return err
		}
		if err := tx.(store.ManagedRefreshAttemptStore).RecordManagedRefreshAttempt(txCtx, first, "succeeded", publication.PublicationID); err != nil {
			return err
		}
		return tx.CompleteJob(txCtx, first.JobID, first.WorkerID)
	}); err != nil {
		t.Fatal(err)
	}
	_, old := enqueueRuntimeRefresh(t, meta, "old", 1, 1, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, old, "started", ""); err != nil {
		t.Fatal(err)
	}
	edit := managedRuntimeRequest(1, "edit")
	edit.Change.Snapshot = publicationSnapshot(2, 1, false, false, false)
	edit.Change.Snapshot = mutateRuntimeSnapshot(t, edit.Change.Snapshot, func(_ map[string]any, registries []any) { registries[0].(map[string]any)["ref"] = "new-desired-ref" })
	if _, err := managed.ApplyManagedConfiguration(ctx, edit); err != nil {
		t.Fatal(err)
	}
	_, newer := enqueueRuntimeRefresh(t, meta, "newer", 2, 2, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, newer, "started", ""); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := recorder.RecordManagedRefreshAttempt(ctx, old, "superseded", ""); err != nil {
			t.Fatal(err)
		}
	}
	state, err := recorder.GetManagedRegistryRefreshState(ctx, "managed-test", "registry")
	if err != nil || state.LastAttempt.JobID != "newer" || state.LastAttempt.Outcome != "started" || state.LastSuccess.PublicationID != "successful" {
		t.Fatalf("late result overwrote status: %+v %v", state, err)
	}
	registry, err := meta.GetRegistry(ctx, "registry")
	if err != nil || registry.Ref != "new-desired-ref" {
		t.Fatalf("late result overwrote desired settings: %+v %v", registry, err)
	}
	current, err := meta.(store.PublicationStore).CurrentRegistryPublication(ctx, "registry")
	if err != nil || current.ID != "successful" {
		t.Fatalf("late result moved publication: %+v %v", current, err)
	}
	oldEvents, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", JobID: old.JobID, Limit: 100})
	if err != nil || len(oldEvents) != 2 || oldEvents[0].Outcome != "superseded" {
		t.Fatalf("append-only outcome retry: %+v %v", oldEvents, err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, old, "failed", ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("terminal outcome rewritten: %v", err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, newer, "failed", ""); err != nil {
		t.Fatal(err)
	}
	state, err = recorder.GetManagedRegistryRefreshState(ctx, "managed-test", "registry")
	if err != nil || state.LastAttempt.Outcome != "failed" || state.LastAttempt.JobID != newer.JobID || state.LastSuccess.PublicationID != "successful" {
		t.Fatalf("failed attempt erased successful publication status: %+v %v", state, err)
	}
}

func TestManagedAttemptRejectsUnattributedParentAndReadOverflow(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	if err := meta.EnqueueJob(ctx, store.Job{ID: "unattributed-parent", Kind: "managed-reconcile", Payload: []byte(`{"marketplace_id":"managed-test","registry_id":"registry","revision":1}`)}); err != nil {
		t.Fatal(err)
	}
	_, fence := enqueueRuntimeRefresh(t, meta, "unattributed", 1, 1, 1, "unattributed-parent", 1)
	recorder := meta.(store.ManagedRefreshAttemptStore)
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "started", ""); !errors.Is(err, store.ErrManagedAttemptInvalid) {
		t.Fatalf("unattributed parent accepted: %v", err)
	}
	if events, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 100}); err != nil || len(events) != 0 {
		t.Fatalf("invalid parent recorded: %+v %v", events, err)
	}
	_, valid := enqueueRuntimeRefresh(t, meta, "attributed", 1, 1, 1, "job-adopt", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, valid, "started", ""); err != nil {
		t.Fatalf("attributed control: %v", err)
	}
	if _, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 101}); !errors.Is(err, store.ErrManagedAttemptInvalid) {
		t.Fatalf("unbounded event read accepted: %v", err)
	}
}

func TestManagedPreflightTerminalOutcomesDoNotReplaceNewAttempt(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	recorder := meta.(store.ManagedRefreshAttemptStore)
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	_, skipped := enqueueRuntimeRefresh(t, meta, "skipped", 1, 1, 1, "", 1)
	pause := managedRuntimeRequest(1, "pause")
	pause.Change.Snapshot = publicationSnapshot(2, 1, true, false, false)
	if _, err := managed.ApplyManagedConfiguration(ctx, pause); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, skipped, "superseded", ""); err != nil {
		t.Fatalf("owned preflight rejection: %v", err)
	}
	if state, err := recorder.GetManagedRegistryRefreshState(ctx, "managed-test", "registry"); err != nil || state.LastAttempt.Outcome != "superseded" || state.LastSuccess != nil {
		t.Fatalf("preflight history: %+v %v", state, err)
	}
	resume := managedRuntimeRequest(2, "resume")
	resume.Change.Snapshot = publicationSnapshot(3, 1, false, false, false)
	if _, err := managed.ApplyManagedConfiguration(ctx, resume); err != nil {
		t.Fatal(err)
	}
	_, newer := enqueueRuntimeRefresh(t, meta, "newest", 3, 3, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, newer, "started", ""); err != nil {
		t.Fatal(err)
	}
	_, late := enqueueRuntimeRefresh(t, meta, "late-preflight", 1, 1, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, late, "failed", ""); err != nil {
		t.Fatal(err)
	}
	if state, err := recorder.GetManagedRegistryRefreshState(ctx, "managed-test", "registry"); err != nil || state.LastAttempt.JobID != newer.JobID || state.LastAttempt.Outcome != "started" {
		t.Fatalf("unstarted old work replaced newer state: %+v %v", state, err)
	}
	if events, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 100}); err != nil || len(events) != 3 || events[0].Outcome != "failed" {
		t.Fatalf("preflight events lost: %+v %v", events, err)
	}
}

func TestManagedExhaustedLeaseIsTerminalWithoutQueueRewrite(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	recorder := meta.(store.ManagedRefreshAttemptStore)
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	job, fence := enqueueRuntimeRefresh(t, meta, "exhausted", 1, 1, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
		t.Fatal(err)
	}
	before, err := recorder.GetManagedRefreshJob(ctx, "managed-test", job.ID)
	if err != nil || before.State != "leased" || before.Terminal {
		t.Fatalf("live control: %+v %v", before, err)
	}
	if err := meta.ExtendLease(ctx, job.ID, job.WorkerID, -time.Second); err != nil {
		t.Fatal(err)
	}
	view, err := recorder.GetManagedRefreshJob(ctx, "managed-test", job.ID)
	if err != nil || view.State != "exhausted" || !view.Terminal || view.RawStatus != store.JobStatusLeased {
		t.Fatalf("expired exhausted job mislabeled active: %+v %v", view, err)
	}
	state, err := recorder.GetManagedRegistryRefreshState(ctx, "managed-test", "registry")
	if err != nil || state.CurrentState != "exhausted" || state.LastAttempt.Outcome != "started" {
		t.Fatalf("immutable event classification: %+v %v", state, err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "failed", ""); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("expired owner recorded: %v", err)
	}
	if err := meta.CompleteJob(ctx, job.ID, job.WorkerID); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("expired owner completed: %v", err)
	}
	if _, err := meta.LeaseJobByID(ctx, job.ID, "another", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("classification increased retry cap: %v", err)
	}
	raw, err := meta.GetJob(ctx, job.ID)
	if err != nil || raw.Status != store.JobStatusLeased || raw.Attempt != 1 {
		t.Fatalf("classification rewrote queue: %+v %v", raw, err)
	}
}

func TestManagedProjectionPreservesStatusHistoryAndRetiredAnchors(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	legacy := store.Registry{ID: "registry", Slug: "old-slug", Aliases: []string{"ancient"}, Origins: []string{"https://old.test/repository"}, Ref: "old-ref", Acquisition: store.AcquisitionIndexOnly, Listed: false, RefreshStatus: store.RegistryRefreshStatusFailed, RefreshError: "bounded-diagnostic", RefreshedAt: time.Unix(100, 0).UTC(), CreatedAt: time.Unix(50, 0).UTC()}
	if err := meta.PutRegistry(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	occurrence := store.RegistryOccurrence{RegistryID: "registry", Ref: "old-ref", Commit: "old-commit", Acquisition: store.AcquisitionIndexOnly, RegistryRevisionManifestDigest: "old-manifest"}
	if err := meta.PutRegistryOccurrence(ctx, occurrence); err != nil {
		t.Fatal(err)
	}
	revocation := store.Revocation{Kind: store.RevocationRegistry, TargetID: "registry", Reason: "deployment-owned", AppliedAt: time.Unix(200, 0).UTC()}
	if err := meta.PutRevocation(ctx, revocation); err != nil {
		t.Fatal(err)
	}
	adopt := managedRuntimeRequest(0, "adopt")
	adopt.Change.Snapshot = mutateRuntimeSnapshot(t, adopt.Change.Snapshot, func(_ map[string]any, registries []any) {
		r := registries[0].(map[string]any)
		r["slug"] = "new-slug"
		r["profile_id"] = "opaque-profile"
		r["publisher"] = map[string]any{"name": "Publisher", "url": "https://publisher.test"}
		r["license"] = "MIT"
		r["non_license_overrides"] = map[string]any{"hidden_content": "reject"}
		r["roots"] = []any{map[string]any{"path": "skills", "source_harness": "factory"}}
	})
	if _, err := managed.ApplyManagedConfiguration(ctx, adopt); err != nil {
		t.Fatal(err)
	}
	projected, err := meta.GetRegistry(ctx, "registry")
	if err != nil || projected.Slug != "new-slug" || projected.CredentialsRef != "opaque-profile" || projected.Publisher.Name != "Publisher" || projected.License != "MIT" || projected.PolicyOverrides["hidden_content"] != "reject" || !projected.Listed || !reflect.DeepEqual(projected.Aliases, []string{"ancient", "old-slug"}) || !reflect.DeepEqual(projected.Origins, []string{"https://old.test/repository", "https://example.test/registry.git"}) || projected.RefreshedAt != legacy.RefreshedAt || projected.RefreshStatus != legacy.RefreshStatus || projected.RefreshError != legacy.RefreshError || projected.CreatedAt != legacy.CreatedAt {
		t.Fatalf("identity/status projection: %+v %v", projected, err)
	}
	projected.RefreshStatus = store.RegistryRefreshStatusOK
	projected.RefreshedAt = time.Unix(300, 0).UTC()
	projected.RefreshError = ""
	if err := meta.PutRegistry(ctx, projected); err != nil {
		t.Fatal(err)
	}
	edit := managedRuntimeRequest(1, "edit")
	edit.Change.Snapshot = mutateRuntimeSnapshot(t, adopt.Change.Snapshot, func(_ map[string]any, registries []any) {
		r := registries[0].(map[string]any)
		r["generation"] = 2
		r["ref"] = "desired-ref"
	})
	if _, err := managed.ApplyManagedConfiguration(ctx, edit); err != nil {
		t.Fatal(err)
	}
	refreshed, err := meta.GetRegistry(ctx, "registry")
	if err != nil || refreshed.Ref != "desired-ref" || refreshed.RefreshedAt != projected.RefreshedAt || refreshed.RefreshStatus != store.RegistryRefreshStatusOK || refreshed.CreatedAt != legacy.CreatedAt {
		t.Fatalf("config edit clobbered status: %+v %v", refreshed, err)
	}
	retire := managedRuntimeRequest(2, "retire")
	retire.Change.Snapshot = mutateRuntimeSnapshot(t, edit.Change.Snapshot, func(_ map[string]any, registries []any) {
		r := registries[0].(map[string]any)
		r["generation"] = 3
		r["lifecycle"] = "retired"
		r["paused"] = true
		r["withdrawn"] = true
		r["retired"] = true
	})
	if _, err := managed.ApplyManagedConfiguration(ctx, retire); err != nil {
		t.Fatal(err)
	}
	retired, err := meta.GetRegistry(ctx, "registry")
	if err != nil || !reflect.DeepEqual(refreshed, retired) {
		t.Fatalf("lifecycle edit rewrote registry row/status: %+v %v", retired, err)
	}
	if got, err := meta.GetRevocation(ctx, store.RevocationRegistry, "registry"); err != nil || !reflect.DeepEqual(got, revocation) {
		t.Fatalf("deployment revocation erased: %+v %v", got, err)
	}
	if history, err := meta.RegistryHistory(ctx, "registry"); err != nil || len(history) != 1 || history[0].Commit != "old-commit" {
		t.Fatalf("history erased: %+v %v", history, err)
	}
}

func TestManagedProjectionRejectsDeletedOrReassignedIdentity(t *testing.T) {
	for _, scenario := range []string{"deleted", "reassigned-id", "slug", "alias", "generation"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			meta, managed := managedStore(t)
			adopt := managedRuntimeRequest(0, "adopt")
			adopt.Change.Snapshot = mutateRuntimeSnapshot(t, adopt.Change.Snapshot, func(_ map[string]any, registries []any) {
				registries[0].(map[string]any)["aliases"] = []string{"reserved"}
			})
			if _, err := managed.ApplyManagedConfiguration(ctx, adopt); err != nil {
				t.Fatal(err)
			}
			edit := managedRuntimeRequest(1, "bad")
			edit.Change.Snapshot = mutateRuntimeSnapshot(t, adopt.Change.Snapshot, func(snapshot map[string]any, registries []any) {
				r := registries[0].(map[string]any)
				switch scenario {
				case "deleted":
					snapshot["registries"] = []any{}
				case "reassigned-id":
					r["id"] = "replacement"
				case "generation":
					r["ref"] = "changed-without-generation"
				default:
					other := map[string]any{}
					for key, value := range r {
						other[key] = value
					}
					other["id"] = "replacement"
					other["aliases"] = []string{}
					if scenario == "alias" {
						other["slug"] = "reserved"
					}
					snapshot["registries"] = append(registries, other)
				}
			})
			want := store.ErrManagedIdentityConflict
			if scenario == "generation" {
				want = store.ErrManagedChangeInvalid
			}
			if _, err := managed.ApplyManagedConfiguration(ctx, edit); !errors.Is(err, want) {
				t.Fatalf("%s accepted: %v", scenario, err)
			}
			if _, err := meta.GetJob(ctx, "job-bad"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("rejected identity queued work: %v", err)
			}
			if current, err := managed.CurrentManagedConfiguration(ctx, "managed-test"); err != nil || current.Revision != 1 {
				t.Fatalf("rejected identity committed audit: %+v %v", current, err)
			}
		})
	}
}

func TestManagedProjectionWorkFailureRollsBackIdentityReservations(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	adopt := managedRuntimeRequest(0, "adopt")
	if err := meta.EnqueueJob(ctx, adopt.Jobs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := managed.ApplyManagedConfiguration(ctx, adopt); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("work failure control: %v", err)
	}
	if _, err := meta.GetRegistry(ctx, "registry"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed work retained projected identity: %v", err)
	}
	if _, err := managed.CurrentManagedConfiguration(ctx, "managed-test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed work retained adoption: %v", err)
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "other", Name: "Other", CanonicalURL: "https://other.test"}); err != nil {
		t.Fatal(err)
	}
	other := managedRuntimeRequest(0, "other")
	other.Change.Marketplace = "other"
	other.Jobs = nil
	if _, err := managed.ApplyManagedConfiguration(ctx, other); err != nil {
		t.Fatalf("failed transaction left identity reservation: %v", err)
	}
	retry := managedRuntimeRequest(0, "retry")
	retry.Jobs = nil
	if _, err := managed.ApplyManagedConfiguration(ctx, retry); !errors.Is(err, store.ErrManagedIdentityConflict) {
		t.Fatalf("committed identity reassigned between marketplaces: %v", err)
	}
}
