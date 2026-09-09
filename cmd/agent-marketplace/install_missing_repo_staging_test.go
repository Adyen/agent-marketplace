package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

func TestRepoInstallStagesMissingConfigSkeletonAndSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "lower-layer-install.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [claude]\nregistries:\n  - url: "+origin+"\n    ref: main\n")

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repo)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	canonicalRepoConfig, err := canonicalizeFuturePath(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop before dependency commit")
	originalCommit := commitDependencyTransaction
	commitDependencyTransaction = func(_ context.Context, _ string, desired []dependencytxn.DesiredFile, _ ...dependencytxn.Option) (dependencytxn.Result, error) {
		if _, err := os.Stat(repoConfig); !os.IsNotExist(err) {
			t.Fatalf("real repo config exists when dependency reconcile starts: %v", err)
		}
		for _, file := range desired {
			if file.Path != canonicalRepoConfig {
				continue
			}
			if !bytes.Contains(file.Bytes, []byte("apiVersion: agent-marketplace/v2")) ||
				!bytes.Contains(file.Bytes, []byte(origin)) ||
				!bytes.Contains(file.Bytes, []byte("review")) {
				t.Fatalf("staged repo config lacks skeleton, lower-layer registry, or selection:\n%s", file.Bytes)
			}
			return dependencytxn.Result{}, stop
		}
		t.Fatalf("staged repo config missing from desired transaction: %#v", desired)
		return dependencytxn.Result{}, stop
	}
	t.Cleanup(func() { commitDependencyTransaction = originalCommit })

	_, err = executeCommand("install", "review")
	if !errors.Is(err, stop) {
		t.Fatalf("install error = %v, want %v", err, stop)
	}
	if _, err := os.Stat(repoConfig); !os.IsNotExist(err) {
		t.Fatalf("real repo config published after failed reconcile: %v", err)
	}
}
