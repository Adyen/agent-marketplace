package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func snapshotOfflineHome(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	return home
}

func seedOrdinaryRegistryClone(t testing.TB, url, ref, origin string) string {
	t.Helper()
	cachePath := cache.RegistryCachePath(url, ref)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	snapshotGit(t, "", "clone", origin, cachePath)
	return cachePath
}

func TestSnapshotCachedColdBootstrapOffline(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	origin := filepath.Join(base, "reg.git")
	work := filepath.Join(base, "work")
	snapshotGit(t, "", "init", "--bare", "--initial-branch=main", origin)
	snapshotGit(t, "", "init", "--initial-branch=main", work)
	snapshotGit(t, work, "remote", "add", "origin", origin)
	writeSnapshotSkill(t, work, "a", "body one")
	commitA := snapshotGitCommitPush(t, work, "a")

	url := "https://127.0.0.1:9/unreachable.example/reg.git"
	seedOrdinaryRegistryClone(t, url, "main", origin)

	local := filepath.Join(base, "local")
	writeSnapshotSkill(t, local, "loc", "local body")

	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: url, Ref: "main"},
		{URL: local},
	}}}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("cold offline BuildSnapshot errors: %v", errs)
	}
	if !snap.Stale {
		t.Fatal("cold offline snapshot not marked stale")
	}
	var resA, resLocal *Resource
	for i := range snap.Resources {
		switch snap.Resources[i].Name {
		case "a":
			resA = &snap.Resources[i]
		case "loc":
			resLocal = &snap.Resources[i]
		}
	}
	if resA == nil || resLocal == nil {
		t.Fatalf("snapshot resources = %+v", snap.Resources)
	}
	if resA.Revision != commitA {
		t.Fatalf("snapshot revision = %q, want exact commit %q", resA.Revision, commitA)
	}
	sc, err := NewFileSkillLoader(true).Load(context.Background(), resA)
	if err != nil {
		t.Fatalf("skills.load: %v", err)
	}
	if sc.Revision != commitA || !strings.Contains(sc.Content, "body one") {
		t.Fatalf("loaded skill = revision %q content %q, want %q/body one", sc.Revision, sc.Content, commitA)
	}
}

func TestSearchCachedGitSnapshotWarnsStale(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	origin := filepath.Join(base, "reg.git")
	work := filepath.Join(base, "work")
	snapshotGit(t, "", "init", "--bare", "--initial-branch=main", origin)
	snapshotGit(t, "", "init", "--initial-branch=main", work)
	snapshotGit(t, work, "remote", "add", "origin", origin)
	writeSnapshotSkill(t, work, "a", "body one")
	commitA := snapshotGitCommitPush(t, work, "a")

	url := "file://" + filepath.ToSlash(origin)
	seedOrdinaryRegistryClone(t, url, "main", origin)

	local := filepath.Join(base, "local")
	writeSnapshotSkill(t, local, "loc", "local body")

	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: url, Ref: "main"},
		{URL: local},
	}}}

	cached := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	snap, errs := cached.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("cached BuildSnapshot errors: %v", errs)
	}
	var resA *Resource
	for i := range snap.Resources {
		if snap.Resources[i].Name == "a" {
			resA = &snap.Resources[i]
		}
	}
	if resA == nil || resA.Revision != commitA {
		t.Fatalf("cached snapshot = %+v, want resource a at commit %q", snap.Resources, commitA)
	}

	store := &SnapshotStore{}
	store.Store(snap)
	eng := &Engine{Snapshots: store}
	res, warnings, err := eng.Search(context.Background(), json.RawMessage(`{"code":"() => catalog.filter(r => r.name === 'a').length"}`))
	if err != nil {
		t.Fatalf("search on cached snapshot: %v", err)
	}
	var value int
	if err := json.Unmarshal(res.Value, &value); err != nil || value != 1 {
		t.Fatalf("search result = %s err %v, want 1", res.Value, err)
	}
	var foundStale bool
	for _, w := range warnings {
		if w == ErrCodeCatalogStale {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatalf("cached git snapshot warnings = %v, want %q", warnings, ErrCodeCatalogStale)
	}

	refresh := &SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh}
	fresh, errs := refresh.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("refresh BuildSnapshot errors: %v", errs)
	}
	if fresh.Stale {
		t.Fatal("refreshed snapshot reported stale")
	}
	if len(fresh.Resources) != len(snap.Resources) {
		t.Fatalf("refreshed snapshot resources = %+v", fresh.Resources)
	}
}

