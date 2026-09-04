package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
)

func TestConfiguredInstallKeepsValidLayerExactWhenAnotherLayerLockIsMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	lockedOrigin, lockedWorking := createBareRepoWithWorkingClone(t, "locked-layer.git")
	writeSkill(t, lockedWorking, "reviewed", "Reviewed before locking", nil)
	commitAndPushAll(t, lockedWorking, "seed locked layer")

	bootstrapOrigin, bootstrapWorking := createBareRepoWithWorkingClone(t, "bootstrap-layer.git")
	writeSkill(t, bootstrapWorking, "bootstrap", "Available from missing-lock layer", nil)
	commitAndPushAll(t, bootstrapWorking, "seed bootstrap layer")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
registries:
  - url: `+lockedOrigin+`
    ref: main
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: `+bootstrapOrigin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("lock", "--scope", "user"); err != nil {
		t.Fatalf("lock user layer: %v\n%s", err, output)
	}
	writeSkill(t, lockedWorking, "unreviewed", "Added after the user lock", nil)
	commitAndPushAll(t, lockedWorking, "advance locked layer")

	if output, err := executeCommand("install", "unreviewed"); err == nil || !strings.Contains(err.Error(), "not found in configured registries") {
		t.Fatalf("advanced resource from valid locked layer became installable: err=%v\n%s", err, output)
	}
	if output, err := executeCommand("install", "bootstrap"); err != nil {
		t.Fatalf("missing-lock layer did not retain mutable bootstrap: %v\n%s", err, output)
	}
	assertFactorySkillExists(t, repoRoot, "bootstrap")
}

func TestCheckCommandsDoNotPopulateColdExactCacheOrState(t *testing.T) {
	homeDir := t.TempDir()
	stateRoot := filepath.Join(homeDir, "state")
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "xdg-cache"))
	t.Setenv("XDG_STATE_HOME", stateRoot)
	cacheRoot := cache.CacheDir()

	origin, working := createBareRepoWithWorkingClone(t, "cold-check.git")
	writeSkill(t, working, "keeper", "Locked but deliberately absent from cache", nil)
	commitAndPushAll(t, working, "seed cold check")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
    skills: [keeper]
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("seed lock: %v\n%s", err, output)
	}
	if err := os.RemoveAll(cacheRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(stateRoot); err != nil {
		t.Fatal(err)
	}

	assertRootsAbsent := func(command string) {
		t.Helper()
		for _, root := range []string{cacheRoot, stateRoot} {
			if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
				t.Fatalf("%s created read-only root %s (%v)", command, root, statErr)
			}
		}
	}
	for _, args := range [][]string{{"sync", "--check"}, {"lock", "--check", "--scope", "repo"}} {
		output, err := executeCommand(args...)
		if err == nil || !strings.Contains(err.Error(), "lock.snapshot-unavailable") {
			t.Fatalf("%s on cold exact cache: err=%v\n%s", strings.Join(args, " "), err, output)
		}
		assertRootsAbsent(strings.Join(args, " "))
	}

	// Stale checks must diagnose from config+lock bytes, not resolve the mutable
	// ref (which deliberately does not exist) or create cache/state while trying.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(strings.Replace(string(raw), "ref: main", "ref: does-not-exist", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		code string
	}{{[]string{"sync", "--check"}, "lock.stale"}, {[]string{"lock", "--check", "--scope", "repo"}, "lock.change-required"}} {
		output, err := executeCommand(test.args...)
		if err == nil || !strings.Contains(err.Error(), test.code) {
			t.Fatalf("%s stale read-only check: err=%v\n%s", strings.Join(test.args, " "), err, output)
		}
		assertRootsAbsent(strings.Join(test.args, " "))
	}
}
