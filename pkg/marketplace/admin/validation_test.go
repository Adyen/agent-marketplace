package admin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type writeTrackingStore struct {
	store.MetadataStore
	store.ManagedConfigurationStore
	writes int
}

func (s *writeTrackingStore) WithTx(ctx context.Context, fn func(context.Context, store.MetadataStore) error) error {
	s.writes++
	return s.MetadataStore.WithTx(ctx, fn)
}
func (s *writeTrackingStore) EnqueueJob(ctx context.Context, job store.Job) error {
	s.writes++
	return s.MetadataStore.EnqueueJob(ctx, job)
}
func (s *writeTrackingStore) ApplyManagedConfiguration(ctx context.Context, req store.ManagedChangeRequest) (store.ManagedChange, error) {
	s.writes++
	return s.ManagedConfigurationStore.ApplyManagedConfiguration(ctx, req)
}

func TestValidateDoesNotPersist(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	tracked := &writeTrackingStore{MetadataStore: s.Metadata, ManagedConfigurationStore: s.Metadata.(store.ManagedConfigurationStore)}
	s.Metadata = tracked
	before, err := s.History(ctx, "marketplace", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.Read(ctx, "marketplace")
	if err != nil {
		t.Fatal(err)
	}
	draft := Mutation{Action: "pause", RegistryID: "registry", ExpectedRevision: 1}
	if err := s.Validate(ctx, "marketplace", draft); err != nil {
		t.Fatal(err)
	}
	prepared, _, err := s.prepare(view, draft)
	if err != nil || !prepared.Registries[1].Paused || view.Snapshot.Registries[1].Paused {
		t.Fatal("draft did not isolate lifecycle changes")
	}
	for _, action := range []string{"pause", "reindex", "upsert_registry", "set_policy"} {
		m := mutation(action, 1)
		if action == "upsert_registry" {
			r := view.Snapshot.Registries[1]
			r.Ref = "reviewed-ref"
			m.Registry = &r
		}
		if action == "set_policy" {
			m.RegistryID = ""
			m.LicensePolicy = &LicensePolicy{Allowlist: []string{"Apache-2.0"}, Undeclared: "warn"}
		}
		if err := s.Validate(ctx, "marketplace", m); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	for _, tc := range []struct {
		m    Mutation
		want error
	}{
		{mutation("pause", 0), ErrRevisionRequired},
		{mutation("pause", 2), store.ErrManagedRevisionConflict},
		{mutation("restore", 1), ErrInvalidConfiguration},
	} {
		if err := s.Validate(ctx, "marketplace", tc.m); !errors.Is(err, tc.want) {
			t.Fatalf("validation got %v want %v", err, tc.want)
		}
	}
	after, err := s.History(ctx, "marketplace", 0, 100)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("validation changed history: %v", err)
	}
	latest, err := s.Read(ctx, "marketplace")
	if err != nil || !reflect.DeepEqual(view, latest) {
		t.Fatalf("validation mutated configuration: %v", err)
	}
	m := mutation("reindex", 1)
	m.Reason = strings.Repeat("x", 1025)
	if err := s.Validate(ctx, "marketplace", m); !errors.Is(err, store.ErrManagedChangeInvalid) {
		t.Fatalf("reason bound: %v", err)
	}
	if _, err := s.Apply(ctx, "marketplace", m); !errors.Is(err, store.ErrManagedChangeInvalid) {
		t.Fatalf("apply reason bound: %v", err)
	}
	m.Reason = strings.Repeat("x", 1024)
	if err := s.Validate(ctx, "marketplace", m); err != nil {
		t.Fatal(err)
	}
	if tracked.writes != 0 {
		t.Fatal("validation attempted persistence")
	}
	change, err := s.Apply(ctx, "marketplace", m)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.JobIDs) != 1 || tracked.writes == 0 {
		t.Fatal("positive control missing job/write")
	}
	job, err := s.Metadata.GetJob(ctx, change.JobIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["registry_id"] != "registry" || payload["marketplace_id"] != "marketplace" || payload["revision"] != float64(2) {
		t.Fatalf("unscoped payload: %s", job.Payload)
	}
	policy := mutation("set_policy", 2)
	policy.RegistryID = ""
	policy.LicensePolicy = &LicensePolicy{Allowlist: []string{"Apache-2.0"}, Undeclared: "reject"}
	change, err = s.Apply(ctx, "marketplace", policy)
	if err != nil || len(change.JobIDs) != 1 {
		t.Fatalf("policy work: %+v %v", change, err)
	}
	job, err = s.Metadata.GetJob(ctx, change.JobIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	payload = nil
	if err = json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if _, present := payload["registry_id"]; present {
		t.Fatalf("policy work restricted: %s", job.Payload)
	}
}
