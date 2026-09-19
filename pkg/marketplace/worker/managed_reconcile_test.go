package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestManagedReconcileOriginalApplicability(t *testing.T) {
	for _, scenario := range []string{"unchanged", "unrelated-create", "edit", "pause-resume", "policy"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			deps, svc := managedFixture(t)
			original, err := deps.MetadataStore.LeaseJob(ctx, "managed-reconcile", "old", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			view, err := managedView(ctx, deps)
			if err != nil {
				t.Fatal(err)
			}
			r := view.Snapshot.Registries[0]
			mutate := func(m admin.Mutation) {
				m.Actor = store.ManagedActor{Issuer: "test", Subject: "operator"}
				m.Reason, m.RequestID, m.IdempotencyKey = m.Action, m.Action, m.Action
				if _, err := svc.Apply(ctx, "test-mp", m); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "unrelated-create":
				r.ID, r.Slug, r.Generation = "other", "other", 0
				mutate(admin.Mutation{Action: "upsert_registry", RegistryID: r.ID, Registry: &r, ExpectedRevision: 1})
			case "edit":
				r.Ref = "new-branch"
				mutate(admin.Mutation{Action: "upsert_registry", RegistryID: r.ID, Registry: &r, ExpectedRevision: 1})
			case "pause-resume":
				mutate(admin.Mutation{Action: "pause", RegistryID: r.ID, ExpectedRevision: 1})
				mutate(admin.Mutation{Action: "resume", RegistryID: r.ID, ExpectedRevision: 2})
			case "policy":
				mutate(admin.Mutation{Action: "set_policy", ExpectedRevision: 1, LicensePolicy: &admin.LicensePolicy{Allowlist: []string{"Apache-2.0"}, Undeclared: "reject"}})
			}
			if err := processManagedReconcile(ctx, deps, original, "old"); err != nil {
				t.Fatal(err)
			}
			child, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "child", time.Minute)
			if scenario == "unchanged" || scenario == "unrelated-create" {
				if err != nil {
					t.Fatal(err)
				}
				var p managedRefreshPayload
				if err := json.Unmarshal(child.Payload, &p); err != nil {
					t.Fatal(err)
				}
				if p.RegistryID != "src-test" || p.RegistryGeneration != 1 || p.Revision != 1 {
					t.Fatalf("wrong child: %+v", p)
				}
				if _, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "extra", time.Minute); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("old broad intent included new registry: %v", err)
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("superseded intent cloned current config: %+v %v", child, err)
			}
			if scenario != "unchanged" {
				for {
					replacement, err := deps.MetadataStore.LeaseJob(ctx, "managed-reconcile", "new", time.Minute)
					if errors.Is(err, store.ErrNotFound) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := processManagedReconcile(ctx, deps, replacement, "new"); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "replacement", time.Minute); err != nil {
					t.Fatalf("replacement intent lost: %v", err)
				}
			}
		})
	}
}

func TestManagedAcquisitionCanceledBySupersession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deps, svc := managedFixture(t)
	if err := scheduleManaged(ctx, deps, "cancel"); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = processManagedRefresh(ctx, deps, j, "worker", func(workCtx context.Context, _ string, _ string, _ acquire.Credentials, _ acquire.Limits, _ time.Duration, _ ...string) (*acquire.Result, error) {
		_, err := svc.Apply(ctx, "test-mp", admin.Mutation{Action: "pause", RegistryID: "src-test", ExpectedRevision: 1, Actor: store.ManagedActor{Issuer: "test", Subject: "operator"}, Reason: "pause", RequestID: "pause", IdempotencyKey: "pause"})
		if err != nil {
			t.Fatal(err)
		}
		<-workCtx.Done()
		return nil, workCtx.Err()
	})
	if !errors.Is(err, ErrManagedSuperseded) {
		t.Fatalf("supersession reported as generic failure: %v", err)
	}
	events, err := deps.MetadataStore.(store.ManagedRefreshAttemptStore).ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: j.ID, Limit: 10})
	if err != nil || len(events) != 2 || events[0].Outcome != "superseded" {
		t.Fatalf("wrong cancellation outcome: %+v %v", events, err)
	}
}

func TestManagedBroadPolicyIntentDoesNotResumePausedMembers(t *testing.T) {
	ctx := context.Background()
	deps, svc := managedFixture(t)
	apply := func(m admin.Mutation) store.ManagedChange {
		view, err := managedView(ctx, deps)
		if err != nil {
			t.Fatal(err)
		}
		m.ExpectedRevision = view.Revision
		m.Actor = store.ManagedActor{Issuer: "test", Subject: "operator"}
		m.Reason, m.RequestID, m.IdempotencyKey = m.Action, m.Action, m.Action
		change, err := svc.Apply(ctx, "test-mp", m)
		if err != nil {
			t.Fatal(err)
		}
		return change
	}
	view, err := managedView(ctx, deps)
	if err != nil {
		t.Fatal(err)
	}
	other := view.Snapshot.Registries[0]
	other.ID, other.Slug, other.Generation = "other", "other", 0
	apply(admin.Mutation{Action: "upsert_registry", RegistryID: other.ID, Registry: &other})
	apply(admin.Mutation{Action: "pause", RegistryID: other.ID})
	policy := apply(admin.Mutation{Action: "set_policy", LicensePolicy: &admin.LicensePolicy{Allowlist: []string{"Apache-2.0"}, Undeclared: "reject"}})
	resumed := apply(admin.Mutation{Action: "resume", RegistryID: other.ID})
	parent, err := deps.MetadataStore.LeaseJobByID(ctx, policy.JobIDs[0], "policy", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedReconcile(ctx, deps, parent, "policy"); err != nil {
		t.Fatal(err)
	}
	child, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "child", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var p managedRefreshPayload
	if err := json.Unmarshal(child.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.RegistryID != "src-test" || p.PolicyRevision != 2 || p.ParentJobID != parent.ID {
		t.Fatalf("lost applicable active policy member: %+v", p)
	}
	if _, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "extra", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("policy intent indexed originally paused member: %v", err)
	}
	resumeJob, err := deps.MetadataStore.LeaseJobByID(ctx, resumed.JobIDs[0], "resume", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedReconcile(ctx, deps, resumeJob, "resume"); err != nil {
		t.Fatal(err)
	}
	replacement, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "replacement", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replacement.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.RegistryID != other.ID || p.ParentJobID != resumeJob.ID {
		t.Fatalf("resume replacement missing: %+v", p)
	}
}

func TestManagedReconcileFailureBackoff(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	if err := deps.MetadataStore.EnqueueJob(ctx, store.Job{ID: "malformed", Kind: "managed-reconcile", Payload: []byte(`{}`), MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJobByID(ctx, "malformed", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	if err := processManagedReconcile(ctx, deps, j, "worker"); !errors.Is(err, store.ErrManagedStateCorrupt) {
		t.Fatal(err)
	}
	failed, err := deps.MetadataStore.GetJob(ctx, j.ID)
	if err != nil || failed.Backoff < time.Second || failed.LeaseUntil.Before(before.Add(time.Second)) {
		t.Fatalf("unbounded immediate retry: %+v %v", failed, err)
	}
	if _, err := deps.MetadataStore.LeaseJobByID(ctx, j.ID, "retry", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retry spun: %v", err)
	}
}
