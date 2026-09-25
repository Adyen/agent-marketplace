package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestStagedLockDestinationValidation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v2\nregistries: []\n")
	withWorkingDir(t, repo)

	_, _, _, err := prepareRuntimeLocks(context.Background(), repo, repo, lockMode{
		stagedConfigEdits: []pipeline.StagedConfigEdit{{RequestedPath: configPath, Path: configPath, Bytes: []byte("apiVersion: agent-marketplace/v2\nregistries: []\n")}},
		stagedLockEdits:   []pipeline.StagedLockEdit{{Path: configPath, Bytes: []byte("apiVersion: 2\n")}},
	})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("colliding staged lock error = %v", err)
	}

	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Fatalf("refused staged locks wrote %s: %v", lockPath, statErr)
	}
}

func TestUpdateMarketplaceV4RejectsStaleStagedLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, upgrade, _ := newUpdatableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	upgrade()

	originalCommit := commitDependencyTransaction
	commitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		for _, file := range desired {
			if !strings.HasSuffix(file.Path, "lock.yaml") {
				continue
			}
			data, err := os.ReadFile(file.Path)
			if err != nil {
				return dependencytxn.Result{}, err
			}
			if err := os.WriteFile(file.Path, append(data, []byte("# concurrent writer\n")...), 0o644); err != nil {
				return dependencytxn.Result{}, err
			}
		}
		return dependencytxn.Commit(ctx, stateDir, desired, opts...)
	}
	t.Cleanup(func() { commitDependencyTransaction = originalCommit })

	output, err = executeCommand("update")
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("update error = %v\n%s; want generation conflict", err, output)
	}
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lockData), "# concurrent writer") {
		t.Fatalf("concurrent lock writer was overwritten:\n%s", lockData)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(configBytes) {
		t.Fatalf("config changed despite conflict:\n%s", after)
	}
}
