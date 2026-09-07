package e2e_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func newServeReindexEnv(t *testing.T) *Env {
	t.Helper()
	env, _ := newServeReindexEnvWithURL(t)
	return env
}

func newServeReindexEnvWithURL(t *testing.T) (*Env, string) {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()

	env.CreateBareRepo("serve-reg", map[string]string{
		".agents/skills/review/SKILL.md": "---\nname: review\ndescription: Review code carefully\nlicense: MIT\n---\n\n# review\n",
	})
	gitServer := newGitHTTPServer(t, env.repos)
	t.Cleanup(gitServer.Close)
	regURL := gitServer.URL + "/serve-reg.git"

	cfg := fmt.Sprintf(`apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: e2e-serve-mp
  name: E2E Serve Marketplace
  canonical_url: https://example.com
storage:
  metadata:
    driver: sqlite
    dsn: marketplace.db
  blobs:
    driver: filesystem
    path: blobs
acquisition_default: mirror
refresh:
  interval: 1h
policy:
  hidden_content: reject
  license_allowlist:
    - MIT
    - Apache-2.0
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 300s
    ingestion_duration: 120s
registries:
  - id: src-serve
    slug: serve-source
    origin: %s
    ref: main
    roots:
      - path: .agents
        source_harness: factory
`, regURL)
	env.WriteFile("/workspace/marketplace.yaml", cfg)
	return env, regURL
}

func openServeMetadata(t *testing.T, env *Env) *sqlite.Store {
	t.Helper()
	metadata := sqlite.New(env.mustResolvePath("/workspace/marketplace.db"))
	if err := metadata.Open(t.Context()); err != nil {
		t.Fatalf("open Marketplace metadata store: %v", err)
	}
	return metadata
}

// TestServeReindex verifies that `serve reindex` can load a Marketplace operator
// config, clone a configured registry, build artifacts, and publish a source
// revision.
func TestServeReindex(t *testing.T) {
	env := newServeReindexEnv(t)

	r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml")
	if r.ExitCode != 0 {
		t.Fatalf("serve reindex exited %d:\n%s", r.ExitCode, r.Combined)
	}

	env.AssertFileExists("/workspace/marketplace.db")
	env.AssertFileExists("/workspace/blobs")
}

func TestServeReindexRecursivelyIngestsParentRoot(t *testing.T) {
	env := newServeReindexEnv(t)
	config := strings.Replace(env.ReadFile("/workspace/marketplace.yaml"), "      - path: .agents\n", "      - path: .\n", 1)
	env.WriteFile("/workspace/marketplace.yaml", config)

	r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml")
	if r.ExitCode != 0 {
		t.Fatalf("serve reindex exited %d:\n%s", r.ExitCode, r.Combined)
	}

	metadata := openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	coordinate := store.ResourceCoordinate{Marketplace: "e2e-serve-mp", Registry: "src-serve", Kind: "skill", Name: "review"}
	if _, err := metadata.GetLatestResourceRevision(t.Context(), coordinate); err != nil {
		t.Fatalf("load recursively indexed resource: %v", err)
	}
}

func TestServeReindexDoesNotProcessUnrelatedRefreshJobs(t *testing.T) {
	env := newServeReindexEnv(t)
	if r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml"); r.ExitCode != 0 {
		t.Fatalf("initial serve reindex exited %d:\n%s", r.ExitCode, r.Combined)
	}

	metadata := openServeMetadata(t, env)
	unrelated := store.Job{
		ID:          "unrelated-refresh",
		Kind:        "refresh",
		Payload:     []byte(`{"registry_id":"missing-source"}`),
		MaxAttempts: 1,
		CreatedAt:   time.Now().Add(-time.Hour),
	}
	if err := job.Enqueue(t.Context(), metadata, unrelated); err != nil {
		_ = metadata.Close()
		t.Fatalf("enqueue unrelated refresh job: %v", err)
	}
	if err := metadata.Close(); err != nil {
		t.Fatalf("close Marketplace metadata store: %v", err)
	}

	r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml", "serve-source")
	if r.ExitCode != 0 {
		t.Fatalf("serve reindex with unrelated job exited %d:\n%s", r.ExitCode, r.Combined)
	}

	metadata = openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	got, err := metadata.GetJob(t.Context(), unrelated.ID)
	if err != nil {
		t.Fatalf("load unrelated refresh job: %v", err)
	}
	if got.Status != store.JobStatusPending {
		t.Fatalf("unrelated refresh job status = %q, want %q", got.Status, store.JobStatusPending)
	}
}

func TestServeReindexTargetsOnlyRequestedRegistry(t *testing.T) {
	env := newServeReindexEnv(t)
	var requests atomic.Int32
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer broken.Close()
	env.WriteFile("/workspace/marketplace.yaml", env.ReadFile("/workspace/marketplace.yaml")+fmt.Sprintf(`  - id: broken-source
    slug: broken-source
    origin: %s/broken.git
    ref: main
    roots:
      - path: .agents
        source_harness: factory
`, broken.URL))

	r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml", "src-serve")
	if r.ExitCode != 0 {
		t.Fatalf("targeted serve reindex exited %d:\n%s", r.ExitCode, r.Combined)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("targeted reindex contacted unrelated registry %d time(s)", got)
	}

	metadata := openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	target, err := metadata.GetRegistry(t.Context(), "src-serve")
	if err != nil {
		t.Fatalf("load targeted registry: %v", err)
	}
	if target.RefreshStatus != store.RegistryRefreshStatusOK {
		t.Fatalf("targeted registry status = %q, want %q", target.RefreshStatus, store.RegistryRefreshStatusOK)
	}
	if _, err := metadata.GetRegistry(t.Context(), "broken-source"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unrelated registry state error = %v, want %v", err, store.ErrNotFound)
	}
}

