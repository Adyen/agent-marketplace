package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

// spec: exact-snapshot-verification/snapshot-tampered-between-tui-actions-is-reported-on-reload
func TestTamperedSnapshotReportedOnReloadWithinOneSession(t *testing.T) {
	assertTamperedSnapshotReportedOnReload(t, context.Background())
}

func TestTamperedSnapshotReportedOnReloadWithOuterScope(t *testing.T) {
	assertTamperedSnapshotReportedOnReload(t, cache.WithExactGitVerificationScope(context.Background()))
}

func assertTamperedSnapshotReportedOnReload(t *testing.T, ctx context.Context) {
	t.Helper()
	home, repo, origin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	writeExactRefreshSkill(t, origin, "pinned")
	runExactRefreshGit(t, origin, "init", "-b", "main")
	runExactRefreshGit(t, origin, "add", ".")
	runExactRefreshGit(t, origin, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "revision A")
	source := "file://" + origin
	oid, err := cache.ResolveExactGitRef(source, source, "main")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.AcquireExactGit(source, source, oid)
	if err != nil {
		t.Fatal(err)
	}
	built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot, SourceHarness: "factory"})
	if err != nil {
		t.Fatal(err)
	}
	root := lockfile.GitRoot{Path: ".", SourceHarness: built.SourceHarness, RevisionSchema: built.Manifest.SchemaVersion, RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
	for _, resource := range built.Manifest.Resources {
		root.Resources = append(root.Resources, lockfile.GitResource{Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest), ArchiveDigest: lockfile.Digest(resource.ArchiveDigest)})
	}
	lockBytes, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: source, Ref: "main", Commit: oid, Roots: []lockfile.GitRoot{root}}}})
	if err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := config.ProjectConfigPath(repo)
	write(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  shared:\n    git: "+source+"\n    ref: main\n    source_harness: factory\nresources:\n  shared: [skill/pinned]\n"))
	write(filepath.Join(repo, ".agents", "lock.yaml"), lockBytes)
	defer swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })()
	defer swapLoadGlobalConfig(func() (*config.Config, error) { return nil, nil })()
	defer swapLoadRepoConfig(func() (*config.Config, error) { return config.LoadConfigFromPath(configPath) })()
	defer swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })()

	verified := readSnapshot(ctx, repo, home, catalogLoadCached)
	if len(verified.errs) != 0 {
		t.Fatalf("initial TUI inspection did not verify the snapshot: %v", verified.errs)
	}
	if err := os.WriteFile(filepath.Join(snapshot, ".factory", "skills", "pinned", "SKILL.md"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reloaded := readSnapshot(ctx, repo, home, catalogLoadCached)
	var lockErr *pipeline.LockViewError
	if !errors.As(errors.Join(reloaded.errs...), &lockErr) || lockErr.Code != "lock.snapshot-unavailable" || !errors.Is(lockErr, cache.ErrExactGitSnapshotUntrusted) {
		t.Fatalf("reload did not report the tampered snapshot as an error: %v", reloaded.errs)
	}
}
