package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func managedFixture(t testing.TB) (Deps, admin.Service) {
	t.Helper()
	meta, blobs := openTestStores(t)
	t.Cleanup(func() { _ = meta.Close() })
	cfg := createTestConfig(t, "unused")
	cfg.Management = config.Management{Mode: "database", AllowedOrigins: []string{"https://git.example"}}
	cfg.Policy.LicenseUndeclared = "reject"
	cfg.Registries[0].Origin = "https://git.example/repository"
	svc := admin.Service{Metadata: meta, Deployment: cfg.Management}
	_, err := svc.Adopt(context.Background(), cfg, admin.AdoptionRequest{Actor: store.ManagedActor{Issuer: "test", Subject: "admin"}, Reason: "adopt", RequestID: "request", IdempotencyKey: "adopt"})
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}, svc
}

func TestManagedFanoutUsesCurrentActiveGeneration(t *testing.T) {
	ctx := context.Background()
	deps, svc := managedFixture(t)
	parent, err := deps.MetadataStore.LeaseJob(ctx, "managed-reconcile", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedReconcile(ctx, deps, parent, "worker"); err != nil {
		t.Fatal(err)
	}
	child, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "child", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var payload managedRefreshPayload
	if err := json.Unmarshal(child.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RegistryGeneration != 1 || payload.PolicyRevision != 1 || payload.ParentJobID != parent.ID || payload.Revision != 1 {
		t.Fatalf("payload: %+v", payload)
	}
	_, err = svc.Apply(ctx, "test-mp", admin.Mutation{Action: "pause", RegistryID: "src-test", ExpectedRevision: 1, Actor: store.ManagedActor{Issuer: "test", Subject: "admin"}, Reason: "pause", RequestID: "pause", IdempotencyKey: "pause"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := managedPreflight(ctx, deps, child, "child", payload); !errors.Is(err, ErrManagedSuperseded) {
		t.Fatalf("stale preflight: %v", err)
	}
	if err := scheduleManaged(ctx, deps, "bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "other", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("paused scheduled: %v", err)
	}
}

func TestManagedPreflightRejectsLostLease(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	if err := scheduleManaged(ctx, deps, "bucket"); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "old", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var p managedRefreshPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if _, _, err := managedPreflight(ctx, deps, j, "old", p); err != nil {
		t.Fatal(err)
	}
	if err := deps.MetadataStore.ReleaseJob(ctx, j.ID, "old"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := managedPreflight(ctx, deps, j, "old", p); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("lost lease: %v", err)
	}
}

func TestManagedInputsPreserveDeploymentLimitsAndOverrideLicense(t *testing.T) {
	deps, _ := managedFixture(t)
	view, err := managedView(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	view.Snapshot.LicensePolicy.Allowlist = []string{"BSD-3-Clause"}
	r := view.Snapshot.Registries[0]
	r.NonLicenseOverrides = map[string]any{"hidden_content": "warn"}
	r.LicenseOverride = &admin.LicensePolicy{Allowlist: []string{"Apache-2.0"}, Undeclared: "allow"}
	cfg, registry := managedInputs(deps, view, r)
	if cfg.Policy.Limits != deps.Config.Policy.Limits || len(cfg.Policy.LicenseAllowlist) != 1 || cfg.Policy.LicenseAllowlist[0] != "BSD-3-Clause" {
		t.Fatalf("deployment/managed policy: %+v", cfg.Policy)
	}
	values, ok := registry.PolicyOverrides["license_allowlist"].([]any)
	if !ok || len(values) != 1 || values[0] != "Apache-2.0" || registry.PolicyOverrides["license_undeclared"] != "allow" || registry.PolicyOverrides["hidden_content"] != "warn" {
		t.Fatalf("registry overrides: %+v", registry.PolicyOverrides)
	}
	registry.PolicyOverrides["hidden_content"] = "reject"
	if r.NonLicenseOverrides["hidden_content"] != "warn" {
		t.Fatal("mutated snapshot policy")
	}
}

func TestManagedProfileOriginBinding(t *testing.T) {
	deps, _ := managedFixture(t)
	t.Setenv("MANAGED_TEST_TOKEN", "secret")
	deps.Config.Management.Profiles = map[string]config.CredentialProfile{"private": {AllowedOrigins: []string{"https://git.example"}, Credentials: config.Credentials{Token: &config.TokenCredential{Env: "MANAGED_TEST_TOKEN"}}}}
	r := admin.Registry{Origin: "https://git.example/repository", ProfileID: "private"}
	creds, err := managedCredentials(deps.Config.Management, r)
	if err != nil || creds.Token != "secret" {
		t.Fatalf("allowed profile: %v", err)
	}
	r.Origin = "https://evil.example/repository"
	if _, err := managedCredentials(deps.Config.Management, r); !errors.Is(err, ErrManagedProfile) {
		t.Fatalf("origin binding: %v", err)
	}
	r.Origin = "file:///tmp/repository"
	if _, err := managedCredentials(deps.Config.Management, r); !errors.Is(err, ErrManagedProfile) {
		t.Fatalf("local origin: %v", err)
	}
}
