package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestConfigSnapshotsTrackParsedIncludeBytes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", canonicalTempDir(t))
	repo := canonicalTempDir(t)
	root := filepath.Join(repo, ".agents", "config.yaml")
	fragment := filepath.Join(repo, ".agents", "fragment.yaml")
	writeConfig(t, root, "include: fragment.yaml\n")
	writeConfig(t, fragment, "harnesses: [claude]\n")
	opts := config.LoadEffectiveOptions{RepoRoot: repo, LocalConfigDir: repo}
	load := func(ctx context.Context, opts config.LoadEffectiveOptions) config.EffectiveConfig {
		return pipeline.LoadEffective(ctx, pipeline.ConfigLoadStrict, opts, nil)
	}
	loaded, before, err := loadStableEffectiveWith(context.Background(), opts, load)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, fragment, "harnesses: [codex]\n")
	if _, err := snapshotEffectiveConfigs(loaded, nil); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("stale parsed bytes accepted: %v", err)
	}
	_, after, err := loadStableEffectiveWith(context.Background(), opts, load)
	if err != nil {
		t.Fatal(err)
	}
	if sameConfigSnapshots(before, after) {
		t.Fatal("include-only change left snapshots equal")
	}
}

func TestPrepareRuntimeLocksValidatesIncludedReadSet(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, change := range []string{"unchanged", "edit", "delete", "retarget"} {
		t.Run(change, func(t *testing.T) {
			home := canonicalTempDir(t)
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			origin, working := createBareRepoWithWorkingClone(t, "include-cas.git")
			writeSkill(t, working, "review", "Review", nil)
			commitAndPushAll(t, working, "review")
			repo := canonicalTempDir(t)
			fragments := canonicalTempDir(t)
			leaf := filepath.Join(fragments, "leaf.yaml")
			body := "sources:\n  origin: {git: file://" + origin + ", ref: main}\n"
			writeConfig(t, leaf, body)
			other := filepath.Join(fragments, "other.yaml")
			writeConfig(t, other, body)
			alias := filepath.Join(fragments, "alias.yaml")
			if err := os.Symlink(leaf, alias); err != nil {
				t.Fatal(err)
			}
			middle := filepath.Join(fragments, "middle.yaml")
			writeConfig(t, middle, "include: alias.yaml\n")
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			writeConfig(t, configPath, "include: "+middle+"\nharnesses: [claude]\n")
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "include: "+middle+"\n")
			original, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(fragments, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(fragments, 0o755) })
			var concurrentBytes []byte
			commit := commitDependencyTransaction
			commitDependencyTransaction = func(ctx context.Context, state string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
				switch change {
				case "edit":
					writeConfig(t, leaf, "sources: {}\n")
				case "delete", "retarget":
					if err := os.Chmod(fragments, 0o755); err != nil {
						t.Fatal(err)
					}
					if change == "delete" {
						if err := os.Remove(leaf); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := os.Remove(alias); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(other, alias); err != nil {
							t.Fatal(err)
						}
					}
				}
				if change != "delete" {
					concurrentBytes, err = os.ReadFile(leaf)
					if err != nil {
						t.Fatal(err)
					}
				}
				return commit(ctx, state, desired, opts...)
			}
			t.Cleanup(func() { commitDependencyTransaction = commit })
			_, _, _, err = prepareRuntimeLocks(context.Background(), repo, repo, lockMode{})
			if change == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); err != nil {
					t.Fatal(err)
				}
			} else {
				if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
					t.Fatalf("error = %v, want generation conflict", err)
				}
				for _, root := range []string{repo, home} {
					if _, err := os.Stat(filepath.Join(root, ".agents", "lock.yaml")); !os.IsNotExist(err) {
						t.Fatalf("lock published on conflict: %v", err)
					}
				}
			}
			if got, err := os.ReadFile(configPath); err != nil || !bytes.Equal(got, original) {
				t.Fatalf("root changed: %q, %v", got, err)
			}
			if change == "delete" {
				if _, err := os.Stat(leaf); !os.IsNotExist(err) {
					t.Fatalf("deleted fragment restored: %v", err)
				}
			} else if got, err := os.ReadFile(leaf); err != nil || !bytes.Equal(got, concurrentBytes) {
				t.Fatalf("fragment edit overwritten: %q, %v", got, err)
			}
			if change == "retarget" {
				if target, err := os.Readlink(alias); err != nil || target != other {
					t.Fatalf("alias retarget overwritten: %s, %v", target, err)
				}
			}
			entries, err := os.ReadDir(fragments)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name()[0] == '.' {
					t.Fatalf("assertion created fragment sidecar: %s", entry.Name())
				}
			}
		})
	}
}

