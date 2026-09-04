package pipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

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
				RepoRoot:      repoRoot,
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
				_, gcErr := cache.GarbageCollectWithLocks(false, nil, &config.Config{})
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

func TestExactLockViewLeaseBlocksGCThroughCatalogConsumption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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
	root := lockfile.GitRoot{Path: ".", SourceHarness: built.SourceHarness, RevisionSchema: "v1", RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
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
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: "+canonical+"\n    ref: HEAD\n"), 0o644); err != nil {
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
	cfg := &config.Config{APIVersion: config.APIVersionV2, Registries: []config.RegistryEntry{entry}}
	ec := config.EffectiveConfig{Repo: config.LayerResult{Source: config.RepoCommitted, Path: configPath, Config: cfg}, Merged: cfg}
	view := exactLockViewWithContext(context.Background(), ec)
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
		_, gcErr := cache.GarbageCollectWithLocks(false, nil, &config.Config{})
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
