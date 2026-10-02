package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// devinUnsupportedTransportSyncResult syncs a registry whose only MCP server
// requests a transport outside Devin's documented http/sse vocabulary, which the
// Devin compiler skips with mcp.devin.transport-unsupported.
//
// WHY THIS CODE rather than the scoped allowed-tools fixture this test
// originally used: bead agent-marketplace-pu6b moved skill.devin.tool-unknown
// from error to WARNING (the key is omitted and the artifact installs), so that
// fixture no longer produces a materialization error at all and the test went
// vacuous on merge. pu6b deliberately did NOT reverse the codes that REMOVE a
// desired aggregate entry, and this is one of them; its severity is
// independently pinned by TestSyncExitCodeForSkippedDevinAggregateEntries in
// cmd/agent-marketplace, so a future downgrade fails loudly there first.
func devinUnsupportedTransportSyncResult(t *testing.T) SyncResult {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	registry := filepath.Join(t.TempDir(), "registry")
	writeFile(t, filepath.Join(registry, ".mcp.json"), `{
  "mcpServers": {
    "alpha": {"url": "https://example.invalid/mcp", "transport": "websocket"}
  }
}
`)

	repoCfg := &config.Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []config.RegistryEntry{{
			URL:           registry,
			SourceHarness: "claude",
			MCP:           config.KindSelector{Include: []string{"alpha"}},
		}},
	}

	return Sync(context.Background(), ResolveOptions{
		Workspace: mustWorkspace(t, t.TempDir()),
		NoBackup:  true,
		Loaders: Loaders{
			Org:    func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"devin"}}, nil },
			Repo:   func() (*config.Config, error) { return repoCfg, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	})
}

// TestSyncSeparatesMaterializeErrorsFromCatalogTrust is the pipeline half of
// bead agent-marketplace-tbjf. A downstream compile failure says NOTHING about
// whether the catalog could be read, so it must land in MaterializeErrors and
// leave the catalog trusted — otherwise every caller that wants to know "was my
// view of the registries reliable?" has to string-match a flat slice.
func TestSyncSeparatesMaterializeErrorsFromCatalogTrust(t *testing.T) {
	result := devinUnsupportedTransportSyncResult(t)

	if len(result.Errors) == 0 {
		t.Fatalf("expected the unsupported-transport fixture to fail materialization")
	}
	if !result.CatalogTrusted() {
		t.Errorf("a compile failure made the catalog untrusted; trust errors: %v", result.CatalogTrustErrors)
	}
	if len(result.CatalogTrustErrors) != 0 {
		t.Errorf("CatalogTrustErrors = %v, want none for a materialization-only failure", result.CatalogTrustErrors)
	}
	if len(result.MaterializeErrors) != len(result.Errors) {
		t.Errorf("MaterializeErrors = %d, Errors = %d; every failure here is post-resolve", len(result.MaterializeErrors), len(result.Errors))
	}
}

// TestSyncCountsCatalogLoadFailureAsUntrusted pins the other direction: a
// registry that cannot be loaded means an absent resource may merely be
// unreachable, so the catalog is NOT trustworthy and nothing downstream ran.
func TestSyncCountsCatalogLoadFailureAsUntrusted(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	repoCfg := &config.Config{
		APIVersion: "agent-marketplace/v2",
		Registries: []config.RegistryEntry{{
			URL:    filepath.Join(t.TempDir(), "missing.git"),
			Ref:    "main",
			Skills: config.KindSelector{Include: []string{"vm-lifecycle"}},
		}},
	}

	result := Sync(context.Background(), ResolveOptions{
		Workspace: mustWorkspace(t, t.TempDir()),
		NoBackup:  true,
		Loaders: Loaders{
			Org:    func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"factory"}}, nil },
			Repo:   func() (*config.Config, error) { return repoCfg, nil },
			Local:  func() (*config.LocalConfig, error) { return nil, nil },
		},
	})

	if len(result.Errors) == 0 {
		t.Fatalf("expected an unreachable registry to fail the sync")
	}
	if result.CatalogTrusted() {
		t.Errorf("an unloadable registry must leave the catalog untrusted; errors: %v", result.Errors)
	}
	if len(result.CatalogTrustErrors) != len(result.Errors) {
		t.Errorf("CatalogTrustErrors = %d, Errors = %d; a catalog-load failure is trust-relevant", len(result.CatalogTrustErrors), len(result.Errors))
	}
	if len(result.MaterializeErrors) != 0 {
		t.Errorf("MaterializeErrors = %v, want none: materialization never ran", result.MaterializeErrors)
	}
}
