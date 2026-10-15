package pipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

func emptyGCReachability(context.Context) (cache.GCReachability, error) {
	return cache.GCReachability{}, nil
}

func TestSyncExactLeaseBlocksGCThroughDiscoveryAndMaterialization(t *testing.T) {
	for _, phase := range []string{"discovery", "before-materialization"} {
		t.Run(phase, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			origin := filepath.Join(t.TempDir(), "origin")
			if err := os.MkdirAll(filepath.Join(origin, ".factory", "skills", "keeper"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(origin, ".factory", "skills", "keeper", "SKILL.md"), []byte("---\nname: keeper\ndescription: kept\n---\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-m", "seed"}} {
				cmd := exec.Command("git", args...)
				cmd.Dir = origin
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, output)
				}
			}
			canonical := "https://example.invalid/sync-gc.git"
			oid, err := cache.ResolveExactGitRefWithContext(context.Background(), canonical, origin, "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := cache.AcquireExactGit(canonical, origin, oid)
			if err != nil {
				t.Fatal(err)
			}
			repoRoot := t.TempDir()
			blocked := make(chan struct{})
			resume := make(chan struct{})
			exactOption := catalog.WithExactSnapshots(func(config.RegistryEntry) (string, string, error) {
				return snapshot, canonical + "@" + oid.String(), nil
			})
			repoCfg := &config.Config{Registries: []config.RegistryEntry{{URL: canonical, Ref: "HEAD", SourceHarness: "factory", Skills: config.KindSelector{Include: []string{"keeper"}}}}}
			opts := ResolveOptions{
				Workspace:     mustWorkspace(t, repoRoot),
				NoBackup:      true,
				CatalogOption: []catalog.LoadOption{exactOption},
				Loaders: Loaders{
					Org:    func() (*config.Config, error) { return nil, nil },
					Global: func() (*config.Config, error) { return &config.Config{Harnesses: []string{"factory"}}, nil },
					Repo:   func() (*config.Config, error) { return repoCfg, nil },
					Local:  func() (*config.LocalConfig, error) { return nil, nil },
				},
				ExactLockLease: func(ctx context.Context) (func(), error) {
					return cache.AcquireExactGitNamespaceLeaseWithContext(ctx)
				},
			}
			ctx := context.Background()
			if phase == "discovery" {
				opts.CatalogOption = append(opts.CatalogOption, catalog.WithDiscoverFunc(func(dir, sourceHarness string) (catalog.LoadedCatalog, error) {
					close(blocked)
					<-resume
					return catalog.Discover(dir, sourceHarness)
				}))
			} else {
				ctx = context.WithValue(ctx, syncPhaseHookContextKey{}, func(got string) {
					if got == phase {
						close(blocked)
						<-resume
					}
				})
			}
			syncDone := make(chan SyncResult, 1)
			go func() { syncDone <- Sync(ctx, opts) }()
			<-blocked

			gcDone := make(chan error, 1)
			go func() {
				_, gcErr := cache.GarbageCollect(context.Background(), false, emptyGCReachability)
				gcDone <- gcErr
			}()
			select {
			case gcErr := <-gcDone:
				t.Fatalf("GC completed while Sync was blocked in %s: %v", phase, gcErr)
			case <-time.After(100 * time.Millisecond):
			}
			close(resume)
			result := <-syncDone
			if len(result.Errors) > 0 {
				t.Fatal(errors.Join(result.Errors...))
			}
			if err := <-gcDone; err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("GC did not remove snapshot after Sync consumption: %v", err)
			}
		})
	}
}

