package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
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
	previousPrepare := session.PrepareTUILocks
	previousSync := session.RunTUIExactSync
	session.PrepareTUILocks = func(context.Context, string, string, lifecycle.LockMode) (lifecycle.ExactLockLeaseFunc, error) {
		prepareCalls.Add(1)
		return nil, nil
	}
	session.RunTUIExactSync = func(context.Context, pipeline.ResolveOptions) pipeline.SyncResult {
		syncCalls.Add(1)
		return pipeline.SyncResult{}
	}
	t.Cleanup(func() {
		session.PrepareTUILocks = previousPrepare
		session.RunTUIExactSync = previousSync
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
	previousPrepare := session.PrepareTUILocks
	previousSync := session.RunTUIExactSync
	session.PrepareTUILocks = func(context.Context, string, string, lifecycle.LockMode) (lifecycle.ExactLockLeaseFunc, error) {
		prepareCalls.Add(1)
		return nil, nil
	}
	session.RunTUIExactSync = func(context.Context, pipeline.ResolveOptions) pipeline.SyncResult {
		syncCalls.Add(1)
		return pipeline.SyncResult{}
	}
	t.Cleanup(func() {
		session.PrepareTUILocks = previousPrepare
		session.RunTUIExactSync = previousSync
	})

	_, err := session.RuntimeDependencyLifecycle(context.Background(), pipeline.Workspace{}, false)
	if !errors.Is(err, pipeline.ErrInvalidWorkspace) {
		t.Fatalf("session.RuntimeDependencyLifecycle error = %v; want ErrInvalidWorkspace", err)
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
	err := session.RuntimeConfigMutation(context.Background(), pipeline.Workspace{}, func(pipeline.ConfigPathMapper) error {
		mutationCalls.Add(1)
		return nil
	})
	if !errors.Is(err, pipeline.ErrInvalidWorkspace) {
		t.Fatalf("session.RuntimeConfigMutation error = %v; want ErrInvalidWorkspace", err)
	}
	if mutationCalls.Load() != 0 {
		t.Fatalf("invalid workspace reached config mutation %d times", mutationCalls.Load())
	}
}
