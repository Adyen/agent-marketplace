package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestStartupDoesNotAutoRefreshMarketplace(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{SourceID: "src", Kind: "skill", Name: "review"}}}
	cfg := &config.Config{APIVersion: config.APIVersionV2, Marketplaces: []config.MarketplaceEntry{entry}}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.Marketplace{{URL: server.URL, InstanceID: "mp", Resources: []lockfile.MarketplaceResource{{SourceID: "src", Kind: "skill", Name: "review", Revision: lockfile.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), Acquisition: lockfile.AcquisitionMirror, ArchiveDigest: lockfile.Digest("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), Direct: true}}}}}
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	oldOrg, oldGlobal, oldRepo, oldLocal := loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig
	loadOrgConfig = func() (*config.Config, error) { return nil, nil }
	loadGlobalConfig = func() (*config.Config, error) { return nil, nil }
	loadRepoConfig = func() (*config.Config, error) { return cfg, nil }
	loadLocalConfig = func() (*config.LocalConfig, error) { return nil, nil }
	t.Cleanup(func() {
		loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig = oldOrg, oldGlobal, oldRepo, oldLocal
	})

	snapshot := readSnapshot(context.Background(), repo, home, catalogLoadCached)
	if got := requests.Load(); got != 0 {
		t.Fatalf("TUI startup queried mutable Marketplace endpoint %d times", got)
	}
	if snapshot.mergedCfg == nil || len(snapshot.mergedCfg.Marketplaces) != 1 || snapshot.mergedCfg.Marketplaces[0].Resources[0].Revision == "" {
		t.Fatalf("startup did not retain exact Marketplace metadata: %#v", snapshot.mergedCfg)
	}
}