func TestPrepareRuntimeLocksIncludesStagedRootThroughAlias(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := canonicalTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	origin, working := createBareRepoWithWorkingClone(t, "include-overlay.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	repo := canonicalTempDir(t)
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "harnesses: [claude]\n")
	alias := filepath.Join(home, "repo-alias.yaml")
	if err := os.Symlink(configPath, alias); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "include: ["+alias+", "+configPath+"]\n")
	expected, err := dependencytxn.ReadGeneration(configPath)
	if err != nil {
		t.Fatal(err)
	}
	staged := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  origin: {git: file://" + origin + ", ref: main}\n")
	_, _, _, err = prepareRuntimeLocks(context.Background(), repo, repo, lockMode{stagedConfigEdits: []pipeline.StagedConfigEdit{{RequestedPath: configPath, Path: configPath, Expected: expected, Bytes: staged, Mode: 0o644}}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(configPath); err != nil || !bytes.Equal(got, staged) {
		t.Fatalf("staged config = %q, %v", got, err)
	}
	for _, root := range []string{home, repo} {
		if _, err := os.Stat(filepath.Join(root, ".agents", "lock.yaml")); err != nil {
			t.Fatalf("included overlay missing lock: %v", err)
		}
	}
}

func TestPrepareRuntimeLocksRejectsStagingGenerationRaces(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "staging-generation-race.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	tests := []struct {
		name     string
		initial  string
		current  string
		expected dependencytxn.Generation
	}{
		{name: "existing destination changed", initial: "\nharnesses: [claude]\n", current: "\nharnesses: [factory]\n"},
		{name: "absent destination created", current: "\nharnesses: [factory]\n", expected: dependencytxn.Generation{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := filepath.Join(canonicalTempDir(t), "repo")
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			if test.initial != "" {
				writeConfig(t, configPath, test.initial)
				var err error
				test.expected, err = dependencytxn.ReadGeneration(configPath)
				if err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				t.Fatal(err)
			}
			staged := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  origin: {git: file://" + origin + ", ref: main}\n")
			writeConfig(t, configPath, test.current)
			current, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			canonicalPath, err := canonicalizeFuturePath(configPath)
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, err = prepareRuntimeLocks(context.Background(), repo, repo, lockMode{stagedConfigEdits: []pipeline.StagedConfigEdit{{RequestedPath: configPath, Path: canonicalPath, Expected: test.expected, Bytes: staged, Mode: 0o644}}})
			if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
				t.Fatalf("error = %v, want generation conflict", err)
			}
			after, readErr := os.ReadFile(configPath)
			if readErr != nil || !bytes.Equal(after, current) {
				t.Fatalf("config changed on conflict: %q, %v", after, readErr)
			}
			if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
				t.Fatalf("lock published on conflict: %v", statErr)
			}
		})
	}
}

func TestPrepareRuntimeLocksRevalidatesPublishedLockGeneration(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "lock-generation-race.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	repo := filepath.Join(canonicalTempDir(t), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  origin: {git: file://"+origin+", ref: main}\n")
	_, _, validate, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if err := os.WriteFile(lockPath, []byte("newer lock layer\\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := validate(context.Background()); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("exact lock validation error = %v, want ErrGenerationConflict", err)
	}
}

func TestPrepareRuntimeLocksRejectsRetargetedStagedConfigSymlink(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := canonicalTempDir(t)
	first := filepath.Join(dir, "first", "config.yaml")
	second := filepath.Join(dir, "second", "config.yaml")
	writeConfig(t, first, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	writeConfig(t, second, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	before := make(map[string][]byte)
	for _, path := range []string{first, second} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = raw
	}
	requested := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(first, requested); err != nil {
		t.Fatal(err)
	}
	expected, err := dependencytxn.ReadGeneration(first)
	if err != nil {
		t.Fatal(err)
	}
	canonicalFirst, err := canonicalizeFuturePath(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(requested); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, requested); err != nil {
		t.Fatal(err)
	}
	staged := []byte("apiVersion: agent-marketplace/v4\nharnesses: [codex]\n")
	_, _, _, err = prepareRuntimeLocks(context.Background(), dir, dir, lockMode{stagedConfigEdits: []pipeline.StagedConfigEdit{{RequestedPath: requested, Path: canonicalFirst, Expected: expected, Bytes: staged, Mode: 0o644}}})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("error = %v, want generation conflict", err)
	}
	for _, path := range []string{first, second} {
		if raw, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(raw, before[path]) {
			t.Fatalf("destination changed at %s: %q, %v", path, raw, readErr)
		}
	}
}

func TestPrepareRuntimeLocksRejectsUnmatchedAndDuplicateStagedEdits(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := canonicalTempDir(t)
	unmatched := filepath.Join(canonicalTempDir(t), "config.yaml")
	source := t.TempDir()
	writeConfig(t, unmatched, "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: [skill/review]\n")
	canonical, err := canonicalizeFuturePath(unmatched)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := dependencytxn.ReadGeneration(canonical)
	if err != nil {
		t.Fatal(err)
	}
	edit := pipeline.StagedConfigEdit{RequestedPath: unmatched, Path: canonical, Expected: generation, Bytes: []byte("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\nresources:\n  local: [skill/changed]\n"), Mode: 0o644}
	for _, test := range []struct {
		name  string
		edits []pipeline.StagedConfigEdit
	}{
		{name: "unmatched", edits: []pipeline.StagedConfigEdit{edit}},
		{name: "duplicate", edits: []pipeline.StagedConfigEdit{edit, edit}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{stagedConfigEdits: test.edits})
			if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
				t.Fatalf("error = %v, want generation conflict", err)
			}
		})
	}
}

