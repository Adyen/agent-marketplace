package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestPrepareRuntimeLocksRejectsStagingGenerationRaces(t *testing.T) {
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
			repo := filepath.Join(t.TempDir(), "repo")
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
			staged := []byte("apiVersion: agent-marketplace/v2\nharnesses: [claude]\nregistries:\n  - url: " + origin + "\n    ref: main\n")
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

func TestPrepareRuntimeLocksRejectsRetargetedStagedConfigSymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	first := filepath.Join(dir, "first", "config.yaml")
	second := filepath.Join(dir, "second", "config.yaml")
	writeConfig(t, first, "\nharnesses: [claude]\n")
	writeConfig(t, second, "\nharnesses: [factory]\n")
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
	staged := []byte("apiVersion: agent-marketplace/v2\nharnesses: [claude]\n")
	_, _, _, err = prepareRuntimeLocks(context.Background(), dir, dir, lockMode{stagedConfigEdits: []pipeline.StagedConfigEdit{{RequestedPath: requested, Path: canonicalFirst, Expected: expected, Bytes: staged, Mode: 0o644}}})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("error = %v, want generation conflict", err)
	}
	for _, path := range []string{first, second} {
		if raw, readErr := os.ReadFile(path); readErr != nil || bytes.Contains(raw, []byte("registries:")) {
			t.Fatalf("destination changed at %s: %q, %v", path, raw, readErr)
		}
	}
}

func TestPrepareRuntimeLocksRejectsUnmatchedAndDuplicateStagedEdits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	unmatched := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, unmatched, "\nskills: [review]\n")
	canonical, err := canonicalizeFuturePath(unmatched)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := dependencytxn.ReadGeneration(canonical)
	if err != nil {
		t.Fatal(err)
	}
	edit := pipeline.StagedConfigEdit{RequestedPath: unmatched, Path: canonical, Expected: generation, Bytes: []byte("apiVersion: agent-marketplace/v2\nskills: [changed]\n"), Mode: 0o644}
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

func TestPrepareRuntimeLocksRejectsGeneratedLockSymlink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	target := filepath.Join(t.TempDir(), "lock.yaml")
	original := []byte("user-owned lock target\\n")
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, target, "\nskills: [review]\n")
	repo := filepath.Join(t.TempDir(), "repo")
	for _, link := range []string{filepath.Join(home, ".agents", "config.yaml"), filepath.Join(repo, ".agents", "config.yaml")} {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("error = %v, want generation conflict", err)
	}
}

func TestPrepareRuntimeLocksCommitsStagedConfigAndLockTogether(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "staged-config-lock.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "\nharnesses: [claude]\nregistries:\n  - url: "+origin+"\n    ref: main\n")
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	staged := append(append([]byte(nil), original...), []byte("skills: [review]\n")...)
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
