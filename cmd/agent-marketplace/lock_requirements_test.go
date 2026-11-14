package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

func TestAcquireGitRequirementsPreservesPolicies(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, seeded := setupGitRequirementsFixture(t)
	if seeded.Roots[0].RevisionDigest != "sha256:93bb84495b5553bcc91bb8caccc7001da2f20190b7036de484e6efe43bc3fc91" {
		t.Fatalf("changed frozen fixture revision: %+v", seeded.Roots[0])
	}
	for _, tc := range []struct {
		name     string
		policies []string
		roots    []string
	}{
		{"implicit-explicit", []string{"", "claude"}, []string{"claude"}},
		{"explicit-implicit", []string{"claude", ""}, []string{"claude"}},
		{"distinct-harnesses", []string{"claude", "factory"}, []string{"claude", "factory"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(previous) })
			var entries []config.RegistryEntry
			for _, hint := range tc.policies {
				entries = append(entries, config.RegistryEntry{URL: seeded.URL, Ref: seeded.Ref, SourceHarness: hint})
			}
			got, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries, nil)
			if err != nil || got.registry.URL != seeded.URL || got.registry.Ref != seeded.Ref || got.registry.Commit != seeded.Commit || len(got.registry.Roots) != len(tc.roots) {
				t.Fatalf("policy acquisition: %v, registry=%+v", err, got)
			}
			for i, harness := range tc.roots {
				want := seeded.Roots[0]
				want.SourceHarness = harness
				if !reflect.DeepEqual(got.registry.Roots[i], want) {
					t.Fatalf("changed source inventory: %+v; want %+v", got.registry.Roots[i], want)
				}
			}
			if builds := strings.Count(logs.String(), `msg="built direct-Git source revision"`); builds != 2 {
				t.Fatalf("requested policies must be independently built: got %d\n%s", builds, logs.String())
			}
		})
	}
}

func TestAcquireGitRequirementsBuildsDuplicatesOnce(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, seeded := setupGitRequirementsFixture(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, hint := range []string{"", "claude"} {
		entries := make([]config.RegistryEntry, 32)
		for i := range entries {
			path := ""
			if i%2 == 0 {
				path = "."
			}
			entries[i] = config.RegistryEntry{URL: seeded.URL, Ref: seeded.Ref, Path: path, SourceHarness: hint}
		}
		for range 2 {
			logs.Reset()
			got, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries, nil)
			if err != nil || !reflect.DeepEqual(got.registry, seeded) {
				t.Fatalf("duplicate acquisition changed exact inventory: %v, registry=%+v", err, got.registry)
			}
			if builds := strings.Count(logs.String(), `msg="built direct-Git source revision"`); builds != 1 {
				t.Fatalf("duplicate requirements built %d times, want one per invocation", builds)
			}
		}
	}
}

