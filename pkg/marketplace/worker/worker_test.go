package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestAcquireCredentialsPreserveRegistryCAFile(t *testing.T) {
	t.Setenv("MARKETPLACE_TEST_TOKEN", "token-value")
	tests := []struct {
		name string
		cred config.Credentials
	}{
		{name: "no authentication"},
		{name: "ssh", cred: config.Credentials{SSHKey: &config.SSHKeyCredential{File: "/registry/key"}}},
		{name: "token", cred: config.Credentials{Token: &config.TokenCredential{Env: "MARKETPLACE_TEST_TOKEN"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toAcquireCredentials(tc.cred, "/registry/ca.pem")
			if err != nil {
				t.Fatalf("toAcquireCredentials: %v", err)
			}
			if got.CAFile != "/registry/ca.pem" {
				t.Fatalf("CAFile = %q", got.CAFile)
			}
		})
	}
}

func TestRefreshEndToEnd(t *testing.T) {
	ctx := context.Background()

	repoDir, cleanup := createTestRepo(t)
	defer cleanup()

	cfg := createTestConfig(t, repoDir)
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()

	deps := Deps{
		Config:        cfg,
		MetadataStore: meta,
		BlobStore:     blobs,
	}

	if err := EnsureMarketplace(ctx, &deps); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}

	src := store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}
	if err := Refresh(ctx, deps, src); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	commit, err := testCommit(repoDir)
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	occ, err := meta.GetRegistryOccurrence(ctx, src.ID, commit)
	if err != nil {
		t.Fatalf("GetRegistryOccurrence: %v", err)
	}
	if occ.Commit != commit {
		t.Fatalf("occurrence commit = %q, want %q", occ.Commit, commit)
	}

	for _, resource := range []struct{ kind, name string }{
		{"skill", "review"},
		{"command", "lint"},
		{"subagent", "reviewer"},
		{"hook", "PreToolUse-command"},
		{"mcpserver", "docs"},
		{"collection", "default"},
	} {
		coord := store.ResourceCoordinate{
			Marketplace: deps.Marketplace.ID,
			Registry:    src.ID,
			Kind:        resource.kind,
			Name:        resource.name,
		}
		rev := getRevision(t, ctx, meta, coord)
		if rev.Coordinate.Kind != resource.kind || rev.Coordinate.Name != resource.name {
			t.Fatalf("resource coordinate mismatch: got %+v", rev.Coordinate)
		}
	}

	if err := meta.ReadyCheck(ctx, blobs); err != nil {
		t.Fatalf("ReadyCheck: %v", err)
	}
}

func TestRefreshAcquiresOnlyConfiguredRoots(t *testing.T) {
	ctx := context.Background()
	repoDir, cleanup := createTestRepo(t)
	defer cleanup()

	unrelated := filepath.Join(repoDir, "unrelated", "large.bin")
	if err := os.MkdirAll(filepath.Dir(unrelated), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, make([]byte, 128*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "add unrelated content"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	cfg := createTestConfig(t, repoDir)
	cfg.Policy.Limits.RepositoryBytes = 64 * 1024
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()
	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}

	if err := Refresh(ctx, deps, store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
}

func TestRefreshLicenseAllowlistRejects(t *testing.T) {
	ctx := context.Background()

	repoDir, cleanup := createTestRepo(t)
	defer cleanup()

	cfg := createTestConfig(t, repoDir)
	cfg.Policy.LicenseAllowlist = []string{"Apache-2.0"}

	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()

	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}

	src := store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}
	if err := Refresh(ctx, deps, src); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	commit, err := testCommit(repoDir)
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	_, err = meta.GetRegistryOccurrence(ctx, src.ID, commit)
	if err != nil {
		t.Fatalf("GetRegistryOccurrence: %v", err)
	}

	coord := store.ResourceCoordinate{
		Marketplace: deps.Marketplace.ID,
		Registry:    src.ID,
		Kind:        "skill",
		Name:        "review",
	}
	rev := getRevision(t, ctx, meta, coord)
	if rev.ArchiveDigest == "" {
		t.Fatalf("rejected resource should retain its intrinsic archive digest")
	}
	if len(rev.Assessment.Findings) == 0 || rev.Assessment.Decision != store.AssessmentDecisionReject {
		t.Fatalf("rejected resource assessment = %#v", rev.Assessment)
	}

	t.Logf("rejected skill findings: %d", len(rev.Assessment.Findings))
}

