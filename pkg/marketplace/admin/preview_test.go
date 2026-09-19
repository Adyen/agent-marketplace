package admin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestPreviewRecordsProposalAndWorkWithoutMutatingConfiguration(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	v, _ := s.Read(ctx, "marketplace")
	existing := v.Snapshot.Registries[1]
	existing.Roots = []store.RegistryRoot{{Path: "skills"}}
	m := mutation("upsert_registry", 1)
	m.RegistryID, m.Registry, m.Reason = existing.ID, &existing, ""

	preview, err := s.Preview(ctx, "marketplace", m)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status != store.ManagedPreviewPending || !preview.Existing || preview.BaseRevision != 1 || preview.Actor != m.Actor {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	var stored Registry
	if err := json.Unmarshal(preview.Registry, &stored); err != nil || stored.Roots[0].Path != "skills" || stored.Generation != existing.Generation {
		t.Fatalf("stored registry must be the submitted proposal: %+v %v", stored, err)
	}
	job, err := s.Metadata.GetJob(ctx, preview.JobID)
	if err != nil || job.Kind != PreviewJobKind || job.MaxAttempts != 1 {
		t.Fatalf("preview job: %+v %v", job, err)
	}
	var payload PreviewJobPayload
	if json.Unmarshal(job.Payload, &payload) != nil || payload.PreviewID != preview.ID || payload.Marketplace != "marketplace" {
		t.Fatalf("payload: %s", job.Payload)
	}
	if after, _ := s.Read(ctx, "marketplace"); after.Revision != 1 || after.Snapshot.Registries[1].Roots[0].Path == "skills" {
		t.Fatal("preview must not change configuration")
	}
	loaded, err := s.GetPreview(ctx, "marketplace", preview.ID)
	if err != nil || loaded.ID != preview.ID {
		t.Fatalf("get preview: %+v %v", loaded, err)
	}

	fresh := Registry{ID: "new", Slug: "new", Origin: "https://git.example.test/new.git", Ref: "main", Roots: []store.RegistryRoot{{Path: "."}}, Acquisition: store.AcquisitionMirror}
	m = mutation("upsert_registry", 1)
	m.RegistryID, m.Registry = "new", &fresh
	created, err := s.Preview(ctx, "marketplace", m)
	if err != nil || created.Existing {
		t.Fatalf("new registry preview: %+v %v", created, err)
	}
}

func TestPreviewRejectsInvalidProposals(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	v, _ := s.Read(ctx, "marketplace")
	valid := v.Snapshot.Registries[1]
	for name, tc := range map[string]struct {
		edit func(*Mutation)
		want error
	}{
		"not upsert":     {func(m *Mutation) { m.Action = "pause"; m.Registry = nil }, ErrInvalidConfiguration},
		"stale revision": {func(m *Mutation) { m.ExpectedRevision = 7 }, store.ErrManagedRevisionConflict},
		"missing actor":  {func(m *Mutation) { m.Actor = store.ManagedActor{} }, store.ErrManagedChangeInvalid},
		"bad root":       {func(m *Mutation) { r := *m.Registry; r.Roots = []store.RegistryRoot{{Path: "../x"}}; m.Registry = &r }, ErrInvalidConfiguration},
		"origin move":    {func(m *Mutation) { r := *m.Registry; r.Origin = "https://git.example.test/moved.git"; m.Registry = &r }, ErrInvalidConfiguration},
	} {
		t.Run(name, func(t *testing.T) {
			m := mutation("upsert_registry", 1)
			r := valid
			m.RegistryID, m.Registry = r.ID, &r
			tc.edit(&m)
			if _, err := s.Preview(ctx, "marketplace", m); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
	if _, err := s.GetPreview(ctx, "marketplace", "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing preview: %v", err)
	}
}
