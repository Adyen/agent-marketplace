package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

func TestAutomaticRefreshRemainsPinnedWhenRefMoves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	origin := filepath.Join(t.TempDir(), "origin")
	writeExactRefreshSkill(t, origin, "pinned")
	runExactRefreshGit(t, origin, "init")
	runExactRefreshGit(t, origin, "config", "user.email", "test@example.com")
	runExactRefreshGit(t, origin, "config", "user.name", "Test")
	runExactRefreshGit(t, origin, "add", ".")
	runExactRefreshGit(t, origin, "commit", "-m", "pinned")

	canonical := "https://example.invalid/tui-refresh"
	oid, err := cache.ResolveExactGitRefWithContext(context.Background(), canonical, origin, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.AcquireExactGit(canonical, origin, oid)
	if err != nil {
		t.Fatal(err)
	}
	built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot, RootPath: ".", SourceHarness: "factory"})
	if err != nil {
		t.Fatal(err)
	}
	root := lockfile.GitRoot{Path: ".", SourceHarness: built.SourceHarness, RevisionSchema: "v1", RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
	for _, resource := range built.Manifest.Resources {
		support := make(map[string]lockfile.Digest, len(resource.SupportFiles))
		for name, digest := range resource.SupportFiles {
			support[name] = lockfile.Digest(digest)
		}
		root.Resources = append(root.Resources, lockfile.GitResource{Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest), ArchiveDigest: lockfile.Digest(resource.ArchiveDigest), SupportFiles: support})
	}
	commit, err := lockfile.ParseGitOID(oid.String())
	if err != nil {
		t.Fatal(err)
	}

	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configBytes := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: " + canonical + "\n    ref: HEAD\n    source_harness: factory\n    skills: [pinned]\n")
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), configBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	lockBytes, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: canonical, Ref: "HEAD", Commit: commit, Roots: []lockfile.GitRoot{root}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "lock.yaml"), lockBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	writeExactRefreshSkill(t, origin, "moved")
	runExactRefreshGit(t, origin, "add", ".")
	runExactRefreshGit(t, origin, "commit", "-m", "move ref")
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("locked snapshot disappeared before refresh: %v", err)
	}

	previousOrg, previousGlobal, previousRepo, previousLocal := loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig
	loadOrgConfig = func() (*config.Config, error) { return nil, nil }
	loadGlobalConfig = func() (*config.Config, error) { return nil, nil }
	loadRepoConfig = func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(agentsDir, "config.yaml"))
	}
	loadLocalConfig = func() (*config.LocalConfig, error) { return nil, nil }
	t.Cleanup(func() {
		loadOrgConfig, loadGlobalConfig, loadRepoConfig, loadLocalConfig = previousOrg, previousGlobal, previousRepo, previousLocal
	})

	materializeEntered := make(chan struct{})
	resumeMaterialize := make(chan struct{})
	previousMaterialize := runMaterialize
	runMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		close(materializeEntered)
		<-resumeMaterialize
		return previousMaterialize(ctx, root, artifacts, opts...)
	}
	t.Cleanup(func() { runMaterialize = previousMaterialize })

	resultDone := make(chan snapshotLoadedMsg, 1)
	go func() { resultDone <- readSnapshot(context.Background(), repoRoot, home, catalogLoadRefresh) }()
	select {
	case early := <-resultDone:
		t.Fatalf("snapshot returned before materialization: %v", early.errs)
	case <-materializeEntered:
	}
	gcDone := make(chan error, 1)
	go func() {
		_, gcErr := cache.GarbageCollectWithLocks(false, nil, &config.Config{})
		gcDone <- gcErr
	}()
	select {
	case gcErr := <-gcDone:
		t.Fatalf("GC completed while TUI consumed exact snapshot: %v", gcErr)
	case <-time.After(100 * time.Millisecond):
	}
	close(resumeMaterialize)
	result := <-resultDone
	if len(result.errs) > 0 {
		t.Fatalf("refresh errors: %v", result.errs)
	}
	if len(result.rows) != 1 || result.rows[0].Artifact.Name != "pinned" {
		t.Fatalf("ref move replaced locked inventory: %#v", result.rows)
	}
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GC did not remove snapshot after TUI consumption: %v", err)
	}
}

func writeExactRefreshSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, ".factory", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runExactRefreshGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