func exactLockViewGCFixture(t *testing.T) (config.EffectiveConfig, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	origin := filepath.Join(t.TempDir(), "origin")
	if err := os.MkdirAll(filepath.Join(origin, ".factory", "skills", "keeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, ".factory", "skills", "keeper", "SKILL.md"), []byte("---\nname: keeper\ndescription: kept\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-m", "seed"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = origin
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	canonical, err := readOnlyCanonicalURL("https://example.invalid/lockview-gc.git")
	if err != nil {
		t.Fatal(err)
	}
	oid, err := cache.ResolveExactGitRefWithContext(context.Background(), canonical, origin, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.AcquireExactGit(canonical, origin, oid)
	if err != nil {
		t.Fatal(err)
	}
	built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot, RootPath: ".", SourceHarness: "factory"})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := lockfile.ParseGitOID(oid.String())
	if err != nil {
		t.Fatal(err)
	}
	root := lockfile.GitRoot{Path: ".", SourceHarness: built.SourceHarness, RevisionSchema: built.Manifest.SchemaVersion, RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
	for _, resource := range built.Manifest.Resources {
		if resource.ArchiveDigest == "" {
			continue
		}
		support := make(map[string]lockfile.Digest, len(resource.SupportFiles))
		for name, digest := range resource.SupportFiles {
			support[name] = lockfile.Digest(digest)
		}
		root.Resources = append(root.Resources, lockfile.GitResource{Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest), ArchiveDigest: lockfile.Digest(resource.ArchiveDigest), SupportFiles: support})
	}

	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  lockview:\n    git: "+canonical+"\n    ref: HEAD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockBytes, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: canonical, Ref: "HEAD", Commit: commit, Roots: []lockfile.GitRoot{root}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "lock.yaml"), lockBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := config.RegistryEntry{URL: canonical, Ref: "HEAD", Source: config.RepoCommitted}
	cfg := &config.Config{APIVersion: config.APIVersionV4, Registries: []config.RegistryEntry{entry}}
	ec := config.EffectiveConfig{Repo: config.LayerResult{Source: config.RepoCommitted, Path: configPath, Config: cfg}, Merged: cfg}
	return ec, snapshot
}

func TestExactLockViewLeaseBlocksGCThroughCatalogConsumption(t *testing.T) {
	ec, snapshot := exactLockViewGCFixture(t)
	_, view := exactLockViewWithContext(context.Background(), ec, false)
	defer view.release()
	if len(view.errors) > 0 {
		t.Fatal(errors.Join(view.errors...))
	}
	lookup := ec.Merged.Registries[0]
	lookup.URL = view.gitMetadata[0].URL
	path, _, err := view.exactSnapshot(lookup)
	if err != nil {
		t.Fatal(err)
	}

	gcStarted := make(chan struct{})
	gcDone := make(chan error, 1)
	go func() {
		close(gcStarted)
		_, gcErr := cache.GarbageCollect(context.Background(), false, emptyGCReachability)
		gcDone <- gcErr
	}()
	<-gcStarted
	select {
	case gcErr := <-gcDone:
		t.Fatalf("GC completed before exact catalog consumption: %v", gcErr)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := os.ReadFile(filepath.Join(path, ".factory", "skills", "keeper", "SKILL.md")); err != nil {
		t.Fatalf("consume exact catalog while GC waits: %v", err)
	}
	view.release()
	view.release = func() {}
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GC did not remove snapshot after catalog consumption: %v", err)
	}
}

type exactAcquisitionContext struct {
	context.Context
	reached chan struct{}
	once    sync.Once
}

func (ctx *exactAcquisitionContext) Deadline() (time.Time, bool) {
	ctx.once.Do(func() { close(ctx.reached) })
	return ctx.Context.Deadline()
}

func awaitCacheLease[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("cache lease ordering barrier timed out")
		var zero T
		return zero
	}
}