func TestSnapshotCachedRepublishesExactAfterGC(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	origin := filepath.Join(base, "reg.git")
	work := filepath.Join(base, "work")
	snapshotGit(t, "", "init", "--bare", "--initial-branch=main", origin)
	snapshotGit(t, "", "init", "--initial-branch=main", work)
	snapshotGit(t, work, "remote", "add", "origin", origin)
	writeSnapshotSkill(t, work, "a", "body one")
	commitA := snapshotGitCommitPush(t, work, "a")

	url := "file://" + filepath.ToSlash(origin)
	seedOrdinaryRegistryClone(t, url, "main", origin)

	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: url, Ref: "main"},
	}}}
	refresh := &SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh}
	snap, errs := refresh.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("refresh BuildSnapshot errors: %v", errs)
	}
	if len(snap.Resources) != 1 || snap.Resources[0].Revision != commitA {
		t.Fatalf("refresh snapshot = %+v", snap.Resources)
	}
	snapshotDir := filepath.Dir(snap.Resources[0].origin.ContentDir)
	if _, err := cache.GarbageCollectWithLocks(false, nil, ec.Merged); err != nil {
		t.Fatalf("GarbageCollectWithLocks: %v", err)
	}
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Fatalf("snapshot %q still present after GC (err=%v)", snapshotDir, err)
	}
	if err := os.Rename(origin, origin+".moved"); err != nil {
		t.Fatal(err)
	}

	cached := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	snap, errs = cached.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("cached BuildSnapshot after GC errors: %v", errs)
	}
	if !snap.Stale {
		t.Fatal("cached snapshot after GC not marked stale")
	}
	if len(snap.Resources) != 1 || snap.Resources[0].Revision != commitA {
		t.Fatalf("republished snapshot = %+v", snap.Resources)
	}
	sc, err := NewFileSkillLoader(true).Load(context.Background(), &snap.Resources[0])
	if err != nil {
		t.Fatalf("skills.load after GC: %v", err)
	}
	if !strings.Contains(sc.Content, "body one") {
		t.Fatalf("republished content = %q, want body one", sc.Content)
	}
}

func TestSnapshotCachedOmitsGitWithoutTrustedClone(t *testing.T) {
	snapshotOfflineHome(t)

	base := t.TempDir()
	missing := "https://127.0.0.1:9/unreachable.example/missing.git"

	junk := "https://127.0.0.1:9/unreachable.example/junk.git"
	junkPath := cache.RegistryCachePath(junk, "main")
	if err := os.MkdirAll(junkPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junkPath, "README"), []byte("not a repository"), 0o600); err != nil {
		t.Fatal(err)
	}

	local := filepath.Join(base, "local")
	writeSnapshotSkill(t, local, "loc", "local body")

	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeCached}
	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: missing, Ref: "main"},
		{URL: junk, Ref: "main"},
		{URL: local},
	}}}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) == 0 || !snap.Stale {
		t.Fatalf("BuildSnapshot = errs %v stale %v, want error and stale", errs, snap.Stale)
	}
	for _, res := range snap.Resources {
		if res.Name == "a" {
			t.Fatalf("snapshot advertised unsafe git resource: %+v", res)
		}
	}
	var foundLocal bool
	for i := range snap.Resources {
		if snap.Resources[i].Name == "loc" {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatalf("local sibling missing from snapshot: %+v", snap.Resources)
	}
}