func TestServeReindexSameCommitIsIdempotent(t *testing.T) {
	env := newServeReindexEnv(t)
	for _, selector := range []string{"serve-source", "src-serve"} {
		if r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml", selector); r.ExitCode != 0 {
			t.Fatalf("serve reindex %q exited %d:\n%s", selector, r.ExitCode, r.Combined)
		}
	}

	metadata := openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	history, err := metadata.RegistryHistory(t.Context(), "src-serve")
	if err != nil {
		t.Fatalf("load registry history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("registry history has %d occurrences after unchanged reindex, want 1", len(history))
	}
}

func TestServeReindexFailurePreservesLastGoodRevision(t *testing.T) {
	env := newServeReindexEnv(t)
	if r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml", "serve-source"); r.ExitCode != 0 {
		t.Fatalf("initial serve reindex exited %d:\n%s", r.ExitCode, r.Combined)
	}

	coordinate := store.ResourceCoordinate{Marketplace: "e2e-serve-mp", Registry: "src-serve", Kind: "skill", Name: "review"}
	metadata := openServeMetadata(t, env)
	baseline, err := metadata.GetLatestResourceRevision(t.Context(), coordinate)
	if err != nil {
		_ = metadata.Close()
		t.Fatalf("load baseline resource revision: %v", err)
	}
	if err := metadata.Close(); err != nil {
		t.Fatalf("close Marketplace metadata store: %v", err)
	}

	config := env.ReadFile("/workspace/marketplace.yaml")
	root := "      - path: .agents\n        source_harness: factory\n"
	if !strings.Contains(config, root) {
		t.Fatal("serve fixture config does not contain the expected registry root")
	}
	env.WriteFile("/workspace/marketplace.yaml", strings.Replace(config, root, root+root, 1))
	r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml", "serve-source")
	if r.ExitCode == 0 {
		t.Fatalf("serve reindex unexpectedly accepted duplicate registry roots:\n%s", r.Combined)
	}

	metadata = openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	current, err := metadata.GetLatestResourceRevision(t.Context(), coordinate)
	if err != nil {
		t.Fatalf("load resource revision after failed reindex: %v", err)
	}
	if current.ContentDigest != baseline.ContentDigest {
		t.Fatalf("failed reindex replaced last good digest %q with %q", baseline.ContentDigest, current.ContentDigest)
	}
	history, err := metadata.RegistryHistory(t.Context(), "src-serve")
	if err != nil {
		t.Fatalf("load registry history after failed reindex: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("registry history has %d occurrences after failed reindex, want 1", len(history))
	}
	registry, err := metadata.GetRegistry(t.Context(), "src-serve")
	if err != nil {
		t.Fatalf("load registry after failed reindex: %v", err)
	}
	if registry.RefreshStatus != store.RegistryRefreshStatusFailed || registry.RefreshError == "" {
		t.Fatalf("failed registry status = %q, error = %q", registry.RefreshStatus, registry.RefreshError)
	}
}

func TestServeReindexAllContinuesAfterFailures(t *testing.T) {
	env, registryURL := newServeReindexEnvWithURL(t)
	config := env.ReadFile("/workspace/marketplace.yaml")
	root := "      - path: .agents\n        source_harness: factory\n"
	config = strings.Replace(config, root, root+root, 1)
	config += fmt.Sprintf(`  - id: src-good
    slug: good-source
    origin: %s
    ref: main
    roots:
      - path: .agents
        source_harness: factory
  - id: src-bad-two
    slug: bad-source-two
    origin: %s
    ref: main
    roots:
      - path: .agents
        source_harness: factory
      - path: .agents
        source_harness: factory
`, registryURL, registryURL)
	env.WriteFile("/workspace/marketplace.yaml", config)

	r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml")
	if r.ExitCode == 0 || !strings.Contains(r.Combined, `reindex registry "src-serve"`) || !strings.Contains(r.Combined, `reindex registry "src-bad-two"`) {
		t.Fatalf("all-registry reindex result = exit %d:\n%s", r.ExitCode, r.Combined)
	}

	metadata := openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	good, err := metadata.GetRegistry(t.Context(), "src-good")
	if err != nil {
		t.Fatalf("load successful registry after sibling failures: %v", err)
	}
	if good.RefreshStatus != store.RegistryRefreshStatusOK {
		t.Fatalf("successful registry status = %q, want %q", good.RefreshStatus, store.RegistryRefreshStatusOK)
	}
	for _, id := range []store.RegistryID{"src-serve", "src-bad-two"} {
		failed, err := metadata.GetRegistry(t.Context(), id)
		if err != nil {
			t.Fatalf("load failed registry %q: %v", id, err)
		}
		if failed.RefreshStatus != store.RegistryRefreshStatusFailed || failed.RefreshError == "" {
			t.Fatalf("registry %q status = %q, error = %q", id, failed.RefreshStatus, failed.RefreshError)
		}
	}
}

func TestServeReindexUnknownRegistryDoesNotCreateRegistryState(t *testing.T) {
	env := newServeReindexEnv(t)
	r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml", "missing-source")
	if r.ExitCode == 0 || !strings.Contains(r.Combined, `registry "missing-source" not found`) {
		t.Fatalf("unknown registry result = exit %d:\n%s", r.ExitCode, r.Combined)
	}

	metadata := openServeMetadata(t, env)
	defer func() { _ = metadata.Close() }()
	if _, err := metadata.GetMarketplace(t.Context(), "e2e-serve-mp"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Marketplace state error = %v, want %v", err, store.ErrNotFound)
	}
	if _, err := metadata.GetRegistry(t.Context(), "src-serve"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("registry state error = %v, want %v", err, store.ErrNotFound)
	}
}
