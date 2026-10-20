package packagegen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
	stateRoot, err := StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := ownership.CanonicalRoot(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	instanceKey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rootKey := ownership.RootKey(canonicalRoot)
	statePath := filepath.Join(stateRoot, "ownership", "plugin-packages", instanceKey+".json")
	tempPath := filepath.Join(filepath.Dir(statePath), ".dependencytxn-pending.tmp")
	generationKey := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pluginState := ownership.PluginState{
		Version: 1,
		Instance: ownership.PluginGenerationRef{
			InstanceKey:   instanceKey,
			Name:          "kit",
			Target:        "claude",
			Scope:         "repo:" + rootKey,
			GenerationKey: generationKey,
		},
	}
	stateBytes, err := ownership.EncodePluginState(pluginState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tempPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempPath, stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	transactionDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(transactionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journalBytes, err := json.Marshal(struct {
		Version int    `json:"version"`
		ID      string `json:"id"`
		Files   []struct {
			Path string `json:"path"`
			Temp string `json:"temp"`
		} `json:"files"`
	}{
		Version: 1,
		ID:      "pending",
		Files: []struct {
			Path string `json:"path"`
			Temp string `json:"temp"`
		}{{Path: statePath, Temp: tempPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(transactionDir, ".dependencytxn-pending.json")
	if err := os.WriteFile(journalPath, journalBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, stagedGeneration, _, stagedErr := dependencytxn.ReadSnapshot(tempPath)
	if stagedErr != nil || !stagedGeneration.Exists {
		t.Fatalf("staged plugin state snapshot = %q / %+v / %v", staged, stagedGeneration, stagedErr)
	}
	pending, err := ownership.ReadPendingPluginGenerationRefs()
	if err != nil || len(pending) != 1 || pending[0].InstanceKey != instanceKey || pending[0].GenerationKey != generationKey {
		t.Fatalf("pending plugin generation references = %+v / %v; state=%s journal=%s", pending, err, statePath, journalBytes)
	}
	generationPath := filepath.Join(stateRoot, "packages", generationKey)
	if err := os.MkdirAll(generationPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generationPath, "plugin.json"), []byte("package"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := GarbageCollect(false)
	if err != nil || len(removed) != 0 {
		t.Fatalf("pending journal generation collection = %v / %v", removed, err)
	}
	if _, err := os.Stat(generationPath); err != nil {
		t.Fatalf("pending ownership journal did not keep generation alive: %v", err)
	}

	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tempPath); err != nil {
		t.Fatal(err)
	}
	removed, err = GarbageCollect(false)
	if err != nil || len(removed) != 1 || removed[0] != generationPath {
		t.Fatalf("unreferenced generation collection = %v / %v", removed, err)
	}
}
