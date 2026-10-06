package main

// Consumer-level evidence for the descriptor-bound config/lock boundary:
// lock reconciliation through an approved contained repository link, the
// cross-layer object-alias refusal, and deinit purge through an approved
// user dotfile link. Package-level boundary behavior lives in
// internal/dependencytxn/authority_test.go; these tests prove the CLI
// orchestrators route the six layer destinations through it.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
)

func TestLockReconcileHonorsContainedRepositoryLockLink(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	origin, originDir := createBareRepoWithWorkingClone(t, "origin.git")
	writeSkill(t, originDir, "a", "alpha", nil)
	commitAndPushAll(t, originDir, "seed")

	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "harnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n")
	target := filepath.Join(repo, ".agents", "shared", "lock.yaml")
	writeFile(t, target, "apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries: []\n")
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if err := os.Symlink(filepath.Join("shared", "lock.yaml"), lockPath); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)

	out, err := executeCommand("lock")
	if err != nil {
		t.Fatalf("lock through contained link: %v\n%s", err, out)
	}

	linkText, err := os.Readlink(lockPath)
	if err != nil || linkText != filepath.Join("shared", "lock.yaml") {
		t.Fatalf("contained lock link replaced: text=%q err=%v", linkText, err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries: []\n" {
		t.Fatalf("lock target never updated; lock output:\n%s", out)
	}
	if !strings.Contains(string(got), "commit: sha1:") {
		t.Fatalf("reconciled lock lost the registry pin:\n%s", got)
	}
	entries, err := os.ReadDir(filepath.Join(repo, ".agents"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("staged residue %q after reconcile", entry.Name())
		}
	}
}

// spec: config-mutation/cross-layer-object-aliases-are-refused
func TestPrepareRuntimeLocksRejectsCrossLayerObjectAlias(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	origin, originDir := createBareRepoWithWorkingClone(t, "origin.git")
	writeSkill(t, originDir, "a", "alpha", nil)
	commitAndPushAll(t, originDir, "seed")

	writeSkill(t, originDir, "b", "beta", nil)
	commitAndPushAll(t, originDir, "add independent install target")
	repo := t.TempDir()
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, repoConfig, "harnesses: [factory]\nskills: [a]\nregistries:\n  - url: "+origin+"\n    ref: main\n")
	repoBefore, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	// The user layer is a dotfile link onto the repository config object, so
	// one transaction would publish two layer paths to a single object.
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repoConfig, userConfig); err != nil {
		t.Fatal(err)
	}

	_, _, _, err = lifecycle.PrepareRuntimeLocks(context.Background(), repo, repo, lifecycle.LockMode{})
	if !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("cross-layer alias: %v, want ErrUnsafePath", err)
	}
	withWorkingDir(t, repo)
	if out, err := executeCommand("install", "b", "--scope", "user"); !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("staged cross-layer alias: %v, want ErrUnsafePath\n%s", err, out)
	}

	got, err := os.ReadFile(repoConfig)
	if err != nil || string(got) != string(repoBefore) {
		t.Fatalf("repo config mutated: %q, %v", got, err)
	}
	if linkText, lerr := os.Readlink(userConfig); lerr != nil || linkText != repoConfig {
		t.Fatalf("user dotfile link touched: text=%q err=%v", linkText, lerr)
	}
	for _, lockPath := range []string{
		filepath.Join(repo, ".agents", "lock.yaml"),
		filepath.Join(home, ".agents", "lock.yaml"),
	} {
		if _, lerr := os.Lstat(lockPath); !os.IsNotExist(lerr) {
			t.Fatalf("lock %q created despite the alias refusal", lockPath)
		}
	}
}

// spec: config-mutation/purge-removes-an-approved-layer-link-without-deleting-its-target
func TestDeinitPurgeRemovesApprovedUserDotfileLink(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	target := filepath.Join(home, "dotfiles", "agents.yaml")
	writeFile(t, target, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n")
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, userConfig); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, home)

	out, err := executeCommand("deinit", "--scope", "user", "--purge-config")
	if err != nil {
		t.Fatalf("purge through dotfile link: %v\n%s", err, out)
	}

	if _, lerr := os.Lstat(userConfig); !os.IsNotExist(lerr) {
		t.Fatalf("dotfile link survived purge: %v", lerr)
	}
	after, err := os.ReadFile(target)
	if err != nil || string(after) != string(before) {
		t.Fatalf("dotfile target mutated: %q, %v", after, err)
	}
	afterInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if afterInfo.Mode() != beforeInfo.Mode() || !os.SameFile(beforeInfo, afterInfo) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("dotfile target metadata changed: mode %v→%v mtime %v→%v", beforeInfo.Mode(), afterInfo.Mode(), beforeInfo.ModTime(), afterInfo.ModTime())
	}
}
