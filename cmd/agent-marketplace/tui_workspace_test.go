package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func TestProductionTUIRejectsOutsideDiscoveredLocalRootBeforeLifecycle(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot := t.TempDir()
	outsideRoot := t.TempDir()
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outsideRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LocalConfigPath(outsideRoot), []byte("registries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(outsideRoot)

	var prepareCalls, syncCalls atomic.Int64
	previousPrepare := prepareTUILocks
	previousSync := runTUIExactSync
	prepareTUILocks = func(context.Context, string, string, lockMode) (exactLockLeaseFunc, error) {
		prepareCalls.Add(1)
		return nil, nil
	}
	runTUIExactSync = func(context.Context, pipeline.ResolveOptions) pipeline.SyncResult {
		syncCalls.Add(1)
		return pipeline.SyncResult{}
	}
	t.Cleanup(func() {
		prepareTUILocks = previousPrepare
		runTUIExactSync = previousSync
	})

	err := runProductionTUI(context.Background(), repoRoot, homeDir)
	if !errors.Is(err, pipeline.ErrInvalidWorkspace) {
		t.Fatalf("runProductionTUI error = %v; want ErrInvalidWorkspace", err)
	}
	if prepareCalls.Load() != 0 || syncCalls.Load() != 0 {
		t.Fatalf("invalid workspace reached lifecycle side effects: prepare=%d sync=%d", prepareCalls.Load(), syncCalls.Load())
	}
}

func TestTUIDependencyLifecycleRejectsInvalidWorkspaceBeforeLocks(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	var prepareCalls, syncCalls atomic.Int64
	previousPrepare := prepareTUILocks
	previousSync := runTUIExactSync
	prepareTUILocks = func(context.Context, string, string, lockMode) (exactLockLeaseFunc, error) {
		prepareCalls.Add(1)
		return nil, nil
	}
	runTUIExactSync = func(context.Context, pipeline.ResolveOptions) pipeline.SyncResult {
		syncCalls.Add(1)
		return pipeline.SyncResult{}
	}
	t.Cleanup(func() {
		prepareTUILocks = previousPrepare
		runTUIExactSync = previousSync
	})

	_, err := runTUIDependencyLifecycle(context.Background(), pipeline.Workspace{}, false)
	if !errors.Is(err, pipeline.ErrInvalidWorkspace) {
		t.Fatalf("runTUIDependencyLifecycle error = %v; want ErrInvalidWorkspace", err)
	}
	if prepareCalls.Load() != 0 || syncCalls.Load() != 0 {
		t.Fatalf("invalid workspace reached lifecycle side effects: prepare=%d sync=%d", prepareCalls.Load(), syncCalls.Load())
	}
}

func TestTUIConfigMutationRejectsInvalidWorkspaceBeforeStaging(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	var mutationCalls atomic.Int64
	err := runTUIConfigMutation(context.Background(), pipeline.Workspace{}, func(pipeline.ConfigPathMapper) error {
		mutationCalls.Add(1)
		return nil
	})
	if !errors.Is(err, pipeline.ErrInvalidWorkspace) {
		t.Fatalf("runTUIConfigMutation error = %v; want ErrInvalidWorkspace", err)
	}
	if mutationCalls.Load() != 0 {
		t.Fatalf("invalid workspace reached config mutation %d times", mutationCalls.Load())
	}
}
