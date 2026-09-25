package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One Git registry is configured at two refs and both select `review`, so the
// release must pick a deterministic collision winner. The candidate must keep
// that winner, both locked refs, and the other selections, before and after
// migration; a refused migration must leave everything unchanged.
func TestPreV4DuplicateRefUpgrade(t *testing.T) {
	if os.Getenv("AM_E2E_PRE_V4_BINARY") == "" {
		t.Skip("historical executable is supplied by the dedicated upgrade gate")
	}
	historical, arch := verifiedExecutable(t, "AM_E2E_PRE_V4_BINARY", preV4ReleaseArch)
	world := newHistoricalWorld(t, upgradeImage(t))
	candidate := world.buildCandidate(arch, "e2e")
	home, workspace := world.path("home"), world.path("workspace")
	for _, dir := range []string{home, filepath.Join(workspace, ".agents")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	registry := world.gitRegistry(map[string]string{"review": "Review from main", "second": "Second from main"})
	gitRun(t, registry.source, "checkout", "-q", "-b", "release")
	registry.writeSkill("review", "Review from release")
	registry.commit("release review")
	gitRun(t, registry.source, "push", "-q", "origin", "release")
	gitRun(t, registry.source, "checkout", "-q", "main")
	gitRun(t, "", "init", "-q", "-b", "main", workspace)

	must := func(executable string, args ...string) string {
		t.Helper()
		return world.must(home, workspace, executable, args...)
	}
	installed := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(workspace, ".factory", "skills", name, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s is not installed: %v", name, err)
		}
		return string(data)
	}
	lockPath := filepath.Join(workspace, ".agents", "lock.yaml")
	repoConfig := filepath.Join(workspace, ".agents", "config.yaml")
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	must(historical, "init", "--harnesses", "factory")
	legacy := fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - url: %s\n    ref: main\n    skills:\n      include: [review, second]\n  - url: %s\n    ref: release\n    skills:\n      include: [review]\n", registry.URL, registry.URL)
	if err := os.WriteFile(repoConfig, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	must(historical, "sync")
	winner, second := installed("review"), installed("second")
	if !strings.Contains(winner, "Review from") || second != skillDocument("second", "Second from main") {
		t.Fatalf("v3.1.1 baseline review=%q second=%q", winner, second)
	}
	committed := read(lockPath)
	for _, ref := range []string{"ref: main", "ref: release"} {
		if !bytes.Contains(committed, []byte(ref)) {
			t.Fatalf("v3.1.1 lock lacks %s:\n%s", ref, committed)
		}
	}
	registry.publish("second", "Second moved on")

	assertParity := func(stage string) {
		t.Helper()
		if got := installed("review"); got != winner {
			t.Fatalf("%s: collision winner changed from %q to %q", stage, winner, got)
		}
		if got := installed("second"); got != second {
			t.Fatalf("%s: locked second changed to %q", stage, got)
		}
	}
	for _, args := range [][]string{{"sync", "--locked"}, {"sync"}, {"sync", "--check"}} {
		must(candidate, args...)
		assertParity(fmt.Sprintf("candidate %v", args))
		if got := read(lockPath); !bytes.Equal(got, committed) {
			t.Fatalf("candidate %v rewrote the duplicate-ref lock:\nbefore:\n%s\nafter:\n%s", args, committed, got)
		}
	}

	before := read(repoConfig)
	if output, err := world.run(home, workspace, candidate, "migrate", "--scope", "repo"); err != nil {
		if got := read(repoConfig); !bytes.Equal(got, before) {
			t.Fatalf("refused duplicate-ref migration changed the repository config:\n%s\noutput:\n%s", got, output)
		}
		if got := read(lockPath); !bytes.Equal(got, committed) {
			t.Fatalf("refused duplicate-ref migration changed the lock:\n%s", got)
		}
		t.Logf("duplicate-ref migration refused safely:\n%s", output)
	} else {
		migrated := string(read(repoConfig))
		if !strings.Contains(migrated, "apiVersion: agent-marketplace/v4") || !strings.Contains(migrated, "ref: main") || !strings.Contains(migrated, "ref: release") {
			t.Fatalf("duplicate-ref migration lost a ref:\n%s", migrated)
		}
	}
	must(candidate, "sync", "--locked")
	assertParity("after migration")
	must(candidate, "sync", "--check")
	assertParity("after migration check")
}
