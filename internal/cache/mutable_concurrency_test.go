package cache

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func mutableLocalRegistry(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	origin := filepath.Join(root, "origin.git")
	for i := 0; i < 16; i++ {
		dir := filepath.Join(work, "skills", fmt.Sprintf("skill-%02d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := fmt.Sprintf("---\nname: skill-%02d\ndescription: Local registry fixture\n---\n%s", i, strings.Repeat("Registry instruction.\n", 200))
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "--initial-branch=main", work},
		{"-C", work, "add", "."},
		{"-C", work, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "seed"},
		{"clone", "--bare", work, origin},
	} {
		if output, err := execGit("", args); err != nil {
			t.Fatalf("seed registry: %v: %s", err, output)
		}
	}
	return "file://" + origin
}

func isolateMutableGit(t testing.TB) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
}

// leaseConflictWindow is how long a conflicting acquisition is attempted while
// the holder keeps its lease. The holder releases only after that attempt
// returns, so a correct implementation can never admit it however slowly the
// host runs; scheduling delay only shortens the observation.
const leaseConflictWindow = 100 * time.Millisecond

// mutableProgressBound bounds every wait for an operation that must complete,
// such as an independent lease acquisition. It is a hang detector, not a speed
// requirement: it only turns an operation that never completes (for example a
// lease wrongly excluded by an unrelated one) into a test failure, so it sits
// far above any scheduling delay on a loaded host.
const mutableProgressBound = 30 * time.Second

func awaitMutable[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	timer := time.NewTimer(mutableProgressBound)
	defer timer.Stop()
	select {
	case value := <-ch:
		return value
	case <-timer.C:
		t.Fatal("timed out waiting for cache operation")
		var zero T
		return zero
	}
}

