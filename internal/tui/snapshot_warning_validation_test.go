package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

// spec: tui-feedback/missing-locked-snapshot-is-a-recoverable-warning
// spec: tui-feedback/untrusted-snapshots-remain-errors
// spec: tui-feedback/failed-sync-remains-an-error
func TestMissingLockedSnapshotWarnsAndRecovers(t *testing.T) {
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
	first, err := cache.ResolveExactGitRef(source, source, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.AcquireExactGit(source, source, first); err != nil {
		t.Fatal(err)
	}
	writeExactRefreshSkill(t, origin, "new-in-b")
	runExactRefreshGit(t, origin, "add", ".")
	runExactRefreshGit(t, origin, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "revision B")
	t.Setenv("HOME", t.TempDir())
	second, err := cache.ResolveExactGitRef(source, source, "main")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.AcquireExactGit(source, source, second)
	if err != nil {
		t.Fatal(err)
	}
	built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot, SourceHarness: "factory"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(built.Manifest.Resources) != 2 {
		t.Fatalf("fixture did not advance to two valid resources: first=%s second=%s resources=%v", first, second, built.Manifest.Resources)
	}
	root := lockfile.GitRoot{Path: ".", SourceHarness: built.SourceHarness, RevisionSchema: built.Manifest.SchemaVersion, RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
	for _, resource := range built.Manifest.Resources {
		root.Resources = append(root.Resources, lockfile.GitResource{Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest), ArchiveDigest: lockfile.Digest(resource.ArchiveDigest)})
	}
	lockBytes, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: source, Ref: "main", Commit: second, Roots: []lockfile.GitRoot{root}}}})
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
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	write(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  shared:\n    git: "+source+"\n    ref: main\n    source_harness: factory\nresources:\n  shared: [skill/pinned]\n"))
	write(lockPath, lockBytes)
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	defer swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })()
	defer swapLoadGlobalConfig(func() (*config.Config, error) { return nil, nil })()
	defer swapLoadRepoConfig(func() (*config.Config, error) { return config.LoadConfigFromPath(configPath) })()
	defer swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })()
	for _, mode := range []catalogLoadMode{catalogLoadCached, catalogLoadRefresh} {
		actual := readSnapshot(context.Background(), repo, home, mode)
		if len(actual.errs) != 0 || !errors.Is(errors.Join(actual.warnings...), cache.ErrExactGitSnapshotUnavailable) || actual.driftAssessment != driftFailed {
			t.Fatalf("missing snapshot must warn while retaining unverified drift: errors=%v warnings=%v drift=%v", actual.errs, actual.warnings, actual.driftAssessment)
		}
		if len(actual.rows) != 1 || actual.rows[0].Artifact.Name != "pinned" || actual.rows[0].UnavailableReason == "" || actual.rows[0].UnavailableSoft {
			t.Fatalf("missing snapshot lost or falsely verified its selected resource: %#v", actual.rows)
		}
		if actual.refreshAfterLoad {
			t.Fatal("TUI unexpectedly scheduled automatic exact snapshot acquisition")
		}
		if _, err := cache.OpenExactGit(source, second); !errors.Is(err, cache.ErrExactGitSnapshotUnavailable) {
			t.Fatalf("inspection acquired or altered the missing snapshot: %v", err)
		}
		for _, hint := range []string{"press s to sync", "agent-marketplace sync --locked"} {
			if !strings.Contains(fmt.Sprint(actual.warnings), hint) {
				t.Fatalf("missing recovery hint %q: %v", hint, actual.warnings)
			}
		}
		model := NewModel(repo, home)
		model.loading, model.pane = false, paneList
		model.applySnapshotStatus(actual)
		model.applySnapshotData(actual)
		if !model.rowsStale || strings.Contains(model.driftStatusSegment(), "in sync") || snapshotAcceptanceError(actual) == nil {
			t.Fatal("warning falsely established a verified/in-sync snapshot")
		}
		next, cmd := model.dispatchAction(actionSync)
		if cmd == nil || !next.(Model).syncing {
			t.Fatal("missing snapshot warning blocked recovery through the sync key")
		}
		syncFailure := errors.New("registry fetch failed")
		model = next.(Model)
		next, _ = model.Update(syncDoneMsg{err: syncFailure})
		model = next.(Model)
		actual.generation = model.requests.active(requestSnapshot)
		next, _ = model.Update(actual)
		model = next.(Model)
		if !errors.Is(errors.Join(model.errs...), syncFailure) || !errors.Is(errors.Join(model.warnings...), cache.ErrExactGitSnapshotUnavailable) || strings.Contains(model.driftStatusSegment(), "in sync") {
			t.Fatalf("failed sync lost its error or falsely verified state: errors=%v warnings=%v", model.errs, model.warnings)
		}
	}
	if after, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(after, lockBytes) {
		t.Fatalf("inspection changed lock: %v", err)
	}
	if after, err := os.ReadFile(configPath); err != nil || !bytes.Equal(after, configBytes) {
		t.Fatalf("inspection changed configuration: %v", err)
	}
	snapshot, err = cache.AcquireExactGit(source, source, second)
	if err != nil {
		t.Fatal(err)
	}
	recovered := readSnapshot(context.Background(), repo, home, catalogLoadCached)
	if len(recovered.errs) != 0 || recovered.driftAssessment != driftReady || len(recovered.resolved) != 1 || recovered.resolved[0].Name != "pinned" {
		t.Fatalf("TUI did not recover after acquisition: errors=%v drift=%v", recovered.errs, recovered.driftAssessment)
	}
	write(filepath.Join(snapshot, ".factory", "skills", "pinned", "SKILL.md"), []byte("tampered\n"))
	untrusted := readSnapshot(context.Background(), repo, home, catalogLoadCached)
	var lockErr *pipeline.LockViewError
	if !errors.As(errors.Join(untrusted.errs...), &lockErr) || lockErr.Code != "lock.snapshot-unavailable" || !errors.Is(lockErr, cache.ErrExactGitSnapshotUntrusted) || errors.Is(lockErr, cache.ErrExactGitSnapshotUnavailable) {
		t.Fatalf("tampered snapshot was not distinguishable from missing cache: %v", untrusted.errs)
	}
	t.Log("tampered snapshot shares lock.snapshot-unavailable code but has ErrExactGitSnapshotUntrusted cause; code-only downgrades are unsafe")
}