func TestRefreshIndexOnly(t *testing.T) {
	ctx := context.Background()

	repoDir, cleanup := createTestRepo(t)
	defer cleanup()

	cfg := createTestConfig(t, repoDir)
	cfg.Registries[0].Acquisition = "index-only"

	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()

	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}

	src := store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}
	if err := Refresh(ctx, deps, src); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	commit, err := testCommit(repoDir)
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	occ, err := meta.GetRegistryOccurrence(ctx, src.ID, commit)
	if err != nil {
		t.Fatalf("GetRegistryOccurrence: %v", err)
	}
	if occ.Acquisition != store.AcquisitionIndexOnly {
		t.Fatalf("acquisition = %q, want index-only", occ.Acquisition)
	}

	coord := store.ResourceCoordinate{
		Marketplace: deps.Marketplace.ID,
		Registry:    src.ID,
		Kind:        "skill",
		Name:        "review",
	}
	rev := getRevision(t, ctx, meta, coord)
	if rev.ArchiveDigest != "" {
		t.Fatalf("index-only resource should not have an archive digest")
	}
}

func TestRefreshDoesNotCorruptPrevious(t *testing.T) {
	ctx := context.Background()

	repoDir, cleanup := createTestRepo(t)
	defer cleanup()

	cfg := createTestConfig(t, repoDir)
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()

	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := EnsureMarketplace(ctx, &deps); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}

	src := store.Registry{ID: store.RegistryID(cfg.Registries[0].ID)}
	if err := Refresh(ctx, deps, src); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	commit1, err := testCommit(repoDir)
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	_, err = meta.GetRegistryOccurrence(ctx, src.ID, commit1)
	if err != nil {
		t.Fatalf("first occurrence: %v", err)
	}

	// Introduce a duplicate resource to make the second refresh fail.
	dupPath := filepath.Join(repoDir, ".agents", "skills", "review", "SKILL.md")
	if err := os.WriteFile(dupPath, []byte("# duplicate"), 0o644); err != nil {
		t.Fatalf("write duplicate: %v", err)
	}

	cfg2 := createTestConfig(t, repoDir)
	deps2 := Deps{Config: cfg2, MetadataStore: meta, BlobStore: blobs}
	if err := Refresh(ctx, deps2, src); err == nil {
		t.Fatalf("second Refresh should have failed")
	}

	// First occurrence must still be present and valid.
	occ, err := meta.GetRegistryOccurrence(ctx, src.ID, commit1)
	if err != nil {
		t.Fatalf("first occurrence after failed refresh: %v", err)
	}
	if occ.Commit != commit1 {
		t.Fatalf("occurrence commit changed after failed refresh")
	}

	coord := store.ResourceCoordinate{
		Marketplace: deps.Marketplace.ID,
		Registry:    src.ID,
		Kind:        "skill",
		Name:        "review",
	}
	rev := getRevision(t, ctx, meta, coord)
	if rev.ContentDigest == "" {
		t.Fatalf("first good resource revision should still be present")
	}
}

func TestReindex(t *testing.T) {
	ctx := context.Background()

	repoDir, cleanup := createTestRepo(t)
	defer cleanup()

	cfg := createTestConfig(t, repoDir)
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()

	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}

	if err := Reindex(ctx, deps, cfg, cfg.Registries[0].Slug); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	commit, err := testCommit(repoDir)
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	_, err = meta.GetRegistryOccurrence(ctx, store.RegistryID(cfg.Registries[0].ID), commit)
	if err != nil {
		t.Fatalf("GetRegistryOccurrence: %v", err)
	}
}

type externallyCompletingStore struct {
	store.MetadataStore
	once sync.Once
}

