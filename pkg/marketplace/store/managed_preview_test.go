package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func previewFixture(id string) (store.ManagedPreview, store.Job) {
	preview := store.ManagedPreview{ID: id, Marketplace: "managed-test", Actor: store.ManagedActor{Issuer: "sso", Subject: "alice"}, BaseRevision: 1, Registry: json.RawMessage(`{"id":"registry","origin":"https://git.test/org/repo"}`)}
	job := store.Job{ID: store.JobID("preview-job-" + id), Kind: "managed-preview", Payload: []byte(`{"marketplace_id":"managed-test","preview_id":"` + id + `"}`), IdempotencyKey: "preview-job-" + id, MaxAttempts: 1}
	return preview, job
}

func TestManagedPreviewLifecycle(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	previews := meta.(store.ManagedPreviewStore)
	preview, job := previewFixture("p1")

	created, err := previews.CreateManagedPreview(ctx, preview, job)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != store.ManagedPreviewPending || created.JobID != job.ID || created.CreatedAt.IsZero() {
		t.Fatalf("unexpected created preview: %+v", created)
	}
	if _, err := meta.GetJob(ctx, job.ID); err != nil {
		t.Fatalf("preview job was not enqueued atomically: %v", err)
	}
	if _, err := previews.CreateManagedPreview(ctx, preview, job); err == nil {
		t.Fatal("duplicate preview id must be rejected")
	}

	loaded, err := previews.GetManagedPreview(ctx, "managed-test", "p1")
	if err != nil || loaded.ID != "p1" || string(loaded.Registry) != string(preview.Registry) {
		t.Fatalf("round trip mismatch: %+v %v", loaded, err)
	}
	if _, err := previews.GetManagedPreview(ctx, "other-marketplace", "p1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("previews must be marketplace scoped, got %v", err)
	}

	loaded.Status = store.ManagedPreviewRunning
	if err := previews.UpdateManagedPreview(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	loaded.Status = store.ManagedPreviewSucceeded
	if err := previews.UpdateManagedPreview(ctx, loaded); !errors.Is(err, store.ErrManagedPreviewInvalid) {
		t.Fatalf("terminal status without result must be invalid, got %v", err)
	}
	loaded.Result = &store.ManagedPreviewResult{Commit: "abc", ObservedAt: time.Now().UTC(), Roots: []store.ManagedPreviewRoot{{Path: ".", Resources: []store.ManagedPreviewResource{{Kind: "skill", Name: "a"}}, ResourceCount: 1}}, TotalResources: 1}
	loaded.CompletedAt = time.Now().UTC()
	if err := previews.UpdateManagedPreview(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	final, err := previews.GetManagedPreview(ctx, "managed-test", "p1")
	if err != nil || final.Status != store.ManagedPreviewSucceeded || final.Result == nil || final.Result.TotalResources != 1 {
		t.Fatalf("terminal record not persisted: %+v %v", final, err)
	}
	final.Failure, final.Result = &store.ManagedPreviewFailure{Code: "late", Message: "x"}, nil
	if err := previews.UpdateManagedPreview(ctx, final); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("terminal preview must not be overwritten, got %v", err)
	}
	if err := previews.UpdateManagedPreview(ctx, store.ManagedPreview{ID: "missing", Marketplace: "managed-test", Actor: preview.Actor, BaseRevision: 1, Registry: preview.Registry, Status: store.ManagedPreviewRunning}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing preview update should be not found, got %v", err)
	}

	if err := previews.PruneManagedPreviews(ctx, "managed-test", time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := previews.GetManagedPreview(ctx, "managed-test", "p1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected pruned preview, got %v", err)
	}
}

func TestManagedPreviewRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	previews := meta.(store.ManagedPreviewStore)
	for name, mutate := range map[string]func(*store.ManagedPreview, *store.Job){
		"bad id":        func(p *store.ManagedPreview, _ *store.Job) { p.ID = "has space" },
		"no revision":   func(p *store.ManagedPreview, _ *store.Job) { p.BaseRevision = 0 },
		"bad registry":  func(p *store.ManagedPreview, _ *store.Job) { p.Registry = json.RawMessage(`{`) },
		"no actor":      func(p *store.ManagedPreview, _ *store.Job) { p.Actor = store.ManagedActor{} },
		"wrong job":     func(_ *store.ManagedPreview, j *store.Job) { j.Kind = "managed-refresh" },
		"terminal seed": func(p *store.ManagedPreview, _ *store.Job) { p.Status = store.ManagedPreviewFailed },
	} {
		preview, job := previewFixture("bad")
		mutate(&preview, &job)
		if _, err := previews.CreateManagedPreview(ctx, preview, job); !errors.Is(err, store.ErrManagedPreviewInvalid) {
			t.Errorf("%s: expected invalid, got %v", name, err)
		}
		if _, err := meta.GetJob(ctx, job.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s: rejected preview must not leave a job behind: %v", name, err)
		}
	}
}