func TestAcquireGitRequirementsRetainsRefusals(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, seeded := setupGitRequirementsFixture(t)
	for _, tc := range []struct {
		name  string
		entry config.RegistryEntry
		want  error
	}{
		{"path-escape", config.RegistryEntry{Path: "../outside", SourceHarness: "claude"}, sourcerevision.ErrPathEscape},
		{"invalid-policy", config.RegistryEntry{SourceHarness: "not-a-harness"}, sourcerevision.ErrCatalog},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := []config.RegistryEntry{{SourceHarness: "claude"}, tc.entry}
			got, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries, nil)
			if !errors.Is(err, tc.want) || len(got.registry.Roots) != 0 || got.registry.Commit.String() != "" {
				t.Fatalf("changed fail-closed refusal: registry=%+v, %v; want %v", got, err, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireLockedRegistry(ctx, seeded.URL, seeded.URL, seeded.Ref, []config.RegistryEntry{{}}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("changed cancellation: %v", err)
	}
}

func TestAcquireGitRequirementsPreservesDifferentRoots(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, seeded := setupGitRequirementsFixture(t)
	entries := []config.RegistryEntry{{Path: ".", SourceHarness: "claude"}, {Path: "skills", SourceHarness: "claude"}, {Path: "skills", SourceHarness: "claude"}}
	got, err := acquireLockedRegistry(context.Background(), seeded.URL, seeded.URL, seeded.Ref, entries, nil)
	if err != nil || got.registry.Commit != seeded.Commit || len(got.registry.Roots) != 2 || !reflect.DeepEqual(got.registry.Roots[0], seeded.Roots[0]) {
		t.Fatalf("different-root acquisition: %v, registry=%+v", err, got)
	}
	second := got.registry.Roots[1]
	if second.Path != "skills" || second.SourceHarness != "claude" || second.RevisionSchema != "v4" || len(second.Resources) != 1 || second.Resources[0].Name != "review-0" {
		t.Fatalf("distinct root was conflated or lost: %+v", second)
	}
}

func TestLockConcurrentRefsReuseSourceRevision(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "same-oid.git")
	for i := range 32 {
		writeSkill(t, working, fmt.Sprintf("probe-%d", i), "Probe", nil)
	}
	commitAndPushAll(t, working, "seed")
	runSyncBenchmarkGit(t, origin, "update-ref", "refs/heads/other", "refs/heads/main")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  first: {git: file://%s, ref: main}
  second: {git: file://%s, ref: other}
`, origin, origin))
	withWorkingDir(t, repo)
	logPath := filepath.Join(t.TempDir(), "lock.jsonl")
	t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "true")
	t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "debug")
	t.Setenv("AGENT_MARKETPLACE_LOG_FILE", logPath)
	var arrived atomic.Int32
	start := make(chan struct{})
	previous := lockSnapshotAcquiredHook
	lockSnapshotAcquiredHook = func() {
		if arrived.Add(1) == 2 {
			close(start)
		}
		select {
		case <-start:
		case <-time.After(5 * time.Second):
			t.Error("second acquisition did not reach the existing hook")
		}
	}
	t.Cleanup(func() { lockSnapshotAcquiredHook = previous })
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("lock: %v\n%s", err, output)
	}
	doc := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if len(doc.Registries) != 2 || doc.Registries[0].Commit.String() == "" || doc.Registries[0].Commit != doc.Registries[1].Commit {
		t.Fatalf("fixture did not produce two refs of the same commit: %+v", doc)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(logs), `"msg":"built direct-Git source revision"`); count != 1 {
		t.Fatalf("same commit/root was built %d times, want one\n%s", count, logs)
	}
}

func TestSyncRepeatedGitRequirementsKeepsExactLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, seeded := setupGitRequirementsFixture(t)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	body := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n"
	for _, alias := range []string{"first", "second", "third"} {
		body += "  " + alias + ": {git: " + seeded.URL + ", ref: main}\n"
	}
	body += "resources:\n  first: [skill/review-0]\n  second: [skill/review-0]\n  third: [skill/review-0]\n"
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), body)
	logPath := filepath.Join(t.TempDir(), "update.jsonl")
	t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "true")
	t.Setenv("AGENT_MARKETPLACE_LOG_FILE", logPath)
	t.Setenv("AGENT_MARKETPLACE_LOG_LEVEL", "debug")
	for _, args := range [][]string{{"update"}, {"sync", "--locked"}, {"sync", "--check"}} {
		if args[0] != "update" {
			if err := os.WriteFile(logPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		if args[0] == "update" {
			logs, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			acquisition, verification, reconciled := strings.Cut(string(logs), `"msg":"lock layer reconciled"`)
			buildRecord := `"msg":"built direct-Git source revision"`
			if builds := strings.Count(acquisition, buildRecord); !reconciled || builds != 1 {
				t.Fatalf("update acquisition: reconciled=%t, builds=%d; want one before reconciliation", reconciled, builds)
			}
			verification, _, verified := strings.Cut(verification, `"msg":"lock snapshot verification completed"`)
			if builds := strings.Count(verification, buildRecord); !verified || builds != 0 {
				t.Fatalf("update verification: completed=%t, builds=%d; want no repeated source build", verified, builds)
			}
			doc, err := lockfile.Read(lockPath)
			if err != nil || len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 {
				t.Fatalf("updated lock inventory: %v, doc=%+v", err, doc)
			}
			oid, err := cache.ParseGitOID(seeded.Commit.String())
			if err != nil {
				t.Fatal(err)
			}
			var expected lockfile.GitRoot
			err = cache.WithExactGitReadOnlyRoot(context.Background(), seeded.URL, oid, ".", func(snapshot string) error {
				fresh, buildErr := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot, RootPath: ".", SourceHarness: seeded.Roots[0].SourceHarness})
				if buildErr != nil {
					return buildErr
				}
				expected = lockfile.GitRoot{
					Path:           ".",
					SourceHarness:  fresh.SourceHarness,
					RevisionSchema: fresh.Manifest.SchemaVersion,
					RevisionDigest: lockfile.Digest(fresh.Manifest.RevisionDigest),
					Resources:      revisionResources(fresh),
				}
				return nil
			})
			if err != nil || !reflect.DeepEqual(doc.Registries[0].Roots[0], expected) {
				t.Fatalf("updated lock root differs from a fresh build: %v, got=%+v want=%+v", err, doc.Registries[0].Roots[0], expected)
			}
			secondOperationLogs, secondOperationErr := func() (string, error) {
				var logs bytes.Buffer
				previousLogger := slog.Default()
				defer slog.SetDefault(previousLogger)
				slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
				_, _, _, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{locked: true})
				return logs.String(), err
			}()
			if secondOperationErr != nil {
				t.Fatalf("second independent lock operation: %v", secondOperationErr)
			}
			if builds := strings.Count(secondOperationLogs, `msg="built direct-Git source revision"`); builds != 1 {
				t.Fatalf("second independent lock operation built %d times, want a fresh source ingestion\n%s", builds, secondOperationLogs)
			}
		}
		after, err := os.ReadFile(lockPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("repeated requirements changed committed lock: %v\n%s", err, after)
		}
	}
	assertMaterializedSkillContains(t, repo, "review-0", "Review")
}
