package fsboundary_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"golang.org/x/sys/unix"
)

func TestSessionVerificationReusesBuiltSourceReads(t *testing.T) {
	snapshot := t.TempDir()
	writeSessionFixture(t, snapshot)
	var mu sync.Mutex
	reads := map[string]int{}
	restore := fsboundary.ReplaceOpenatForTest(func(dirfd int, name string, flags int, mode uint32) (int, error) {
		fd, err := unix.Openat(dirfd, name, flags, mode)
		if err == nil && flags&unix.O_DIRECTORY == 0 && flags&unix.O_NOFOLLOW != 0 && (name == "SKILL.md" || name == "run.sh") {
			var stat unix.Stat_t
			if unix.Fstat(fd, &stat) == nil && stat.Mode&unix.S_IFMT == unix.S_IFREG {
				mu.Lock()
				reads[name]++
				mu.Unlock()
			}
		}
		return fd, err
	})
	defer restore()

	ctx, closeSession := sourcerevision.WithSession(context.Background())
	defer func() {
		if err := closeSession(); err != nil {
			t.Errorf("close source-revision session: %v", err)
		}
	}()
	built, err := sourcerevision.BuildWithContext(ctx, sourcerevision.Input{SnapshotRoot: snapshot, RootPath: ".", SourceHarness: "factory"})
	if err != nil {
		t.Fatalf("BuildWithContext: %v", err)
	}
	if built.Catalog == nil || built.SourceHarness != "factory" {
		t.Fatalf("built catalog = %+v, source harness = %q", built.Catalog, built.SourceHarness)
	}
	mu.Lock()
	afterBuild := map[string]int{"SKILL.md": reads["SKILL.md"], "run.sh": reads["run.sh"]}
	mu.Unlock()
	verified, err := sourcerevision.VerifyLockedGitRootWithContext(ctx, snapshot, sessionLockRoot(built), nil)
	if err != nil {
		t.Fatalf("VerifyLockedGitRootWithContext: %v", err)
	}
	if verified.Catalog == nil || len(verified.Catalog.Entries) != 1 || verified.Catalog.Entries[0].Name != "probe" {
		t.Fatalf("verified catalog entries = %+v, want probe", verified.Catalog)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(reads, afterBuild) {
		t.Fatalf("verification reopened source payloads: before=%v after=%v", afterBuild, reads)
	}
	for _, name := range []string{"SKILL.md", "run.sh"} {
		if reads[name] == 0 || reads[name] > 2 {
			t.Fatalf("successful regular-file payload opens for %s = %d, want 1 or 2", name, reads[name])
		}
	}
}

func TestSessionVerificationRefusesSourceGenerationDrift(t *testing.T) {
	for _, name := range []string{"primary edit", "support edit", "addition", "deletion", "root replacement"} {
		t.Run(name, func(t *testing.T) {
			snapshot := t.TempDir()
			writeSessionFixture(t, snapshot)
			ctx, closeSession := sourcerevision.WithSession(context.Background())
			defer func() {
				if err := closeSession(); err != nil {
					t.Errorf("close source-revision session: %v", err)
				}
			}()
			built, err := sourcerevision.BuildWithContext(ctx, sourcerevision.Input{SnapshotRoot: snapshot, RootPath: ".", SourceHarness: "factory"})
			if err != nil {
				t.Fatalf("BuildWithContext: %v", err)
			}
			mutateSessionSource(t, snapshot, name)
			got, err := sourcerevision.VerifyLockedGitRootWithContext(ctx, snapshot, sessionLockRoot(built), nil)
			if !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("verification error = %v, want generation conflict", err)
			}
			if !reflect.DeepEqual(got, sourcerevision.Result{}) {
				t.Fatalf("verification returned a result after source drift: %+v", got)
			}
		})
	}
}

