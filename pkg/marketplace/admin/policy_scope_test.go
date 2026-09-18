package admin

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryLicenseChangesPreserveGlobalPolicyGeneration(t *testing.T) {
	for _, kind := range []string{"override", "default license"} {
		t.Run(kind, func(t *testing.T) {
			s := managedFixture(t)
			ctx := context.Background()
			view, err := s.Read(ctx, "marketplace")
			if err != nil {
				t.Fatal(err)
			}
			r := view.Snapshot.Registries[1]
			if kind == "override" {
				r.LicenseOverride = &LicensePolicy{Allowlist: []string{"Apache-2.0"}, Undeclared: "warn"}
			} else {
				r.License = "MIT"
			}
			m := mutation("upsert_registry", 1)
			m.Registry = &r
			prepared, work, err := s.prepare(view, m)
			if err != nil {
				t.Fatal(err)
			}
			if !work || prepared.PolicyRevision != 1 || prepared.Registries[0].Generation != 1 || prepared.Registries[1].Generation != 2 {
				t.Fatalf("registry edit changed global/unrelated generation: %+v", prepared)
			}
			accepted, err := s.Apply(ctx, "marketplace", m)
			if err != nil {
				t.Fatal(err)
			}
			latest, err := s.Read(ctx, "marketplace")
			if err != nil {
				t.Fatal(err)
			}
			if latest.Snapshot.PolicyRevision != 1 || latest.Snapshot.Registries[0].Generation != 1 || latest.Snapshot.Registries[1].Generation != 2 {
				t.Fatalf("persisted generations: %+v", latest)
			}
			if len(accepted.JobIDs) != 1 {
				t.Fatalf("work intents: %+v", accepted.JobIDs)
			}
			job, err := s.Metadata.GetJob(ctx, accepted.JobIDs[0])
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["registry_id"] != "registry" || payload["revision"] != float64(2) {
				t.Fatalf("registry scope: %s", job.Payload)
			}
			policy := mutation("set_policy", 2)
			policy.RegistryID = ""
			policy.LicensePolicy = &LicensePolicy{Allowlist: []string{"MIT", "Apache-2.0"}, Undeclared: "warn"}
			if _, err := s.Apply(ctx, "marketplace", policy); err != nil {
				t.Fatal(err)
			}
			latest, err = s.Read(ctx, "marketplace")
			if err != nil {
				t.Fatal(err)
			}
			if latest.Snapshot.PolicyRevision != 2 || latest.Snapshot.Registries[0].Generation != 1 || latest.Snapshot.Registries[1].Generation != 2 {
				t.Fatalf("global edit generations: %+v", latest)
			}
		})
	}
}