func TestMixedLockViewCacheLeaseOrdering(t *testing.T) {
	for _, preheld := range []bool{true, false} {
		name := "standalone-exact-before-mutable"
		if preheld {
			name = "preheld-exact-with-concurrent-gc"
		}
		t.Run(name, func(t *testing.T) {
			ec, snapshot := exactLockViewGCFixture(t)
			orgEntry := config.RegistryEntry{URL: "https://example.invalid/unlocked-org.git", Ref: "main", Source: config.Organization}
			orgPath := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(orgPath, []byte("apiVersion: agent-marketplace/v4\nsources:\n  organization:\n    git: "+orgEntry.URL+"\n    ref: main\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ec.Organization = config.LayerResult{Source: config.Organization, Path: orgPath, Config: &config.Config{Registries: []config.RegistryEntry{orgEntry}}}
			ec.Merged = &config.Config{Registries: append([]config.RegistryEntry{orgEntry}, ec.Repo.Config.Registries...)}
			mutableRoot := cache.RegistryCachePath(orgEntry.URL, orgEntry.Ref)
			if err := os.MkdirAll(filepath.Join(mutableRoot, ".factory", "skills", "organization"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(mutableRoot, ".factory", "skills", "organization", "SKILL.md"), []byte("---\nname: organization\ndescription: unlocked\n---\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			viewCtx, view := exactLockViewWithContext(context.Background(), ec, false)
			defer view.release()
			if len(view.errors) > 0 {
				t.Fatal(errors.Join(view.errors...))
			}
			if path, identity, err := view.exactSnapshot(orgEntry); err != nil || path != "" || identity != "" {
				t.Fatalf("organization fallback = %q, %q, %v", path, identity, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			barrier := &exactAcquisitionContext{Context: ctx, reached: make(chan struct{})}
			var finish func()
			releaseBlocking := cancel
			if preheld {
				gcDone := make(chan struct{})
				var gcErr error
				go func() {
					defer close(gcDone)
					_, gcErr = cache.GarbageCollect(barrier, false, emptyGCReachability)
				}()
				t.Cleanup(func() {
					cancel()
					view.release()
					awaitCacheLease(t, gcDone)
				})
				awaitCacheLease(t, barrier.reached)
				finish = func() {
					select {
					case <-gcDone:
						t.Fatalf("GC finished before final snapshot consumption: %v", gcErr)
					default:
					}
					if _, err := os.ReadFile(filepath.Join(snapshot, ".factory", "skills", "keeper", "SKILL.md")); err != nil {
						t.Fatal(err)
					}
					view.release()
					awaitCacheLease(t, gcDone)
					if gcErr != nil {
						t.Fatal(gcErr)
					}
					if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("snapshot survived final release: %v", err)
					}
				}
			} else {
				view.release()
				viewCtx = barrier
				mutableHeld := make(chan struct{})
				mutableDone := make(chan error, 1)
				resume := make(chan struct{})
				var once sync.Once
				releaseMutable := func() { once.Do(func() { close(resume) }) }
				go func() {
					_, err := cache.WithCacheMutationLock(func() (struct{}, error) {
						close(mutableHeld)
						<-resume
						return struct{}{}, nil
					})
					mutableDone <- err
				}()
				releaseBlocking = releaseMutable
				t.Cleanup(releaseMutable)
				awaitCacheLease(t, mutableHeld)
				finish = func() {
					awaitCacheLease(t, barrier.reached)
					releaseMutable()
					if err := awaitCacheLease(t, mutableDone); err != nil {
						t.Fatal(err)
					}
				}
			}
			loadDone := make(chan struct{})
			var loaded catalog.LoadResult
			go func() {
				defer close(loadDone)
				loaded = catalog.LoadRegistriesWithContext(viewCtx, ec.Merged, filepath.Dir(filepath.Dir(ec.Repo.Path)), catalog.LoadModeInspect, view.catalogOption)
			}()
			t.Cleanup(func() {
				releaseBlocking()
				awaitCacheLease(t, loadDone)
			})
			if !preheld {
				finish()
			}
			select {
			case <-loadDone:
			case <-time.After(2 * time.Second):
				cancel()
				view.release()
				awaitCacheLease(t, loadDone)
				t.Fatal("mixed discovery stalled behind GC while retaining the exact shared lease")
			}
			if len(loaded.Errors) > 0 {
				t.Fatal(errors.Join(loaded.Errors...))
			}
			found := make(map[string]bool)
			for _, cat := range loaded.Catalogs {
				for _, entry := range cat.Entries {
					found[entry.Name] = true
				}
			}
			if len(found) != 2 || !found["keeper"] || !found["organization"] {
				t.Fatalf("mixed resources = %v", found)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("organization lock unexpectedly created: %v", err)
			}
			if preheld {
				finish()
			}
		})
	}
}
