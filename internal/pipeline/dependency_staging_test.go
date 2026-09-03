package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSilentSyncCollectPreservesDependencyWarnings(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	warning := errors.New("dependency warning")
	warnings, err := silentSyncCollect(context.Background(), dir,
		withDependencyReconciler(func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error) {
			return DependencyLockView{Warnings: []error{warning}}, nil
		}),
	)
	if err != nil {
		t.Fatalf("silentSyncCollect: %v", err)
	}
	if len(warnings) != 1 || !errors.Is(warnings[0], warning) {
		t.Fatalf("warnings = %v, want dependency warning", warnings)
	}
}

func TestMutateConfigWithDependenciesKeepsEditPrivateUntilReconcile(t *testing.T) {
	dir := canonicalTempDir(t)
	destination := filepath.Join(dir, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("apiVersion: agent-marketplace/v2\n")
	if err := os.WriteFile(destination, original, 0o644); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop after reconcile")
	err := MutateConfigWithDependencies(context.Background(), dir, dir, func(mapper ConfigPathMapper) error {
		shadow, err := mapper(destination)
		if err != nil {
			return err
		}
		return os.WriteFile(shadow, []byte("apiVersion: agent-marketplace/v2\nskills: [review]\n"), 0o644)
	}, func(_ context.Context, _, _ string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
		real, err := os.ReadFile(destination)
		if err != nil || string(real) != string(original) {
			t.Fatalf("real config at reconciler start = %q, %v", real, err)
		}
		if len(edits) != 1 || string(edits[0].Bytes) != "apiVersion: agent-marketplace/v2\nskills: [review]\n" {
			t.Fatalf("staged edits = %#v", edits)
		}
		return DependencyLockView{}, stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("error = %v, want %v", err, stop)
	}
	after, err := os.ReadFile(destination)
	if err != nil || string(after) != string(original) {
		t.Fatalf("real config after failure = %q, %v", after, err)
	}
}

func TestDependencyReconcilerReceivesCoalescedStagedConfig(t *testing.T) {
	dir := canonicalTempDir(t)
	destination := filepath.Join(dir, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("apiVersion: agent-marketplace/v2\n")
	if err := os.WriteFile(destination, original, 0o644); err != nil {
		t.Fatal(err)
	}
	staging, err := newConfigStagingManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := staging.Close(); err != nil {
			t.Error(err)
		}
	})
	shadow, err := staging.Path(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shadow, []byte("apiVersion: agent-marketplace/v2\nskills: [one]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shadow, []byte("apiVersion: agent-marketplace/v2\nskills: [final]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop after reconcile")
	_, err = silentSyncCollect(context.Background(), dir,
		withConfigStaging(staging),
		withDependencyReconciler(func(_ context.Context, _, _ string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
			real, readErr := os.ReadFile(destination)
			if readErr != nil || string(real) != string(original) {
				t.Fatalf("real config at reconciler start = %q, %v", real, readErr)
			}
			if len(edits) != 1 || string(edits[0].Bytes) != "apiVersion: agent-marketplace/v2\nskills: [final]\n" {
				t.Fatalf("staged edits = %#v", edits)
			}
			return DependencyLockView{}, stop
		}),
	)
	if !errors.Is(err, stop) {
		t.Fatalf("error = %v, want %v", err, stop)
	}
}
