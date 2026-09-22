package admin

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func adoptionFixture(t testing.TB) (*sqlite.Store, config.Config, Deployment) {
	t.Helper()
	meta := sqlite.New(filepath.Join(t.TempDir(), "metadata.db"))
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	cfg := config.Config{
		Instance: config.Instance{ID: "marketplace", Name: "Marketplace", CanonicalURL: "https://marketplace.test"},
		Policy:   config.Policy{LicenseAllowlist: []string{"MIT"}, LicenseUndeclared: "reject"},
		Registries: []config.Registry{{ID: "registry", Slug: "engineering", Origin: "https://git.example.test/team/skills.git", Ref: "main", Acquisition: "mirror",
			Roots: []config.RegistryRoot{{Path: ".agents"}}, Credentials: config.Credentials{Token: &config.TokenCredential{Env: "PRIVATE_TOKEN_ENV"}},
			PolicyOverrides: map[string]any{"hidden_content": "reject"},
		}},
	}
	deployment := Deployment{AllowedOrigins: []string{"https://git.example.test"}, Profiles: map[string]Profile{
		"engineering-readonly": {AllowedOrigins: []string{"https://git.example.test"}, Credentials: cfg.Registries[0].Credentials},
	}}
	return meta, cfg, deployment
}

func TestAdoptionPreservesIdentityAndStoresOnlyProfiles(t *testing.T) {
	ctx := context.Background()
	meta, cfg, deployment := adoptionFixture(t)
	cfg.Policy.RejectedResources = config.RejectedResourcesExpose
	cfg.Registries[0].PolicyOverrides["rejected_resources"] = config.RejectedResourcesBlock
	existing := store.Registry{ID: "registry", Slug: "old-slug", Aliases: []string{"older-slug"}, Origins: []string{"https://git.example.test/old.git"}, Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}
	if err := meta.PutRegistry(ctx, existing); err != nil {
		t.Fatal(err)
	}
	service := Service{Metadata: meta, Deployment: deployment}
	request := AdoptionRequest{Actor: store.ManagedActor{Issuer: "test-proxy", Subject: "operator"}, Reason: "Adopt approved settings", RequestID: "adoption-request", IdempotencyKey: "adoption-key"}
	accepted, err := service.Adopt(ctx, cfg, request)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(accepted.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Registries) != 1 {
		t.Fatalf("registries: %+v", snapshot.Registries)
	}
	r := snapshot.Registries[0]
	if r.ID != "registry" || r.ProfileID != "engineering-readonly" || r.Generation != 1 || r.Lifecycle != "active" || r.NonLicenseOverrides["hidden_content"] != "reject" {
		t.Fatalf("lost managed settings: %+v", r)
	}
	if snapshot.DistributionPolicy.RejectedResources != config.RejectedResourcesExpose || r.RejectedResources != config.RejectedResourcesBlock {
		t.Fatalf("lost rejected-resource policy: snapshot=%+v registry=%+v", snapshot.DistributionPolicy, r)
	}
	if _, leaked := r.NonLicenseOverrides["rejected_resources"]; leaked {
		t.Fatal("typed rejected-resource policy leaked into non-license overrides")
	}
	if strings.Join(r.Aliases, ",") != "old-slug,older-slug" || strings.Join(r.HistoricalOrigins, ",") != "https://git.example.test/old.git" {
		t.Fatalf("lost identity history: %+v", r)
	}
	encoded, err := json.Marshal(accepted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "PRIVATE_TOKEN_ENV") || strings.Contains(string(encoded), "credentials") {
		t.Fatalf("adoption exposed a deployment credential reference")
	}
	if len(accepted.JobIDs) != 1 {
		t.Fatalf("adoption lost durable reconciliation: %+v", accepted.JobIDs)
	}
	if _, err := meta.GetJob(ctx, accepted.JobIDs[0]); err != nil {
		t.Fatal(err)
	}
	retry, err := service.Adopt(ctx, cfg, request)
	if err != nil || retry.Revision != accepted.Revision {
		t.Fatalf("adoption retry: %+v, %v", retry, err)
	}
	cfg.Registries[0].Ref = "different-ref"
	request.IdempotencyKey = "new-adoption"
	if _, err := service.Adopt(ctx, cfg, request); !errors.Is(err, store.ErrManagedRevisionConflict) {
		t.Fatalf("repeat adoption must not replace database state: %v", err)
	}
}

func TestAdoptionRejectsUnapprovedOriginsAndProfiles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*config.Config, *Deployment)
	}{
		{"missing profile", func(_ *config.Config, d *Deployment) { d.Profiles = nil }},
		{"profile host mismatch", func(_ *config.Config, d *Deployment) {
			p := d.Profiles["engineering-readonly"]
			p.AllowedOrigins = []string{"https://different.test"}
			d.Profiles["engineering-readonly"] = p
		}},
		{"local repository", func(c *config.Config, _ *Deployment) { c.Registries[0].Origin = "/private/repository" }},
		{"file URL", func(c *config.Config, _ *Deployment) { c.Registries[0].Origin = "file:///private/repository" }},
		{"secret URL", func(c *config.Config, _ *Deployment) {
			c.Registries[0].Origin = "https://secret@git.example.test/team.git"
		}},
		{"host suffix", func(c *config.Config, _ *Deployment) {
			c.Registries[0].Origin = "https://git.example.test.attacker.test/team.git"
		}},
		{"root traversal", func(c *config.Config, _ *Deployment) { c.Registries[0].Roots[0].Path = "../private" }},
		{"ambiguous license override", func(c *config.Config, _ *Deployment) {
			c.Registries[0].PolicyOverrides["license_allowlist"] = []string{}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta, cfg, deployment := adoptionFixture(t)
			tc.mutate(&cfg, &deployment)
			service := Service{Metadata: meta, Deployment: deployment}
			_, err := service.Adopt(context.Background(), cfg, AdoptionRequest{Actor: store.ManagedActor{Issuer: "proxy", Subject: "admin"}, Reason: "Adopt", RequestID: "request", IdempotencyKey: "key"})
			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("unsafe adoption accepted: %v", err)
			}
			if _, err := meta.CurrentManagedConfiguration(context.Background(), "marketplace"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("invalid adoption changed authority: %v", err)
			}
		})
	}
}