func TestRepositoryIgnoresDependencyTransactionLockFiles(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Clean(filepath.Join(root, "..", ".."))
	cmd := exec.Command("git", "-C", root, "check-ignore", "--no-index", "-q", ".agents/config.yaml.dependencytxn.lock")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dependency transaction lock is not ignored: %v\n%s", err, output)
	}
}

func TestPrepareRuntimeLocksRejectsRepositoryConfigAncestorSymlink(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "ancestor-symlink.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	repo := filepath.Join(canonicalTempDir(t), "repo")
	external := filepath.Join(canonicalTempDir(t), "external")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(external, "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  origin: {git: file://" + origin + ", ref: main}\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(repo, ".agents")); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{})
	if !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
	if got, readErr := os.ReadFile(configPath); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("external config changed: %q, %v", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(external, "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("external lock was published: %v", statErr)
	}
}

func TestPrepareRuntimeLocksRejectsStagedConfigAncestorSymlink(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "staged-ancestor-symlink.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	repo := filepath.Join(canonicalTempDir(t), "repo")
	external := filepath.Join(canonicalTempDir(t), "external")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	requested := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.Symlink(external, filepath.Join(repo, ".agents")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(external, "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	if err := os.WriteFile(destination, original, 0o644); err != nil {
		t.Fatal(err)
	}
	expected, err := dependencytxn.ReadGeneration(destination)
	if err != nil {
		t.Fatal(err)
	}
	staged := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  origin: {git: file://" + origin + ", ref: main}\n")
	_, _, _, err = prepareRuntimeLocks(context.Background(), repo, repo, lockMode{stagedConfigEdits: []pipeline.StagedConfigEdit{{RequestedPath: requested, Path: destination, Expected: expected, Bytes: staged, Mode: 0o644}}})
	if !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("external staged config changed: %q, %v", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(external, "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("external staged lock was published: %v", statErr)
	}
}

func TestPrepareRuntimeLocksRejectsGeneratedLockSymlink(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := canonicalTempDir(t)
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	target := filepath.Join(t.TempDir(), "lock.yaml")
	original := []byte("user-owned lock target\n")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, lockPath); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{})
	if !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("symlink target changed: %q, %v", got, readErr)
	}
	info, lstatErr := os.Lstat(lockPath)
	if lstatErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("generated lock symlink was replaced: info=%v err=%v", info, lstatErr)
	}
}

func TestPrepareRuntimeLocksRejectsWritableLayerDestinationAlias(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := canonicalTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	target := filepath.Join(canonicalTempDir(t), "config.yaml")
	writeConfig(t, target, "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: "+t.TempDir()+"}\nresources:\n  local: [skill/review]\n")
	repo := filepath.Join(canonicalTempDir(t), "repo")
	for _, link := range []string{filepath.Join(home, ".agents", "config.yaml"), filepath.Join(repo, ".agents", "config.yaml")} {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{})
	if !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
}

func TestPrepareRuntimeLocksCommitsStagedConfigAndLockTogether(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "staged-config-lock.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	repo := filepath.Join(canonicalTempDir(t), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  origin: {git: file://"+origin+", ref: main}\n")
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	staged := append(append([]byte(nil), original...), []byte("resources:\n  origin: [skill/review]\n")...)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")

	originalCommit := commitDependencyTransaction
	commitDependencyTransaction = func(_ context.Context, _ string, desired []dependencytxn.DesiredFile, _ ...dependencytxn.Option) (dependencytxn.Result, error) {
		foundConfig, foundLock := false, false
		for _, file := range desired {
			switch filepath.Base(file.Path) {
			case "config.yaml":
				foundConfig = bytes.Equal(file.Bytes, staged)
			case "lock.yaml":
				foundLock = len(file.Bytes) > 0
			}
		}
		if !foundConfig || !foundLock {
			t.Fatalf("desired transaction missing config or lock: %#v", desired)
		}
		return dependencytxn.Result{}, dependencytxn.ErrGenerationConflict
	}
	t.Cleanup(func() { commitDependencyTransaction = originalCommit })

	generation, err := dependencytxn.ReadGeneration(configPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalPath, err := canonicalizeFuturePath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = prepareRuntimeLocks(context.Background(), repo, repo, lockMode{stagedConfigEdits: []pipeline.StagedConfigEdit{{RequestedPath: configPath, Path: canonicalPath, Expected: generation, Bytes: staged, Mode: 0o644}}})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("error = %v, want generation conflict", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("config published on conflict: %q, %v", after, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock published on conflict: %v", err)
	}
}