// awaitMutableFile reports whether path exists before ctx is done.
func awaitMutableFile(ctx context.Context, path string) bool {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func TestMutableLeaseSubprocess(t *testing.T) {
	if os.Getenv("MUTABLE_TEST_HOME") == "" {
		return
	}
	t.Setenv("HOME", os.Getenv("MUTABLE_TEST_HOME"))
	operation, err := strconv.Atoi(os.Getenv("MUTABLE_TEST_OPERATION"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = withRegistryLocks(context.Background(), strings.Split(os.Getenv("MUTABLE_TEST_URLS"), "\n"), operation, func() (struct{}, error) {
		fmt.Println("ready")
		_, err := io.Copy(io.Discard, os.Stdin)
		return struct{}{}, err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func holdMutableLease(t *testing.T, subprocess bool, urls []string, operation int) func() {
	t.Helper()
	var release func()
	if subprocess {
		// The holder outlives a contender's mutableProgressBound, which starts
		// only once the holder is ready, so a contender that wrongly blocks
		// fails at that bound while the lease is still held instead of being
		// admitted when the holder is killed. The holder exits cleanly only
		// after release closes its stdin, which release checks.
		ctx, cancel := context.WithTimeout(context.Background(), 2*mutableProgressBound)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMutableLeaseSubprocess$")
		cmd.Env = append(os.Environ(), "MUTABLE_TEST_HOME="+os.Getenv("HOME"), "MUTABLE_TEST_OPERATION="+strconv.Itoa(operation), "MUTABLE_TEST_URLS="+strings.Join(urls, "\n"))
		stdin, err := cmd.StdinPipe()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		release = func() {
			_ = stdin.Close()
			if err := cmd.Wait(); err != nil {
				t.Error(err)
			}
			cancel()
		}
		var once sync.Once
		done := func() { once.Do(release) }
		t.Cleanup(done)
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil || line != "ready\n" {
			t.Fatalf("subprocess lease: %q, %v", line, err)
		}
		return done
	}
	entered, unblock, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := withRegistryLocks(context.Background(), urls, operation, func() (struct{}, error) {
			close(entered)
			<-unblock
			return struct{}{}, nil
		})
		finished <- err
	}()
	var once sync.Once
	release = func() {
		once.Do(func() {
			close(unblock)
			if err := awaitMutable(t, finished); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(release)
	awaitMutable(t, entered)
	return release
}

func TestMutableRegistryLeaseExclusion(t *testing.T) {
	for _, subprocess := range []bool{false, true} {
		for _, tc := range []struct {
			name              string
			holder, contender int
			same, blocked     bool
		}{
			{"independent-writers", lockfileExclusive, lockfileExclusive, false, false},
			{"same-writers", lockfileExclusive, lockfileExclusive, true, true},
			{"discovery-blocks-checkout", lockfileShared, lockfileExclusive, true, true},
			{"checkout-blocks-discovery", lockfileExclusive, lockfileShared, true, true},
			{"concurrent-discovery", lockfileShared, lockfileShared, true, false},
			{"unrelated-discovery", lockfileShared, lockfileExclusive, false, false},
		} {
			t.Run(fmt.Sprintf("subprocess=%t/%s", subprocess, tc.name), func(t *testing.T) {
				isolateMutableGit(t)
				first, second := "https://example.invalid/a.git", "https://example.invalid/b.git"
				if tc.same {
					second = first
				}
				release := holdMutableLease(t, subprocess, []string{first}, tc.holder)
				// Both attempts run while the holder keeps its lease. A
				// conflicting one is observed for leaseConflictWindow; one that
				// must succeed has to succeed while the lease is held, but not
				// within any particular time.
				bound := mutableProgressBound
				if tc.blocked {
					bound = leaseConflictWindow
				}
				ctx, cancel := context.WithTimeout(context.Background(), bound)
				defer cancel()
				called := false
				consume := func() (struct{}, error) { called = true; return struct{}{}, nil }
				_, err := withRegistryLocks(ctx, []string{second}, tc.contender, consume)
				if tc.blocked {
					if !errors.Is(err, context.DeadlineExceeded) || called {
						t.Fatalf("conflicting lease: called=%t err=%v", called, err)
					}
				} else if err != nil || !called {
					t.Fatalf("independent lease: called=%t err=%v", called, err)
				}
				release()
				ctx, cancelRetry := context.WithTimeout(context.Background(), mutableProgressBound)
				defer cancelRetry()
				if _, err := withRegistryLocks(ctx, []string{second}, tc.contender, consume); err != nil {
					t.Fatalf("lease leaked after release/cancellation: %v", err)
				}
			})
		}
	}
}

func TestMutableRegistryLeaseBlocksGCAndPersists(t *testing.T) {
	for _, subprocess := range []bool{false, true} {
		for _, operation := range []int{lockfileShared, lockfileExclusive} {
			t.Run(fmt.Sprintf("subprocess=%t/operation=%d", subprocess, operation), func(t *testing.T) {
				isolateMutableGit(t)
				url := "https://example.invalid/stale.git"
				marker := filepath.Join(RegistryCachePath(url, "feature/nested"), "keep")
				writeFile(t, marker, "held")
				release := holdMutableLease(t, subprocess, []string{url}, operation)
				lockPath := CacheDir() + ".registry-" + HashURL(url) + ".lock"
				before, err := os.Stat(lockPath)
				if err != nil {
					t.Fatal(err)
				}
				reader := staticReachability(nil)
				ctx, cancel := context.WithTimeout(context.Background(), leaseConflictWindow)
				defer cancel()
				if _, err := GarbageCollect(ctx, false, reader); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("GC during lease: %v", err)
				}
				assertFileContents(t, marker, "held")
				release()
				if _, err := GarbageCollect(context.Background(), false, reader); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("GC failed to remove unreachable cache: %v", err)
				}
				after, err := os.Stat(lockPath)
				if err != nil || !os.SameFile(before, after) {
					t.Fatalf("GC replaced/deleted lock inode: %v", err)
				}
			})
		}
	}
}

func TestMutableRegistryLeaseCancellationReleasesEarlierKeys(t *testing.T) {
	isolateMutableGit(t)
	first, second := "https://example.invalid/a.git", "https://example.invalid/b.git"
	if HashURL(first) > HashURL(second) {
		first, second = second, first
	}
	release := holdMutableLease(t, false, []string{second}, lockfileExclusive)
	ctx, cancel := context.WithTimeout(context.Background(), leaseConflictWindow)
	defer cancel()
	consume := func() (struct{}, error) { return struct{}{}, nil }
	if _, err := WithRegistryReadLockWithContext(ctx, []string{second, first, first}, consume); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("partial acquisition: %v", err)
	}
	// A leaked lease on first is never released, so this attempt fails at the
	// bound; a correct implementation succeeds however slowly the host runs.
	ctx, cancelRetry := context.WithTimeout(context.Background(), mutableProgressBound)
	defer cancelRetry()
	if _, err := withRegistryLocks(ctx, []string{first}, lockfileExclusive, consume); err != nil {
		t.Fatalf("earlier key leaked: %v", err)
	}
	release()
	ctx, cancelReleased := context.WithTimeout(context.Background(), mutableProgressBound)
	defer cancelReleased()
	if _, err := withRegistryLocks(ctx, []string{second, first, first}, lockfileExclusive, consume); err != nil {
		t.Fatalf("duplicate/unsorted key acquisition: %v", err)
	}
}

func TestMutableRegistryLeaseNamespaceCancellation(t *testing.T) {
	isolateMutableGit(t)
	release, err := acquireCacheFileLockWithContext(context.Background(), lockfileExclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), leaseConflictWindow)
	defer cancel()
	_, err = WithRegistryReadLockWithContext(ctx, []string{"https://example.invalid/a.git"}, func() (struct{}, error) {
		t.Fatal("namespace exclusion bypassed")
		return struct{}{}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(CacheDir() + ".registry-*.lock")
	if err != nil || len(entries) != 0 {
		t.Fatalf("created key state before namespace acquisition: %v, %v", entries, err)
	}
}

func TestMutableCancelledLeaseDoesNotCreateState(t *testing.T) {
	isolateMutableGit(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WithRegistryReadLockWithContext(ctx, []string{"https://example.invalid/a.git"}, func() (struct{}, error) {
		t.Fatal("cancelled callback ran")
		return struct{}{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(os.Getenv("HOME"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancelled lease created state: %v, %v", entries, err)
	}
}

func TestMutableIndependentRegistryJobsOverlap(t *testing.T) {
	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "warm"
		}
		t.Run(name, func(t *testing.T) {
			isolateMutableGit(t)
			first, second := mutableLocalRegistry(t), mutableLocalRegistry(t)
			if warm {
				for _, url := range []string{first, second} {
					if _, err := CloneOrPull(url, "main"); err != nil {
						t.Fatal(err)
					}
				}
			}
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			barrier := t.TempDir()
			bin := t.TempDir()
			writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\nblock=0\nobserve=0\nfor arg do\n  if [ \"$arg\" = \"$MUTABLE_BLOCK_URL\" ] || { [ \"$arg\" = fetch ] && [ \"$PWD\" = \"$MUTABLE_BLOCK_PATH\" ]; }; then block=1; fi\n  if [ \"$arg\" = \"$MUTABLE_OBSERVE_URL\" ] || { [ \"$arg\" = fetch ] && [ \"$PWD\" = \"$MUTABLE_OBSERVE_PATH\" ]; }; then observe=1; fi\ndone\nif [ \"$observe\" = 1 ]; then : > \"$MUTABLE_BARRIER/observed\"; fi\nif [ \"$block\" = 1 ]; then\n  : > \"$MUTABLE_BARRIER/entered\"\n  while [ ! -f \"$MUTABLE_BARRIER/release\" ]; do sleep 0.01; done\nfi\nexec \"$MUTABLE_REAL_GIT\" \"$@\"\n")
			if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MUTABLE_REAL_GIT", realGit)
			t.Setenv("MUTABLE_BLOCK_URL", first)
			t.Setenv("MUTABLE_OBSERVE_URL", second)
			blockPath, blockErr := filepath.EvalSymlinks(filepath.Dir(RegistryCachePath(first, "main")))
			observePath, observeErr := filepath.EvalSymlinks(filepath.Dir(RegistryCachePath(second, "main")))
			if warm && (blockErr != nil || observeErr != nil) {
				t.Fatal(errors.Join(blockErr, observeErr))
			}
			t.Setenv("MUTABLE_BLOCK_PATH", filepath.Join(blockPath, "main"))
			t.Setenv("MUTABLE_OBSERVE_PATH", filepath.Join(observePath, "main"))
			t.Setenv("MUTABLE_BARRIER", barrier)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() { _, err := CloneOrPull(first, "main"); firstDone <- err }()
			defer func() {
				writeFile(t, filepath.Join(barrier, "release"), "")
				if err := awaitMutable(t, firstDone); err != nil {
					t.Error(err)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), mutableProgressBound)
			defer cancel()
			if !awaitMutableFile(ctx, filepath.Join(barrier, "entered")) {
				t.Fatal("first Git operation never reached barrier")
			}
			go func() { _, err := CloneOrPull(second, "main"); secondDone <- err }()
			// The first operation stays parked at the barrier until release, so
			// the second must reach Git while it is still blocked.
			overlapCtx, overlapCancel := context.WithTimeout(context.Background(), mutableProgressBound)
			defer overlapCancel()
			if !awaitMutableFile(overlapCtx, filepath.Join(barrier, "observed")) {
				writeFile(t, filepath.Join(barrier, "release"), "")
				if err := awaitMutable(t, secondDone); err != nil {
					t.Fatal(err)
				}
				t.Fatal("independent registry blocked behind unrelated Git operation")
			}
			writeFile(t, filepath.Join(barrier, "release"), "")
			if err := awaitMutable(t, secondDone); err != nil {
				t.Fatal(err)
			}
		})
	}
}
