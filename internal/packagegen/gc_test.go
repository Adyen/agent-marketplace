package packagegen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestPublishRemovesAbandonedPackageStages(t *testing.T) {
	input, packagesDir := packageGenerationFixture(t)
	stale := filepath.Join(packagesDir, ".packagegen-abandoned")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(input); err != nil {
		t.Fatalf("publish package generation: %v", err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("publish left abandoned staging directory: %v", err)
	}
}

func TestGarbageCollectRemovesAbandonedPackageStages(t *testing.T) {
	_, packagesDir := packageGenerationFixture(t)
	stale := filepath.Join(packagesDir, ".packagegen-abandoned")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	removed, err := GarbageCollect(false)
	if err != nil {
		t.Fatalf("collect abandoned package stages: %v", err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("garbage collection left abandoned staging directory: %v", err)
	}
	if len(removed) != 1 || removed[0] != stale {
		t.Fatalf("removed stages = %v, want [%s]", removed, stale)
	}
}

func TestPackageGenerationMaintenanceWaitsForPublisherLock(t *testing.T) {
	input, packagesDir := packageGenerationFixture(t)
	stage := filepath.Join(packagesDir, ".packagegen-active")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- dependencytxn.WithPathLock(context.Background(), packagesDir, func() error {
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-lockDone:
		t.Fatalf("acquire package publication lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("package publication lock was not acquired")
	}

	publishDone := make(chan error, 1)
	go func() {
		_, err := Publish(input)
		publishDone <- err
	}()
	assertPackageMaintenanceWaits(t, stage, publishDone)

	collectDone := make(chan error, 1)
	go func() {
		_, err := GarbageCollect(false)
		collectDone <- err
	}()
	assertPackageMaintenanceWaits(t, stage, collectDone)

	close(release)
	if err := <-lockDone; err != nil {
		t.Fatalf("release package publication lock: %v", err)
	}
	if err := <-publishDone; err != nil {
		t.Fatalf("publisher after releasing lock: %v", err)
	}
	if err := <-collectDone; err != nil {
		t.Fatalf("collector after releasing lock: %v", err)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatalf("stale stage remained after publisher and collector completed: %v", err)
	}
}

func assertPackageMaintenanceWaits(t *testing.T, stage string, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("package maintenance did not wait for publisher lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("active publisher stage was removed while lock was held: %v", err)
	}
}

func packageGenerationFixture(t *testing.T) (Input, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", canonicalHome)
	source := filepath.Join(root, "package")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"name":"stage-kit"}`)
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	input := Input{
		Plugin: &ir.Plugin{
			Manifest: ir.Manifest{Name: "stage-kit"},
			Files:    []ir.PluginFile{{Path: "plugin.json", Digest: hex.EncodeToString(digest[:])}},
		},
		PackageRoot:        source,
		Target:             hcc.TargetClaude,
		Delivery:           "decompose",
		Scope:              "repo:abc",
		CatalogFingerprint: "catalog-a",
	}
	generation, err := Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	packagesDir := filepath.Dir(generation.Path)
	if err := os.MkdirAll(packagesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return input, packagesDir
}

func TestGarbageCollectKeepsGenerationReferencedByPendingOwnershipJournal(t *testing.T) {
	input, packagesDir := packageGenerationFixture(t)
	generation, err := Publish(input)
	if err != nil {
		t.Fatal(err)
	}
	instanceKey := strings.Repeat("a", 64)
	statePath, err := ownership.PluginStatePath(instanceKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	pluginState := ownership.PluginState{
		Version: 1,
		Instance: ownership.PluginGenerationRef{
			InstanceKey:   instanceKey,
			Name:          "kit",
			Target:        "claude",
			Scope:         "machine",
			GenerationKey: generation.Key,
		},
	}
	stateBytes, err := ownership.EncodePluginState(pluginState)
	if err != nil {
		t.Fatal(err)
	}
	transactionDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("stop after journal")
	_, err = dependencytxn.Commit(t.Context(), transactionDir, []dependencytxn.DesiredFile{
		{Path: statePath, Bytes: stateBytes, Mode: 0o600},
	}, dependencytxn.WithFaultHook(func(p dependencytxn.FaultPoint) error {
		if p.Stage == dependencytxn.FaultAfterJournal {
			return interrupted
		}
		return nil
	}))
	if !errors.Is(err, interrupted) {
		t.Fatalf("interrupt plugin transaction: %v", err)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination already published: %v", err)
	}
	if removed, err := GarbageCollect(false); err != nil || len(removed) != 0 {
		t.Fatalf("pending journal generation collection = %v / %v", removed, err)
	}
	orphan := filepath.Join(packagesDir, strings.Repeat("b", 64))
	if err := os.Mkdir(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	// An unreadable reference set must abort before even the orphan is swept.
	badRef := filepath.Join(filepath.Dir(statePath), strings.Repeat("f", 64)+".json")
	if err := os.WriteFile(badRef, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := GarbageCollect(false); err == nil {
		t.Fatal("collected despite malformed committed state")
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("reference read failure did not preserve orphan: %v", err)
	}
	if err := os.Remove(badRef); err != nil {
		t.Fatal(err)
	}
	if removed := collectAcrossRecovery(t, transactionDir); !slices.Equal(removed, []string{orphan}) {
		t.Fatalf("collection across recovery = %v; want only %s", removed, orphan)
	}
	state, _, _, _, err := ownership.ReadPluginState(instanceKey)
	if err != nil || state.Instance.GenerationKey != generation.Key {
		t.Fatalf("recovered generation = %s / %v", state.Instance.GenerationKey, err)
	}
	if _, err := os.Stat(filepath.Join(generation.Path, "plugin.json")); err != nil {
		t.Fatalf("recovered generation missing: %v", err)
	}
}

func collectAcrossRecovery(t *testing.T, transactionDir string) []string {
	t.Helper()
	removed, err := garbageCollect(false, func() ([]ownership.PluginGenerationRef, error) {
		if err := dependencytxn.Recover(t.Context(), transactionDir); err != nil {
			return nil, err
		}
		return ownership.ReadPendingPluginGenerationRefs()
	})
	if err != nil {
		t.Fatal(err)
	}
	return removed
}
