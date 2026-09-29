package codemode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestBuildSnapshotCrossOriginRefCollision(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	local := filepath.Join(base, "tools")
	writeSnapshotSkill(t, local, "x", "local skill body")
	writeSnapshotSkill(t, local, "y", "local sibling body")
	if err := os.WriteFile(filepath.Join(local, ".mcp.json"), []byte(`{"mcpServers":{"srv":{"command":"npx"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	builder := &SnapshotBuilder{
		CatalogMode: catalog.LoadModeCached,
		Catalog: func(context.Context, config.EffectiveConfig, catalog.LoadMode) ([]marketplace.CatalogOutcome, error) {
			return []marketplace.CatalogOutcome{{
				Endpoint: config.MarketplaceEntry{Alias: "local", URL: "https://mp.example"},
				Complete: true,
				Resources: []mpapi.CatalogResource{
					{RegistryID: "tools-id", RegistrySlug: "tools", Kind: "skill", Name: "x", Revision: "r1"},
					{RegistryID: "tools-id", RegistrySlug: "tools", Kind: "mcpserver", Name: "srv", Revision: "r1"},
					{RegistryID: "tools-id", RegistrySlug: "tools", Kind: "skill", Name: "mp-only", Revision: "r1"},
				},
			}}, nil
		},
	}
	ec := config.EffectiveConfig{Merged: &config.Config{
		Registries:   []config.RegistryEntry{{URL: local}},
		Marketplaces: []config.MarketplaceEntry{{Alias: "local", URL: "https://mp.example"}},
	}}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)

	for _, ref := range []string{"local:tools/skill/x", "local:tools/mcp/srv"} {
		var matches int
		for i := range snap.Resources {
			if snap.Resources[i].Ref == ref {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("ref %q appears %d times in %+v", ref, matches, snap.Resources)
		}
		found := snap.Find(ref)
		if found == nil {
			t.Fatalf("Find(%q) returned nil for %+v", ref, snap.Resources)
		}
		if found.MarketplaceOrigin() != nil {
			t.Fatalf("Find(%q) resolved to the Marketplace origin, want the configured registry", ref)
		}
		var collisionErr bool
		for _, err := range errs {
			if strings.Contains(err.Error(), ref) {
				collisionErr = true
			}
		}
		if !collisionErr {
			t.Fatalf("BuildSnapshot errs = %v, want a diagnostic naming %q", errs, ref)
		}
	}

	sc, err := NewFileSkillLoader(true).Load(context.Background(), snap.Find("local:tools/skill/x"))
	if err != nil || !strings.Contains(sc.Content, "local skill body") {
		t.Fatalf("skills.load for colliding ref = %v content %+v, want local registry body", err, sc)
	}
	if snap.Find("local:tools/skill/y") == nil {
		t.Fatalf("unaffected registry sibling missing: %+v", snap.Resources)
	}
	if snap.Find("local:tools/skill/mp-only") == nil {
		t.Fatalf("unaffected Marketplace sibling missing: %+v", snap.Resources)
	}
}
