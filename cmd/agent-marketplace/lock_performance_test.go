package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
)

func instrumentSyncGit(t *testing.T, body string) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := "#!/bin/sh\n" + body + "\nexec \"$AM_TEST_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AM_TEST_REAL_GIT", git)
	t.Setenv("AM_TEST_GIT_LOG", filepath.Join(bin, "invocations"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func TestSyncIndependentRegistriesOverlap(t *testing.T) {
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(key, "")
	}
	for _, missingLock := range []bool{false, true} {
		name := "warm"
		if missingLock {
			name = "missing-lock"
		}
		t.Run(name, func(t *testing.T) {
			repo := setupLocalGitSyncFixture(t, 2)
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("seed sync: %v\n%s", err, output)
			}
			lockPath := filepath.Join(repo, ".agents", "lock.yaml")
			before, _ := readLifecycleLock(t, lockPath)
			command := "ls-tree"
			if missingLock {
				command = "fetch"
				if err := os.Remove(lockPath); err != nil {
					t.Fatal(err)
				}
			}
			barrier := t.TempDir()
			t.Setenv("AM_TEST_BARRIER", barrier)
			t.Setenv("AM_TEST_BARRIER_COMMAND", command)
			instrumentSyncGit(t, `
case " $* " in
  *" $AM_TEST_BARRIER_COMMAND "*)
    touch "$AM_TEST_BARRIER/${PWD##*/}"
    ready() { set -- "$AM_TEST_BARRIER"/*; [ "$#" -ge 2 ]; }
    attempts=0
    until ready; do
      attempts=$((attempts + 1))
      if [ "$attempts" -ge 200 ]; then
        echo "independent registry never reached barrier" >&2
        exit 1
      fi
      sleep 0.01
    done
    ;;
esac
`)
			output, err := executeCommand("sync")
			if err != nil {
				t.Fatalf("concurrent sync: %v\n%s", err, output)
			}
			after, _ := readLifecycleLock(t, lockPath)
			if string(before) != string(after) {
				t.Fatal("concurrency changed deterministic lock bytes")
			}
			if !strings.Contains(output, "Installed 0") {
				t.Fatalf("unchanged snapshots rematerialized: %s", output)
			}
			arrivals, err := os.ReadDir(barrier)
			if err != nil || len(arrivals) != 2 {
				t.Fatalf("barrier did not exercise both origins: %v, %v", arrivals, err)
			}
		})
	}
}

func TestSyncConcurrentAcquisitionFailurePreservesDependenciesAndOutput(t *testing.T) {
	repo := setupLocalGitSyncFixture(t, 2)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	index := strings.LastIndex(body, "ref: main")
	if index < 0 {
		t.Fatal("fixture lacks second Git requirement")
	}
	body = body[:index] + strings.Replace(body[index:], "ref: main", "ref: absent", 1)
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	before := dependencyTreeSnapshot(t, repo)
	if output, err := executeCommand("sync"); err == nil {
		t.Fatalf("missing ref accepted: %s", output)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed acquisition published a lock: %v", err)
	}
	if after := dependencyTreeSnapshot(t, repo); !reflect.DeepEqual(before, after) {
		t.Fatal("failed acquisition changed dependency files or installed output")
	}
}

func TestOfflineSyncRejectsUnsafeCacheCoordinationWithoutRepair(t *testing.T) {
	setupLocalGitSyncFixture(t, 1)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, output)
	}
	exactRoot := filepath.Join(cache.CacheDir(), "exact-git")
	if err := os.Chmod(filepath.Join(exactRoot, "locks"), 0o750); err != nil {
		t.Fatal(err)
	}
	before := exactCacheTreeSnapshot(t, exactRoot)
	if output, err := executeCommand("sync", "--offline"); !errors.Is(err, cache.ErrExactGitCoordinationUnavailable) {
		t.Fatalf("offline sync accepted unsafe coordination: %v\n%s", err, output)
	}
	if after := exactCacheTreeSnapshot(t, exactRoot); after != before {
		t.Fatal("offline sync repaired cache coordination metadata")
	}
}

func TestSyncWarmLockVerifiesSnapshotOnce(t *testing.T) {
	for _, name := range []string{"canonical", "symlink-root"} {
		t.Run(name, func(t *testing.T) {
			testSyncWarmLockVerifiesSnapshotOnce(t, name == "symlink-root")
		})
	}
}

func testSyncWarmLockVerifiesSnapshotOnce(t *testing.T, alias bool) {
	_, repo, _ := setupDependencyInspection(t)
	origin, working := createBareRepoWithWorkingClone(t, "warm-performance.git")
	writeSkill(t, working, "review", "Review", nil)
	rootPath := "."
	if alias {
		rootPath = "alias"
		if err := os.Symlink("skills", filepath.Join(working, rootPath)); err != nil {
			t.Fatal(err)
		}
	}
	commitAndPushAll(t, working, "review")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nharnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n    path: "+rootPath+"\n    skills:\n      include: [review]\n")
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	before, _ := readLifecycleLock(t, lockPath)
	bin := instrumentSyncGit(t, "printf '%s\\n' \"$*\" >> \"$AM_TEST_GIT_LOG\"\n")
	log := filepath.Join(bin, "invocations")
	ingestLog := filepath.Join(bin, "sync.log")
	t.Setenv("AGENT_MARKETPLACE_LOG_ENABLED", "true")
	t.Setenv("AGENT_MARKETPLACE_LOG_FILE", ingestLog)
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("warm sync: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Installed 0") {
		t.Fatalf("warm sync changed installed resources: %s", output)
	}
	after, _ := readLifecycleLock(t, lockPath)
	if string(before) != string(after) {
		t.Fatal("warm sync rewrote lock")
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(calls), "ls-tree"); count != 1 {
		t.Errorf("warm sync verified the tracked tree %d times, want once\n%s", count, calls)
	}
	if strings.Contains(string(calls), "fetch") || strings.Contains(string(calls), "clone") {
		t.Errorf("warm sync accessed its origin:\n%s", calls)
	}
	ingests, err := os.ReadFile(ingestLog)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(ingests), `"msg":"harness ingest start"`); count != 1 {
		t.Errorf("warm sync ingested the source %d times, want once", count)
	}
	listing, err := executeCommand("list")
	if err != nil || strings.Contains(listing, "out of sync") || !strings.Contains(listing, "review") {
		t.Fatalf("list disagrees with unchanged sync: %v\n%s", err, listing)
	}
}
