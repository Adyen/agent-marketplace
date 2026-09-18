package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestReindexTargetsUseManagedAuthority(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	ctx := context.Background()
	meta := sqlite.New(filepath.Join(t.TempDir(), "metadata.db"))
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	cfg := mpconfig.Config{Instance: mpconfig.Instance{ID: "market", Name: "Market", CanonicalURL: "https://market.example"}, Management: mpconfig.Management{Mode: "database", AllowedOrigins: []string{"https://git.example"}}, Policy: mpconfig.Policy{LicenseUndeclared: "reject"}, Registries: []mpconfig.Registry{{ID: "registry", Slug: "current", Origin: "https://git.example/repo", Ref: "main", Acquisition: "mirror", Roots: []mpconfig.RegistryRoot{{Path: ".agents"}}}}}
	service := admin.Service{Metadata: meta, Deployment: cfg.Management}
	actor := store.ManagedActor{Issuer: "test", Subject: "operator"}
	if _, err := service.Adopt(ctx, cfg, admin.AdoptionRequest{Actor: actor, Reason: "adopt", RequestID: "adopt", IdempotencyKey: "adopt"}); err != nil {
		t.Fatal(err)
	}
	cfg.Registries = []mpconfig.Registry{{ID: "stale", Slug: "stale"}}
	for _, name := range []string{"", "current", "registry"} {
		targets, err := reindexTargets(ctx, meta, cfg, name)
		if err != nil || !reflect.DeepEqual(targets, []string{"registry"}) {
			t.Fatalf("targets(%q)=%v %v", name, targets, err)
		}
	}
	if _, err := reindexTargets(ctx, meta, cfg, "stale"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale YAML selected: %v", err)
	}
	if _, err := service.Apply(ctx, "market", admin.Mutation{Action: "pause", RegistryID: "registry", ExpectedRevision: 1, Actor: actor, Reason: "pause", RequestID: "pause", IdempotencyKey: "pause"}); err != nil {
		t.Fatal(err)
	}
	targets, err := reindexTargets(ctx, meta, cfg, "")
	if err != nil || len(targets) != 0 {
		t.Fatalf("paused registry selected: %v %v", targets, err)
	}
	targets, err = reindexTargets(ctx, meta, cfg, "current")
	if err != nil || !reflect.DeepEqual(targets, []string{"registry"}) {
		t.Fatalf("explicit target must reach worker lifecycle validation: %v %v", targets, err)
	}
	cfg.Management.Mode = "file"
	targets, err = reindexTargets(ctx, meta, cfg, "")
	if err != nil || !reflect.DeepEqual(targets, []string{"stale"}) {
		t.Fatalf("file targets: %v %v", targets, err)
	}
}