func mutateSessionSource(t *testing.T, root, change string) {
	t.Helper()
	switch change {
	case "primary edit":
		writeSessionFile(t, filepath.Join(root, ".factory", "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Probe\n---\nchanged primary\n")
	case "support edit":
		writeSessionFile(t, filepath.Join(root, ".factory", "skills", "probe", "scripts", "run.sh"), "#!/bin/sh\necho changed\n")
	case "addition":
		writeSessionFile(t, filepath.Join(root, "new-observation.txt"), "new metadata entry\n")
	case "deletion":
		if err := os.Remove(filepath.Join(root, ".factory", "skills", "probe", "scripts", "run.sh")); err != nil {
			t.Fatal(err)
		}
	case "root replacement":
		if err := os.Rename(root, root+".moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
		writeSessionFixture(t, root)
	}
}

func TestSessionDoesNotReuseResultForDifferentHarness(t *testing.T) {
	snapshot := t.TempDir()
	writeSessionFixture(t, snapshot)
	ctx, closeSession := sourcerevision.WithSession(context.Background())
	defer func() {
		if err := closeSession(); err != nil {
			t.Errorf("close source-revision session: %v", err)
		}
	}()
	built, err := sourcerevision.BuildWithContext(ctx, sourcerevision.Input{SnapshotRoot: snapshot, RootPath: ".", SourceHarness: "factory"})
	if err != nil {
		t.Fatal(err)
	}
	writeSessionFile(t, filepath.Join(snapshot, ".claude", "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Probe\n---\nclaude-only content\n")
	locked := sessionLockRoot(built)
	locked.SourceHarness = "claude"
	got, err := sourcerevision.VerifyLockedGitRootWithContext(ctx, snapshot, locked, nil)
	if !errors.Is(err, sourcerevision.ErrManifestMismatch) {
		t.Fatalf("different-harness verification = %v, want manifest mismatch", err)
	}
	if !reflect.DeepEqual(got, sourcerevision.Result{}) {
		t.Fatalf("different-harness verification returned the cached result: %+v", got)
	}
}

func TestSessionCheckpointOverridesMemoizedExactGitVerification(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	source := filepath.Join(t.TempDir(), "registry")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSessionFixture(t, source)
	runSessionGit(t, source, "init", "--initial-branch=main")
	runSessionGit(t, source, "add", ".")
	runSessionGit(t, source, "-c", "user.name=Session Test", "-c", "user.email=session@example.invalid", "commit", "-m", "seed")
	origin := "file://" + filepath.ToSlash(source)
	cacheContext := cache.WithFreshExactGitVerificationScope(context.Background())
	oid, err := cache.ResolveExactGitRefWithContext(cacheContext, origin, origin, "main")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.AcquireExactGitWithRootContext(cacheContext, origin, origin, oid, ".")
	if err != nil {
		t.Fatal(err)
	}
	ctx, closeSession := sourcerevision.WithSession(cacheContext)
	defer func() {
		if err := closeSession(); err != nil {
			t.Errorf("close source-revision session: %v", err)
		}
	}()
	built, err := sourcerevision.BuildWithContext(ctx, sourcerevision.Input{SnapshotRoot: snapshot, RootPath: ".", SourceHarness: "factory"})
	if err != nil {
		t.Fatal(err)
	}
	locked := sessionLockRoot(built)
	if err := cache.WithExactGitReadOnlyRoot(ctx, origin, oid, ".", func(path string) error {
		if filepath.Clean(path) != filepath.Clean(snapshot) {
			return fmt.Errorf("memoized snapshot path = %q, want %q", path, snapshot)
		}
		return nil
	}); err != nil {
		t.Fatalf("initial exact snapshot verification: %v", err)
	}
	writeSessionFile(t, filepath.Join(snapshot, ".factory", "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Probe\n---\ntampered\n")
	if err := cache.WithExactGitReadOnlyRoot(ctx, origin, oid, ".", func(string) error { return nil }); err != nil {
		t.Fatalf("scoped exact-Git verification did not use its recorded success: %v", err)
	}
	got, err := sourcerevision.VerifyLockedGitRootWithContext(ctx, snapshot, locked, nil)
	if !errors.Is(err, fsboundary.ErrGenerationConflict) || !reflect.DeepEqual(got, sourcerevision.Result{}) {
		t.Fatalf("verification after memoized snapshot acceptance = %+v, %v; want empty result and generation conflict", got, err)
	}
}

func sessionLockRoot(result sourcerevision.Result) lockfile.GitRoot {
	root := lockfile.GitRoot{
		Path:           ".",
		SourceHarness:  result.SourceHarness,
		RevisionSchema: result.Manifest.SchemaVersion,
		RevisionDigest: lockfile.Digest(result.Manifest.RevisionDigest),
	}
	for _, resource := range result.Manifest.Resources {
		if resource.ArchiveDigest == "" {
			continue
		}
		locked := lockfile.GitResource{
			Kind:          resource.Kind,
			Name:          resource.Name,
			ContentDigest: lockfile.Digest(resource.ContentDigest),
			ArchiveDigest: lockfile.Digest(resource.ArchiveDigest),
		}
		for name, digest := range resource.SupportFiles {
			if locked.SupportFiles == nil {
				locked.SupportFiles = make(map[string]lockfile.Digest)
			}
			locked.SupportFiles[name] = lockfile.Digest(digest)
		}
		root.Resources = append(root.Resources, locked)
	}
	return root
}

func writeSessionFixture(t *testing.T, root string) {
	t.Helper()
	writeSessionFile(t, filepath.Join(root, ".factory", "skills", "probe", "SKILL.md"), "---\nname: probe\ndescription: Probe\n---\ninitial primary\n")
	writeSessionFile(t, filepath.Join(root, ".factory", "skills", "probe", "scripts", "run.sh"), "#!/bin/sh\necho initial\n")
}

func writeSessionFile(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runSessionGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
