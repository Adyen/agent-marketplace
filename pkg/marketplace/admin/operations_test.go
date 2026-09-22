package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func managedFixture(t testing.TB) Service {
	t.Helper()
	meta, cfg, deployment := adoptionFixture(t)
	cfg.Registries = append(cfg.Registries, cfg.Registries[0])
	cfg.Registries[1].ID = "other"
	cfg.Registries[1].Slug = "other"
	s := Service{Metadata: meta, Deployment: deployment}
	_, err := s.Adopt(context.Background(), cfg, AdoptionRequest{Actor: store.ManagedActor{Issuer: "proxy", Subject: "admin"}, Reason: "adopt", RequestID: "adopt", IdempotencyKey: "adopt"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func mutation(action string, revision int64) Mutation {
	return Mutation{Action: action, RegistryID: "registry", ExpectedRevision: revision, Actor: store.ManagedActor{Issuer: "proxy", Subject: "admin"}, Reason: "reviewed", RequestID: action, IdempotencyKey: action}
}
func TestManagedLifecycleAndRetry(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	for i, action := range []string{"pause", "withdraw", "retire", "restore_retired", "restore", "resume", "reindex"} {
		req := mutation(action, int64(i+1))
		result, err := s.Apply(ctx, "marketplace", req)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		view, err := s.Read(ctx, "marketplace")
		if err != nil {
			t.Fatal(err)
		}
		if view.Revision != int64(i+2) || view.Snapshot.Registries[0].Generation != 1 {
			t.Fatalf("revision/unrelated generation: %+v", view)
		}
		r := view.Snapshot.Registries[1]
		if (action == "restore" && (!r.Paused || r.Withdrawn || r.Retired)) || (action == "restore_retired" && (!r.Withdrawn || r.Retired)) {
			t.Fatalf("unsafe lifecycle: %+v", r)
		}
		if action != "resume" && action != "reindex" && len(result.JobIDs) != 0 {
			t.Fatalf("inactive jobs: %+v", result)
		}
		if (action == "resume" || action == "reindex") && len(result.JobIDs) != 1 {
			t.Fatal("missing durable work")
		}
		retry, err := s.Apply(ctx, "marketplace", req)
		if err != nil || retry.Revision != result.Revision {
			t.Fatalf("retry: %+v %v", retry, err)
		}
		req.Reason = "changed"
		if _, err = s.Apply(ctx, "marketplace", req); !errors.Is(err, store.ErrManagedIdempotencyConflict) {
			t.Fatalf("digest mismatch: %v", err)
		}
	}
}
func TestManagedEditValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Registry)
	}{
		{"origin move", func(r *Registry) { r.Origin = "https://git.example.test/moved.git" }},
		{"nonlicense", func(r *Registry) { r.NonLicenseOverrides["hidden_content"] = "warn" }},
		{"profile path", func(r *Registry) { r.ProfileID = "/private/key" }},
		{"identity", func(r *Registry) { r.ID = "new" }},
		{"lifecycle", func(r *Registry) { r.Paused = true }},
		{"root", func(r *Registry) { r.Roots[0].Path = "../private" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := managedFixture(t)
			v, _ := s.Read(context.Background(), "marketplace")
			r := v.Snapshot.Registries[1]
			tc.edit(&r)
			m := mutation("upsert_registry", 1)
			m.Registry = &r
			if _, err := s.Apply(context.Background(), "marketplace", m); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("unsafe accepted: %v", err)
			}
			v, _ = s.Read(context.Background(), "marketplace")
			if v.Revision != 1 {
				t.Fatal("failed edit mutated state")
			}
		})
	}
}
func TestManagedCreationAndReservedAlias(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	v, _ := s.Read(ctx, "marketplace")
	r := v.Snapshot.Registries[1]
	r.Slug = "renamed"
	m := mutation("upsert_registry", 1)
	m.Registry = &r
	if _, err := s.Apply(ctx, "marketplace", m); err != nil {
		t.Fatal(err)
	}
	fresh := Registry{ID: "new", Slug: "engineering", Origin: "https://git.example.test/new.git", Ref: "main", Roots: []store.RegistryRoot{{Path: "."}}, Acquisition: store.AcquisitionMirror}
	m = mutation("upsert_registry", 2)
	m.IdempotencyKey = "new"
	m.RegistryID = "new"
	m.Registry = &fresh
	if _, err := s.Apply(ctx, "marketplace", m); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("reserved alias accepted: %v", err)
	}
	fresh.Slug = "new"
	if _, err := s.Apply(ctx, "marketplace", m); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Read(ctx, "marketplace")
	if len(v.Snapshot.Registries) != 3 || v.Snapshot.Registries[0].Generation != 1 || !v.Snapshot.Registries[0].Active() {
		t.Fatalf("new registry: %+v", v)
	}
}

