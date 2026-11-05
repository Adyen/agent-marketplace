package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUpdateBumpTagOverflowPreservesPublishedState(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := canonicalTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	staging := canonicalTempDir(t)
	t.Setenv("TMPDIR", staging)
	origin, working := createBareRepoWithWorkingClone(t, "tag-overflow.git")
	writeSkill(t, working, "target", "Original content", nil)
	commitAndPushAll(t, working, "original")
	createAndPushTag(t, working, "v1")
	repo := canonicalTempDir(t)
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  registry:\n    git: file://"+origin+"\n    ref: v1\nresources:\n  registry:\n    include: [skill/target]\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial supported sync: %v\n%s", err, output)
	}
	native := filepath.Join(repo, ".claude", "skills", "target", "SKILL.md")
	original, info := statSnapshot(t, native)
	if !strings.Contains(string(original), "Original content") {
		t.Fatal("control: native content was not materialized")
	}
	before := cliFilesystemSnapshot(t, repo)
	writeSkill(t, working, "target", "New content must not be installed", nil)
	commitAndPushAll(t, working, "new content")
	createAndPushTag(t, working, "v2")
	createAndPushTag(t, working, "v"+strings.Repeat("9", 40))

	if output, err := executeCommand("update", "--bump"); !errors.Is(err, strconv.ErrRange) {
		t.Fatalf("update --bump = %v\n%s; want numeric overflow", err, output)
	}
	assertCliFilesystemSnapshot(t, "overflow refusal", repo, before)
	assertStatUnchanged(t, "overflow refusal", native, original, info)
	assertNoStagingResidue(t, staging)
	// Logs and Git cache acquisition are outside the no-publication claim.
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); err != nil {
		t.Fatalf("control: the pre-existing lock disappeared: %v", err)
	}
}
