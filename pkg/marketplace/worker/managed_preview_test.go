package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func fakeClone(repo, commit string, err error) managedCloneFunc {
	return func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		if err != nil {
			return nil, err
		}
		return &acquire.Result{Dir: repo, Commit: commit}, nil
	}
}

func requestPreview(t testing.TB, svc admin.Service, r admin.Registry) store.ManagedPreview {
	t.Helper()
	view, err := svc.Read(context.Background(), "test-mp")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), "test-mp", admin.Mutation{Action: "upsert_registry", RegistryID: r.ID, Registry: &r, ExpectedRevision: view.Revision, Actor: store.ManagedActor{Issuer: "test", Subject: "admin"}})
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

func runPreviewJob(t testing.TB, deps Deps, preview store.ManagedPreview, clone managedCloneFunc) store.ManagedPreview {
	t.Helper()
	ctx := context.Background()
	j, err := deps.MetadataStore.LeaseJob(ctx, admin.PreviewJobKind, "preview-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedPreview(ctx, deps, j, "preview-worker", clone); err != nil {
		t.Fatalf("process preview: %v", err)
	}
	if job, err := deps.MetadataStore.GetJob(ctx, j.ID); err != nil || job.Status != store.JobStatusCompleted {
		t.Fatalf("preview job must complete: %+v %v", job, err)
	}
	loaded, err := deps.MetadataStore.(store.ManagedPreviewStore).GetManagedPreview(ctx, "test-mp", preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Terminal() || loaded.CompletedAt.IsZero() {
		t.Fatalf("preview must be terminal: %+v", loaded)
	}
	return loaded
}

func newPreviewRegistry(roots ...store.RegistryRoot) admin.Registry {
	return admin.Registry{ID: "draft", Slug: "draft", Origin: "https://git.example/draft", Ref: "main", Roots: roots, Acquisition: store.AcquisitionMirror, License: "MIT"}
}

func TestManagedPreviewDiscoversWithoutPublishing(t *testing.T) {
	ctx := context.Background()
	deps, svc := managedFixture(t)
	repo, cleanup := createTestRepo(t)
	defer cleanup()
	commit, err := testCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	preview := requestPreview(t, svc, newPreviewRegistry(store.RegistryRoot{Path: ".agents", SourceHarness: "factory"}))
	loaded := runPreviewJob(t, deps, preview, fakeClone(repo, commit, nil))
	if loaded.Status != store.ManagedPreviewSucceeded || loaded.Result == nil || loaded.Failure != nil {
		t.Fatalf("expected success: %+v %+v", loaded, loaded.Failure)
	}
	result := loaded.Result
	if result.Commit != commit || result.PolicyRevision != 1 || result.DiffBase != "" || len(result.Roots) != 1 {
		t.Fatalf("result header: %+v", result)
	}
	agents := result.Roots[0]
	if agents.Path != ".agents" || agents.SourceHarness != "factory" || agents.ResourceCount < 5 || len(agents.Resources) != agents.ResourceCount || result.TotalResources != agents.ResourceCount {
		t.Fatalf("root discovery: %+v", agents)
	}
	if len(result.Diagnostics) == 0 || result.Diagnostics[0].Root != ".agents" || result.Diagnostics[0].Code == "" || result.Diagnostics[0].Error {
		t.Fatalf("informational catalog diagnostics must be surfaced: %+v", result.Diagnostics)
	}
	kinds := make(map[string]store.ManagedPreviewResource)
	for _, entry := range agents.Resources {
		kinds[entry.Kind] = entry
		if entry.Change != "" {
			t.Fatalf("new registries have no diff baseline: %+v", entry)
		}
	}
	if skill := kinds["skill"]; skill.Name != "review" || skill.SourcePath == "" || skill.Decision == "" || skill.Description == "" {
		t.Fatalf("skill entry incomplete: %+v", skill)
	}
	for _, kind := range []string{"command", "subagent", "hook", "mcpserver", "collection"} {
		if _, ok := kinds[kind]; !ok {
			t.Fatalf("missing %s in %+v", kind, agents.Resources)
		}
	}

	if _, err := deps.MetadataStore.(store.PublicationStore).CurrentRegistryPublication(ctx, "draft"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("preview must not publish: %v", err)
	}
	if _, err := deps.MetadataStore.GetRegistry(ctx, "draft"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("preview must not create registry rows: %v", err)
	}
	if view, _ := svc.Read(ctx, "test-mp"); view.Revision != 1 {
		t.Fatalf("preview must not change configuration: %+v", view)
	}
}

func TestManagedPreviewReportsFailuresWithoutSecrets(t *testing.T) {
	deps, svc := managedFixture(t)
	repo, cleanup := createTestRepo(t)
	defer cleanup()
	commit, err := testCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "misc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "misc", "README.md"), []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		roots []store.RegistryRoot
		clone managedCloneFunc
		code  string
		named string
	}{
		"missing root":   {[]store.RegistryRoot{{Path: "skills"}}, fakeClone(repo, commit, nil), "root_missing", `"skills"`},
		"no layout root": {[]store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}, {Path: "misc"}}, fakeClone(repo, commit, nil), "catalog_error", `"misc"`},
		"clone failed":   {[]store.RegistryRoot{{Path: "."}}, fakeClone("", "", errors.New("fatal: https://user:secret-token@git.example/draft not found")), "acquisition_failed", ""},
	} {
		t.Run(name, func(t *testing.T) {
			preview := requestPreview(t, svc, newPreviewRegistry(tc.roots...))
			loaded := runPreviewJob(t, deps, preview, tc.clone)
			if loaded.Status != store.ManagedPreviewFailed || loaded.Failure == nil || loaded.Result != nil {
				t.Fatalf("expected failure: %+v", loaded)
			}
			if loaded.Failure.Code != tc.code || strings.Contains(loaded.Failure.Message, "secret-token") || strings.Contains(loaded.Failure.Message, repo) {
				t.Fatalf("failure: %+v", loaded.Failure)
			}
			if !strings.Contains(loaded.Failure.Message, tc.named) {
				t.Fatalf("offending root must be named: %+v", loaded.Failure)
			}
		})
	}
}