func TestSnapshotLeaseFailureSkipsRemoteRegistries(t *testing.T) {
	snapshotOfflineHome(t)

	acquired := make(chan struct{})
	block := make(chan struct{})
	gcDone := make(chan error, 1)
	go func() {
		_, err := cache.GarbageCollectWithReachabilityAndContext(context.Background(), true, func(ctx context.Context) (cache.GCReachability, error) {
			close(acquired)
			<-block
			return cache.GCReachability{Configs: []*config.Config{{}}}, nil
		})
		gcDone <- err
	}()
	select {
	case <-acquired:
	case <-time.After(10 * time.Second):
		t.Fatal("garbage collection did not acquire the exclusive namespace lease")
	}

	base := t.TempDir()
	local := filepath.Join(base, "local")
	writeSnapshotSkill(t, local, "loc", "local body")

	builder := &SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh}
	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: "https://127.0.0.1:9/unreachable.example/reg.git", Ref: "main"},
		{URL: local},
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snap, errs := builder.BuildSnapshot(ctx, ec)
	close(block)
	if err := <-gcDone; err != nil {
		t.Fatalf("blocked garbage collection: %v", err)
	}
	if !snap.Stale {
		t.Fatal("snapshot not stale after lease failure")
	}
	var sawLease bool
	for _, err := range errs {
		if !strings.Contains(err.Error(), "lease") {
			t.Fatalf("remote registry attempted after lease failure: %v", err)
		}
		sawLease = true
	}
	if !sawLease {
		t.Fatal("no lease error reported")
	}
	var foundLocal bool
	for i := range snap.Resources {
		if snap.Resources[i].Name == "loc" {
			foundLocal = true
		}
		if snap.Resources[i].Name == "a" {
			t.Fatalf("remote resource materialized after lease failure: %+v", snap.Resources[i])
		}
	}
	if !foundLocal {
		t.Fatalf("local sibling missing after lease failure: %+v", snap.Resources)
	}
}

const gitPinHelperEnv = "CODEMODE_TEST_PIN_HELPER"

func TestGitPinsMergeDisjointWriters(t *testing.T) {
	home := snapshotOfflineHome(t)
	state := filepath.Join(home, "state")

	keyA := pinKey("canonical-a", "main")
	oidA := "sha1:" + strings.Repeat("a", 40)
	keyB := pinKey("canonical-b", "dev")
	oidB := "sha1:" + strings.Repeat("b", 40)

	marker := filepath.Join(t.TempDir(), "child-started")
	ctx := context.Background()
	release, err := cache.WithCacheMutationLockWithContext(ctx, func() (func(), error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGitPinsChildWriter$")
		cmd.Env = append(os.Environ(),
			gitPinHelperEnv+"=1",
			"PIN_CANONICAL=canonical-b",
			"PIN_REF=dev",
			"PIN_OID="+oidB,
			"PIN_MARKER="+marker,
			"XDG_STATE_HOME="+state,
			"HOME="+home,
		)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				return nil, fmt.Errorf("child never signaled merge start")
			}
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(300 * time.Millisecond)
		if pins := loadGitPins(); pins[keyB] != "" {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("child pin write escaped the cache mutation lock")
		}
		merged := loadGitPins()
		merged[keyA] = oidA
		if err := saveGitPins(merged); err != nil {
			_ = cmd.Process.Kill()
			return nil, err
		}
		return func() { _ = cmd.Wait() }, nil
	})
	if err != nil {
		t.Fatalf("parent pin write: %v", err)
	}
	if release == nil {
		t.Fatal("child process did not start")
	}
	release()

	pins := loadGitPins()
	if pins[keyA] != oidA || pins[keyB] != oidB {
		t.Fatalf("pins = %v, want both %q and %q persisted", pins, keyA, keyB)
	}
}

func TestGitPinsChildWriter(t *testing.T) {
	if os.Getenv(gitPinHelperEnv) != "1" {
		t.Skip("helper process for TestGitPinsMergeDisjointWriters")
	}
	if err := os.WriteFile(os.Getenv("PIN_MARKER"), []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeGitPins(context.Background(), map[string]string{
		pinKey(os.Getenv("PIN_CANONICAL"), os.Getenv("PIN_REF")): os.Getenv("PIN_OID"),
	}); err != nil {
		t.Fatal(err)
	}
}
