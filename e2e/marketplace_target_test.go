package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
)

type marketplaceTarget struct {
	env       *Env
	bareURL   string
	origin    string
	cfg       mpconfig.Config
	meta      store.MetadataStore
	blobs     store.BlobStore
	handlerMu sync.RWMutex
	handler   http.Handler
	server    *httptest.Server
	slug      string
}

func newMarketplaceTarget(t *testing.T, env *Env, repoName string, registry RegistryDef, gitServerURL, instanceID, slug, stateDir string) *marketplaceTarget {
	t.Helper()
	bareURL := env.CreateBareRepo(repoName, registry.FilesMap())
	origin := strings.TrimRight(gitServerURL, "/") + "/" + repoName + ".git"
	cfgPath := writeMarketplaceConfig(t, stateDir, instanceID, slug, slug+"-id", origin)
	result := env.RunAM("serve", "reindex", "--config", cfgPath)
	if result.ExitCode != 0 {
		t.Fatalf("initial Marketplace reindex failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	cfg, meta, blobs := openPublicGateStores(t, cfgPath)
	target := &marketplaceTarget{
		env:     env,
		bareURL: bareURL,
		origin:  origin,
		cfg:     cfg,
		meta:    meta,
		blobs:   blobs,
		slug:    slug,
		handler: mpapi.NewServer(cfg, meta, blobs).PublicHandler(),
	}
	target.server = httptest.NewServer(http.HandlerFunc(target.serveHTTP))
	t.Cleanup(func() {
		target.server.Close()
		_ = target.meta.Close()
	})
	return target
}

func writeMarketplaceConfig(t *testing.T, stateDir, instanceID, slug, registryID, origin string) string {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("create Marketplace state directory: %v", err)
	}
	dbPath := filepath.Join(stateDir, "marketplace.db")
	blobsPath := filepath.Join(stateDir, "blobs")
	configPath := filepath.Join(stateDir, "marketplace.yaml")
	config := fmt.Sprintf(`apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: %s
  name: %s
  canonical_url: https://example.invalid
storage:
  metadata:
    driver: sqlite
    dsn: %s
  blobs:
    driver: filesystem
    path: %s
acquisition_default: mirror
refresh:
  interval: 1h
policy:
  hidden_content: warn
  license_allowlist: [MIT]
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 300s
    ingestion_duration: 120s
registries:
  - id: %s
    slug: %s
    origin: %s
    ref: main
    acquisition: mirror
    license: MIT
    roots:
      - path: .
        source_harness: claude
revocations:
  registry_ids: []
  coordinates: []
  revisions: []
`, instanceID, instanceID, dbPath, blobsPath, registryID, slug, origin)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write Marketplace config: %v", err)
	}
	return configPath
}

func (m *marketplaceTarget) serveHTTP(w http.ResponseWriter, r *http.Request) {
	m.handlerMu.RLock()
	defer m.handlerMu.RUnlock()
	m.handler.ServeHTTP(w, r)
}

func (m *marketplaceTarget) setHandler(handler http.Handler) {
	m.handlerMu.Lock()
	defer m.handlerMu.Unlock()
	m.handler = handler
}

func (m *marketplaceTarget) reindex(t *testing.T, cfg mpconfig.Config) {
	t.Helper()
	if err := worker.Reindex(context.Background(), worker.Deps{Config: cfg, MetadataStore: m.meta, BlobStore: m.blobs}, cfg, m.slug); err != nil {
		t.Fatalf("Marketplace reindex: %v", err)
	}
}

func (m *marketplaceTarget) revoke(t *testing.T, revocations MarketplaceRevocations) {
	t.Helper()
	cfg := m.cfg
	cfg.Revocations = mpconfig.Revocations{
		Coordinates: append([]string(nil), revocations.Coordinates...),
		Revisions:   append([]string(nil), revocations.Revisions...),
	}
	if err := worker.EnsureRevocations(t.Context(), worker.Deps{Config: cfg, MetadataStore: m.meta, BlobStore: m.blobs}); err != nil {
		t.Fatalf("reconcile Marketplace revocation: %v", err)
	}
	m.cfg = cfg
	m.setHandler(mpapi.NewServer(cfg, m.meta, m.blobs).PublicHandler())
}

func pushRegistryFiles(t *testing.T, bareURL string, files map[string]string) {
	t.Helper()
	work := t.TempDir()
	gitRun(t, "", "clone", "-b", "main", strings.TrimPrefix(bareURL, "file://"), work)
	for path, content := range files {
		fullPath := filepath.Join(work, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, work, "config", "user.email", "e2e@example.com")
	gitRun(t, work, "config", "user.name", "E2E")
	gitRun(t, work, "add", ".")
	gitRun(t, work, "commit", "-m", "update source")
	gitRun(t, work, "push", "origin", "main")
}