func (s *externallyCompletingStore) GetJob(ctx context.Context, id store.JobID) (store.Job, error) {
	s.once.Do(func() {
		leased, err := s.LeaseJob(ctx, "refresh", "other-worker", time.Minute)
		if err == nil && leased.ID == id {
			_ = s.CompleteJob(ctx, id, "other-worker")
		}
	})
	return s.MetadataStore.GetJob(ctx, id)
}

func TestReindexReturnsWhenAnotherWorkerCompletesItsJob(t *testing.T) {
	repoDir, cleanup := createTestRepo(t)
	defer cleanup()
	cfg := createTestConfig(t, repoDir)
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()
	deps := Deps{Config: cfg, MetadataStore: &externallyCompletingStore{MetadataStore: meta}, BlobStore: blobs}
	if err := Reindex(context.Background(), deps, cfg, cfg.Registries[0].Slug); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
}

func TestReindexDoesNotProcessUnrelatedRefreshJobs(t *testing.T) {
	ctx := context.Background()
	repoDir, cleanup := createTestRepo(t)
	defer cleanup()
	cfg := createTestConfig(t, repoDir)
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()

	unrelated := job.Job{
		ID:          "unrelated",
		Kind:        "refresh",
		Payload:     []byte(`{"registry_id":"missing-registry"}`),
		MaxAttempts: 1,
		CreatedAt:   time.Now().Add(-time.Hour),
	}
	if err := job.Enqueue(ctx, meta, unrelated); err != nil {
		t.Fatalf("enqueue unrelated job: %v", err)
	}

	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := Reindex(ctx, deps, cfg, cfg.Registries[0].Slug); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	got, err := meta.GetJob(ctx, unrelated.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != job.Pending {
		t.Fatalf("unrelated job status = %q, want %q", got.Status, job.Pending)
	}
}

func TestReindexSameCommitAdvancesAssessment(t *testing.T) {
	ctx := context.Background()
	repoDir, cleanup := createTestRepo(t)
	defer cleanup()
	cfg := createTestConfig(t, repoDir)
	meta, blobs := openTestStores(t)
	defer func() { _ = meta.Close() }()
	deps := Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := Reindex(ctx, deps, cfg, cfg.Registries[0].Slug); err != nil {
		t.Fatalf("first Reindex: %v", err)
	}
	commit, err := testCommit(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	firstOccurrence, err := meta.GetRegistryOccurrence(ctx, store.RegistryID(cfg.Registries[0].ID), commit)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Policy.LicenseAllowlist = []string{"Apache-2.0"}
	deps.Config = cfg
	if err := Reindex(ctx, deps, cfg, cfg.Registries[0].Slug); err != nil {
		t.Fatalf("second Reindex: %v", err)
	}
	if err := Reindex(ctx, deps, cfg, cfg.Registries[0].Slug); err != nil {
		t.Fatalf("unchanged Reindex: %v", err)
	}
	secondOccurrence, err := meta.GetRegistryOccurrence(ctx, store.RegistryID(cfg.Registries[0].ID), commit)
	if err != nil {
		t.Fatal(err)
	}
	if firstOccurrence.RegistryRevisionManifestDigest != secondOccurrence.RegistryRevisionManifestDigest {
		t.Fatalf("registry occurrence changed across assessment: %s != %s", firstOccurrence.RegistryRevisionManifestDigest, secondOccurrence.RegistryRevisionManifestDigest)
	}
	coord := store.ResourceCoordinate{Marketplace: "test-mp", Registry: store.RegistryID(cfg.Registries[0].ID), Kind: "skill", Name: "review"}
	revision, err := meta.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatal(err)
	}
	assessments, err := meta.ListResourceAssessments(ctx, coord, revision.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(assessments) != 2 || revision.Assessment.Decision != store.AssessmentDecisionReject {
		t.Fatalf("assessments = %d, current decision = %q", len(assessments), revision.Assessment.Decision)
	}
}

func createTestRepo(tb testing.TB) (string, func()) {
	tb.Helper()
	dir := tb.TempDir()
	subdir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		tb.Fatalf("mkdir repo: %v", err)
	}

	write := func(path, content string) {
		full := filepath.Join(subdir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			tb.Fatalf("mkdir %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			tb.Fatalf("write %q: %v", path, err)
		}
	}

	write(".agents/skills/review/SKILL.md", "---\nname: review\ndescription: Review code carefully\nlicense: MIT\n---\nReview the code carefully.\n")
	write(".agents/commands/lint.md", "---\nname: lint\ndescription: Run the linter\nlicense: MIT\n---\nRun the linter.\n")
	write(".agents/droids/reviewer.md", "---\nname: reviewer\ndescription: Code reviewer subagent\nmodel: sonnet\nlicense: MIT\n---\nReview code.\n")
	write(".agents/hooks/hooks.json", `{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/check.sh"}]}]}`)
	write(".agents/mcp.json", `{"mcpServers":{"docs":{"command":"npx","args":["-y","docs-server"]}}}`)
	write(".agents/collections/default/COLLECTION.yaml", "name: default\ndescription: Default collection\nresources:\n  - name: review\n    kind: skill\n  - name: lint\n    kind: command\n")

	cmds := [][]string{
		{"git", "init", "-q", "--initial-branch=main"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "add", "."},
		{"git", "commit", "-q", "-m", "initial"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = subdir
		out, err := cmd.CombinedOutput()
		if err != nil {
			tb.Fatalf("%v in %q: %v\n%s", c, subdir, err, out)
		}
	}

	return subdir, func() { _ = os.RemoveAll(dir) }
}

func createTestConfig(tb testing.TB, repoDir string) config.Config {
	tb.Helper()
	return config.Config{
		APIVersion: "agent-marketplace.io/marketplace/v1",
		Instance: config.Instance{
			ID:           "test-mp",
			Name:         "Test Marketplace",
			CanonicalURL: "https://marketplace.test",
		},
		Listen:             "127.0.0.1:8080",
		AcquisitionDefault: "mirror",
		Refresh:            config.Refresh{Interval: config.Duration(30 * time.Minute)},
		Policy: config.Policy{
			HiddenContent:    "reject",
			LicenseAllowlist: []string{"MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause"},
			Limits: config.Limits{
				RepositoryBytes:   100 * 1024 * 1024,
				FileCount:         1024,
				FileBytes:         2 * 1024 * 1024,
				ArtifactBytes:     16 * 1024 * 1024,
				CloneDuration:     config.Duration(5 * time.Minute),
				IngestionDuration: config.Duration(2 * time.Minute),
			},
		},
		Registries: []config.Registry{
			{
				ID:          "src-test",
				Slug:        "example-registry",
				Origin:      "file://" + repoDir,
				Ref:         "main",
				Acquisition: "mirror",
				Publisher:   config.Publisher{Name: "Test Publisher"},
				License:     "MIT",
				Roots: []config.RegistryRoot{
					{Path: ".agents", SourceHarness: "factory"},
				},
			},
		},
	}
}

func openTestStores(tb testing.TB) (*sqlite.Store, *fsblob.Store) {
	tb.Helper()
	dir := tb.TempDir()
	meta := sqlite.New(filepath.Join(dir, "marketplace.db"))
	if err := meta.Open(context.Background()); err != nil {
		tb.Fatalf("Open metadata: %v", err)
	}
	blobs := fsblob.New(filepath.Join(dir, "blobs"))
	return meta, blobs
}

func getRevision(tb testing.TB, ctx context.Context, meta store.MetadataStore, coord store.ResourceCoordinate) store.AssessedResourceRevision {
	tb.Helper()
	revs, err := meta.ListResourceRevisions(ctx, coord)
	if err != nil {
		tb.Fatalf("ListResourceRevisions %s/%s: %v", coord.Kind, coord.Name, err)
	}
	if len(revs) == 0 {
		tb.Fatalf("no revisions for %s/%s", coord.Kind, coord.Name)
	}
	return revs[0]
}

func testCommit(repoDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rev-parse: %w: %s", err, out)
	}
	return string(out[:40]), nil
}
