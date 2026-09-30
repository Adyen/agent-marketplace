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

	if len(errs) != 0 {
		t.Fatalf("BuildSnapshot errors: %v", errs)
	}
	for _, ref := range []string{"local:tools/skill/x", "local:tools/mcp/srv"} {
		found := snap.Find(ref)
		if found == nil {
			t.Fatalf("Find(%q) returned nil for %+v", ref, snap.Resources)
		}
		if found.MarketplaceOrigin() != nil {
			t.Fatalf("Find(%q) resolved to the Marketplace origin, want the configured registry", ref)
		}
	}
	for _, ref := range []string{"marketplace:tools/skill/x", "marketplace:tools/mcp/srv", "marketplace:tools/skill/mp-only"} {
		found := snap.Find(ref)
		if found == nil {
			t.Fatalf("Find(%q) returned nil for %+v", ref, snap.Resources)
		}
		if found.MarketplaceOrigin() == nil {
			t.Fatalf("Find(%q) lost its Marketplace origin", ref)
		}
		if found.Registry != "marketplace:tools" || !strings.HasPrefix(found.Ref, found.Registry+"/") {
			t.Fatalf("Marketplace ref %q does not begin with its registry %q", found.Ref, found.Registry)
		}
	}

	sc, err := NewFileSkillLoader(true).Load(context.Background(), snap.Find("local:tools/skill/x"))
	if err != nil || !strings.Contains(sc.Content, "local skill body") {
		t.Fatalf("skills.load for colliding ref = %v content %+v, want local registry body", err, sc)
	}
	if snap.Find("local:tools/skill/y") == nil {
		t.Fatalf("unaffected registry sibling missing: %+v", snap.Resources)
	}
}

// spec: code-mode-mcp/named-sources-label-refs-by-source-name
func TestBuildSnapshotLabelsNamedSourcesBySourceName(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	dirA := filepath.Join(base, "alpha-dir")
	dirB := filepath.Join(base, "beta-dir")
	writeLocalMCPServer(t, dirA, "fake", "echo", "aaa")
	writeLocalMCPServer(t, dirB, "fake", "echo", "bbb")

	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: dirA, SourceName: "alpha", V4Kind: config.V4SourceDirectory},
		{URL: dirB, SourceName: "beta", V4Kind: config.V4SourceDirectory},
	}}}
	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("BuildSnapshot errors: %v", errs)
	}
	for ref, dir := range map[string]string{
		"local:alpha/mcp/fake": dirA,
		"local:beta/mcp/fake":  dirB,
	} {
		found := snap.Find(ref)
		if found == nil {
			t.Fatalf("Find(%q) returned nil for %+v", ref, snap.Resources)
		}
		if found.Registry+"/mcp/fake" != ref {
			t.Fatalf("ref %q does not begin with registry %q", ref, found.Registry)
		}
		if found.Origin().ContentDir != dir {
			t.Fatalf("ref %q routed to %s", ref, found.Origin().ContentDir)
		}
	}
}

// spec: code-mode-mcp/named-sources-label-refs-by-source-name
func TestBuildSnapshotLabelsNamedGitSourceBySourceName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	base := t.TempDir()
	origin := filepath.Join(base, "reg.git")
	work := filepath.Join(base, "work")
	snapshotGit(t, "", "init", "--bare", "--initial-branch=main", origin)
	snapshotGit(t, "", "init", "--initial-branch=main", work)
	snapshotGit(t, work, "remote", "add", "origin", origin)
	writeLocalMCPServer(t, work, "fake", "echo", "git")
	snapshotGitCommitPush(t, work, "a")

	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: "file://" + filepath.ToSlash(origin), Ref: "main", SourceName: "team-git", V4Kind: config.V4SourceGit},
	}}}
	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("BuildSnapshot errors: %v", errs)
	}
	found := snap.Find("team-git/mcp/fake")
	if found == nil {
		t.Fatalf("named git source ref missing: %+v", snap.Resources)
	}
	if found.Registry != "team-git" {
		t.Fatalf("registry = %q, want the source name", found.Registry)
	}
}

// spec: code-mode-mcp/colliding-source-labels-disambiguated
func TestBuildSnapshotDisambiguatesCollidingSourceNames(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	dirA := filepath.Join(base, "a")
	dirB := filepath.Join(base, "b")
	writeLocalMCPServer(t, dirA, "fake", "echo", "aaa")
	writeLocalMCPServer(t, dirB, "fake", "echo", "bbb")

	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: dirA, SourceName: "tools", V4Kind: config.V4SourceDirectory},
		{URL: dirB, SourceName: "tools", V4Kind: config.V4SourceDirectory},
	}}}
	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	build := func() map[string]string {
		snap, errs := builder.BuildSnapshot(context.Background(), ec)
		if len(errs) > 0 {
			t.Fatalf("BuildSnapshot errors: %v", errs)
		}
		out := map[string]string{}
		for i := range snap.Resources {
			res := &snap.Resources[i]
			out[res.Ref] = res.Origin().ContentDir
		}
		return out
	}
	first := build()
	if len(first) != 2 {
		t.Fatalf("expected 2 disambiguated refs, got %+v", first)
	}
	for ref := range first {
		if !strings.HasPrefix(ref, "local:tools-") {
			t.Fatalf("ref %q lacks the collision suffix on the shared label", ref)
		}
	}
	second := build()
	for ref, dir := range first {
		if second[ref] != dir {
			t.Fatalf("ref %q moved from %s to %s across builds", ref, dir, second[ref])
		}
	}
}