func TestManagedPreviewDiffsAgainstCurrentPublication(t *testing.T) {
	ctx := context.Background()
	deps, svc := managedFixture(t)
	repo, cleanup := createTestRepo(t)
	defer cleanup()
	commit, err := testCommit(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduleManaged(ctx, deps, "seed"); err != nil {
		t.Fatal(err)
	}
	refresh, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "seed", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedRefresh(ctx, deps, refresh, "seed", fakeClone(repo, commit, nil)); err != nil {
		t.Fatal(err)
	}
	published, err := deps.MetadataStore.(store.PublicationStore).CurrentRegistryPublication(ctx, "src-test")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(repo, ".agents", "droids", "reviewer.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review code very carefully\nlicense: MIT\n---\nReview it twice.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills", "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "skills", "deploy", "SKILL.md"), []byte("---\nname: deploy\ndescription: Deploy safely\nlicense: MIT\n---\nDeploy.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	view, err := svc.Read(ctx, "test-mp")
	if err != nil {
		t.Fatal(err)
	}
	existing := view.Snapshot.Registries[0]
	preview := requestPreview(t, svc, existing)
	if !preview.Existing {
		t.Fatal("existing registry preview must be marked existing")
	}
	loaded := runPreviewJob(t, deps, preview, fakeClone(repo, "f"+commit[1:], nil))
	if loaded.Status != store.ManagedPreviewSucceeded || loaded.Result.DiffBase != published.ID {
		t.Fatalf("diff baseline: %+v %+v", loaded, loaded.Failure)
	}
	changes := make(map[string]string)
	for _, entry := range loaded.Result.Roots[0].Resources {
		changes[entry.Kind+"/"+entry.Name] = entry.Change
	}
	for _, entry := range loaded.Result.Removed {
		changes[entry.Kind+"/"+entry.Name] = entry.Change
	}
	for key, want := range map[string]string{"skill/deploy": "added", "skill/review": "changed", "subagent/reviewer": "removed", "command/lint": "unchanged"} {
		if changes[key] != want {
			t.Fatalf("%s: want %s, got %q in %+v", key, want, changes[key], changes)
		}
	}
	if again, err := deps.MetadataStore.(store.PublicationStore).CurrentRegistryPublication(ctx, "src-test"); err != nil || again.ID != published.ID {
		t.Fatalf("preview must not advance publication: %+v %v", again, err)
	}
}

func TestMalformedSkillPreviewAndPublication(t *testing.T) {
	ctx := context.Background()
	deps, svc := managedFixture(t)
	repo := t.TempDir()
	writeSkill := func(name, body string) {
		t.Helper()
		dir := filepath.Join(repo, ".agents", "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	valid := func(name string) string {
		return "---\nname: " + name + "\ndescription: Valid skill\nlicense: MIT\n---\nReview code.\n"
	}
	broken := "---\nname: [broken\n---\n"
	writeSkill("good", valid("good"))
	writeSkill("bad", valid("bad"))
	refresh := func(label string) error {
		t.Helper()
		if err := scheduleManaged(ctx, deps, label); err != nil {
			t.Fatal(err)
		}
		job, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", label, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return processManagedRefresh(ctx, deps, job, label, fakeClone(repo, strings.Repeat(label, 40), nil))
	}
	publication := func() store.RegistryPublication {
		t.Helper()
		p, err := deps.MetadataStore.(store.PublicationStore).CurrentRegistryPublication(ctx, "src-test")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := refresh("a"); err != nil {
		t.Fatal(err)
	}
	initial := publication()
	if len(initial.Resources) != 2 {
		t.Fatalf("initial resources: %+v", initial.Resources)
	}
	writeSkill("bad", broken)
	view, err := svc.Read(ctx, "test-mp")
	if err != nil {
		t.Fatal(err)
	}
	preview := runPreviewJob(t, deps, requestPreview(t, svc, view.Snapshot.Registries[0]), fakeClone(repo, strings.Repeat("b", 40), nil))
	if preview.Status != store.ManagedPreviewSucceeded || preview.Result.TotalResources != 1 || len(preview.Result.Removed) != 1 || preview.Result.Removed[0].Name != "bad" {
		t.Fatalf("partial preview: %+v", preview)
	}
	found := false
	for _, d := range preview.Result.Diagnostics {
		if strings.HasSuffix(d.Code, ".load-skill-failed") && !d.Error {
			found = true
		}
	}
	if !found {
		t.Fatal("missing skipped-skill warning")
	}
	if publication().ID != initial.ID {
		t.Fatal("preview published")
	}
	if err := refresh("b"); err != nil {
		t.Fatal(err)
	}
	partial := publication()
	if partial.ID == initial.ID || len(partial.Resources) != 1 || partial.Resources[0].Coordinate.Name != "good" {
		t.Fatalf("partial publication: %+v", partial)
	}
	writeSkill("good", broken)
	if err := refresh("c"); !errors.Is(err, ErrManagedRefresh) {
		t.Fatalf("all malformed refresh: %v", err)
	}
	if publication().ID != partial.ID {
		t.Fatal("all-malformed refresh replaced previous publication")
	}
}

func TestManagedPreviewJobWithoutRecordCompletes(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	payload, _ := json.Marshal(admin.PreviewJobPayload{Marketplace: "test-mp", PreviewID: "gone"})
	if err := deps.MetadataStore.EnqueueJob(ctx, store.Job{ID: "managed-preview-gone", Kind: admin.PreviewJobKind, Payload: payload, IdempotencyKey: "managed-preview-gone", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, admin.PreviewJobKind, "w", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedPreview(ctx, deps, j, "w", fakeClone("", "", errors.New("must not clone"))); err != nil {
		t.Fatal(err)
	}
	if job, _ := deps.MetadataStore.GetJob(ctx, j.ID); job.Status != store.JobStatusCompleted {
		t.Fatalf("orphan preview job should complete: %+v", job)
	}
}

func TestPreviewRootIndexPrefersDeepestRoot(t *testing.T) {
	roots := []store.RegistryRoot{{Path: "."}, {Path: "./team/"}, {Path: "team/skills"}}
	for path, want := range map[string]int{"README.md": 0, "team/commands/x.md": 1, "team/skills/a/SKILL.md": 2, "team/skillsets/x": 1} {
		if got := previewRootIndex(roots, path); got != want {
			t.Errorf("%s: want %d, got %d", path, want, got)
		}
	}
}