func TestManagedPreconditionsAndInactivePolicy(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	m := mutation("pause", 1)
	missing := m
	missing.ExpectedRevision = 0
	if _, err := s.Apply(ctx, "marketplace", missing); !errors.Is(err, ErrRevisionRequired) {
		t.Fatalf("missing revision: %v", err)
	}
	missing = m
	missing.Actor.Subject = ""
	if _, err := s.Apply(ctx, "marketplace", missing); !errors.Is(err, store.ErrManagedChangeInvalid) {
		t.Fatalf("missing actor: %v", err)
	}
	if _, err := s.Apply(ctx, "marketplace", m); err != nil {
		t.Fatal(err)
	}
	stale := mutation("withdraw", 1)
	if _, err := s.Apply(ctx, "marketplace", stale); !errors.Is(err, store.ErrManagedRevisionConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	other := mutation("pause", 2)
	other.RegistryID = "other"
	other.IdempotencyKey = "pause-other"
	if _, err := s.Apply(ctx, "marketplace", other); err != nil {
		t.Fatal(err)
	}
	p := mutation("set_policy", 3)
	p.RegistryID = ""
	p.LicensePolicy = &LicensePolicy{Allowlist: []string{"Apache-2.0"}, Undeclared: "reject"}
	change, err := s.Apply(ctx, "marketplace", p)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.JobIDs) != 0 {
		t.Fatal("policy queued inactive registries")
	}
	view, _ := s.Read(ctx, "marketplace")
	if view.Snapshot.PolicyRevision != 2 || !view.Snapshot.Registries[0].Paused || !view.Snapshot.Registries[1].Paused {
		t.Fatal("policy changed lifecycle")
	}
	history, err := s.History(ctx, "marketplace", 0, 2)
	if err != nil || len(history) != 2 || history[0].Revision != 4 {
		t.Fatalf("bounded history: %+v %v", history, err)
	}
}

func TestManagedRejectedResourcesPolicyChangesWithoutReindex(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	global := mutation("set_policy", 1)
	global.RegistryID = ""
	global.DistributionPolicy = &DistributionPolicy{RejectedResources: config.RejectedResourcesExpose}
	global.ConfirmRejectedResources = true
	change, err := s.Apply(ctx, "marketplace", global)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.JobIDs) != 0 {
		t.Fatalf("distribution-only policy queued reindex: %+v", change.JobIDs)
	}
	view, _ := s.Read(ctx, "marketplace")
	if view.Snapshot.PolicyRevision != 2 || view.Snapshot.DistributionPolicy.RejectedResources != config.RejectedResourcesExpose {
		t.Fatalf("global distribution policy = %+v", view.Snapshot)
	}
	r := view.Snapshot.Registries[1]
	r.RejectedResources = config.RejectedResourcesBlock
	registry := mutation("upsert_registry", 2)
	registry.Registry = &r
	registry.IdempotencyKey = "registry-exposure"
	change, err = s.Apply(ctx, "marketplace", registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.JobIDs) != 0 {
		t.Fatalf("registry distribution-only policy queued reindex: %+v", change.JobIDs)
	}
	view, _ = s.Read(ctx, "marketplace")
	if view.Snapshot.PolicyRevision != 3 || view.Snapshot.Registries[1].Generation != 1 || view.Snapshot.Registries[1].RejectedResources != config.RejectedResourcesBlock {
		t.Fatalf("registry distribution policy = %+v", view.Snapshot)
	}
}

func TestManagedRenamePolicyAndDraft(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	v, _ := s.Read(ctx, "marketplace")
	old := v.Snapshot.Registries[1]
	r := old
	r.Slug = "renamed"
	r.Origin = "https://git.example.test/moved.git"
	m := mutation("upsert_registry", 1)
	m.Registry = &r
	m.ConfirmOriginMove = true
	if _, err := s.Apply(ctx, "marketplace", m); err != nil {
		t.Fatal(err)
	}
	draft, err := s.RestoreDraft(ctx, "marketplace", 1, "registry")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Registry.Slug != old.Slug || len(draft.Registry.HistoricalOrigins) != 1 || draft.ExpectedRevision != 2 {
		t.Fatalf("draft lost history: %+v", draft)
	}
	p := mutation("set_policy", 2)
	p.RegistryID = ""
	p.LicensePolicy = &LicensePolicy{Undeclared: "allow"}
	if _, err = s.Apply(ctx, "marketplace", p); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("unconfirmed unrestricted: %v", err)
	}
	p.ConfirmUnrestricted = true
	if _, err = s.Apply(ctx, "marketplace", p); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Read(ctx, "marketplace")
	if v.Snapshot.PolicyRevision != 2 || v.Snapshot.Registries[0].Generation != 1 {
		t.Fatal("policy generation invalid")
	}
}
