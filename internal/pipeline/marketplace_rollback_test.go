package pipeline

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// This reconciler performs real ref resolution, exact snapshot acquisition and
// source-revision construction, then publishes through the dependency CAS. The
// production command reconciler is additionally exercised by the collision E2E.
func rollbackReconcileGit(t *testing.T, origin, lockPath string, published *[]byte, snapshot *string) DependencyReconciler {
	t.Helper()
	return func(ctx context.Context, _, _ string, _ bool, _ []StagedConfigEdit) (DependencyLockView, error) {
		_, generation, mode, doc, err := readMarketplaceLayerLock(lockPath, lockfile.ScopeRepo)
		if err != nil {
			return DependencyLockView{}, err
		}
		if len(doc.Registries) == 0 {
			registry, acquired := rollbackAcquireGit(t, origin)
			*snapshot = acquired
			doc.Registries = []lockfile.GitRegistry{registry}
		}
		data, err := lockfile.Encode(doc)
		if err != nil {
			return DependencyLockView{}, err
		}
		state, err := dependencytxn.StateDir()
		if err != nil {
			return DependencyLockView{}, err
		}
		_, err = dependencytxn.Commit(ctx, state, []dependencytxn.DesiredFile{{Path: lockPath, Expected: generation, ExpectedMode: mode, Bytes: data, Mode: mode}})
		if err != nil {
			return DependencyLockView{}, err
		}
		*published = data
		registry := doc.Registries[0]
		return DependencyLockView{CatalogOption: catalog.WithExactSnapshots(func(config.RegistryEntry) (string, string, error) {
			oid, err := cache.ParseGitOID(registry.Commit.String())
			if err != nil {
				return "", "", err
			}
			path, err := cache.OpenExactGit(registry.URL, oid)
			return path, registry.URL + "@" + registry.Commit.String(), err
		})}, nil
	}
}

func rollbackAcquireGit(t *testing.T, origin string) (lockfile.GitRegistry, string) {
	t.Helper()
	url := "https://example.invalid/rollback"
	oid, err := cache.ResolveExactGitRef(url, origin, "main")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.AcquireExactGit(url, origin, oid)
	if err != nil {
		t.Fatal(err)
	}
	built, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: snapshot, RootPath: ".", SourceHarness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := lockfile.ParseGitOID(oid.String())
	if err != nil {
		t.Fatal(err)
	}
	root := lockfile.GitRoot{Path: ".", SourceHarness: "claude", RevisionSchema: built.Manifest.SchemaVersion, RevisionDigest: lockfile.Digest(built.Manifest.RevisionDigest)}
	for _, resource := range built.Manifest.Resources {
		if resource.ArchiveDigest != "" {
			root.Resources = append(root.Resources, lockfile.GitResource{Kind: resource.Kind, Name: resource.Name, ContentDigest: lockfile.Digest(resource.ContentDigest), ArchiveDigest: lockfile.Digest(resource.ArchiveDigest)})
		}
	}
	return lockfile.GitRegistry{URL: url, Ref: "main", Commit: commit, Roots: []lockfile.GitRoot{root}}, snapshot
}

func rollbackGit(t *testing.T, origin string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = origin
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func TestMarketplaceRollbackAfterReconciledCollision(t *testing.T) {
	for _, collection := range []bool{false, true} {
		name := "skill"
		if collection {
			name = "collection"
		}
		t.Run(name, func(t *testing.T) {
			home := isolateConsumerPipeline(t)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			repo, origin := t.TempDir(), t.TempDir()
			t.Chdir(repo)
			writeFileMode(t, filepath.Join(origin, "skills", "keeper", "SKILL.md"), "---\nname: keeper\ndescription: keeper\n---\nKEEPER\n", 0o644)
			rollbackGit(t, origin, "init", "-b", "main")
			rollbackGit(t, origin, "add", ".")
			rollbackGit(t, origin, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "keeper")
			old, _ := rollbackAcquireGit(t, origin)
			warm := cache.RegistryCachePath(old.URL, "main")
			writeFileMode(t, filepath.Join(warm, "skills", "keeper", "SKILL.md"), "---\nname: keeper\ndescription: keeper\n---\nKEEPER\n", 0o644)
			fake := newLayerMarketplaceServer(t)
			registry := fake.addRegistry(t, "source")
			fake.addSkill(t, registry, "review")
			locator := fake.URL + "/r/source/skill/review"
			if collection {
				member := fake.resources["source/skill/review"]
				fake.addCollection(t, registry, "bundle", []hccartifact.CollectionMemberEntry{{RegistryID: string(registry.ID), Kind: "skill", Name: "review", Revision: string(member.revision.ContentDigest)}})
				locator = fake.URL + "/r/source/collection/bundle"
			}
			path := config.ProjectConfigPath(repo)
			original := "apiVersion: agent-marketplace/v4\n# preserve authored bytes\nharnesses: [factory]\nsources:\n  public: {marketplace: " + fake.URL + "}\n  git: {git: " + old.URL + ", ref: main, source_harness: claude}\nresources:\n  git: {all: true, required: true}\n"
			writeFileMode(t, path, original, 0o600)
			lockPath := filepath.Join(repo, ".agents", "lock.yaml")
			initial, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{old}})
			if err != nil {
				t.Fatal(err)
			}
			writeFileMode(t, lockPath, string(initial), 0o640)
			ws, err := NewWorkspace(repo, repo)
			if err != nil {
				t.Fatal(err)
			}
			if result := Sync(context.Background(), ResolveOptions{Workspace: ws, ReadOnlyExactLocks: true}); len(result.Errors) > 0 {
				t.Fatal(errors.Join(result.Errors...))
			}
			handwritten := filepath.Join(repo, ".factory", "commands", "handwritten.md")
			writeFileMode(t, handwritten, "HANDWRITTEN\n", 0o640)
			keeper := filepath.Join(repo, ".factory", "skills", "keeper", "SKILL.md")
			keeperBytes, keeperInfo := layerFreezeFile(t, keeper)
			// An unrelated ownership record must not be included in the rollback.
			ownership := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "ownership", "control", "state.json")
			writeFileMode(t, ownership, "{\"version\":1}\n", 0o600)
			ownershipBytes, ownershipInfo := layerFreezeFile(t, ownership)
			var published []byte
			var acquired string
			opts := InstallOptions{Args: []string{locator}, RepoRoot: repo, Scope: ScopeRepo, DependencyReconcile: rollbackReconcileGit(t, origin, lockPath, &published, &acquired), CatalogOption: []catalog.LoadOption{catalog.WithCloneOrPull(func(string, string) (string, error) { return warm, nil })}}
			prepared, err := PrepareMarketplaceInstall(context.Background(), opts)
			if err != nil {
				t.Fatalf("prepare healthy lock control: %v", err)
			}
			if err := os.Remove(lockPath); err != nil {
				t.Fatal(err)
			}
			writeFileMode(t, filepath.Join(origin, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: Git collision\n---\nUNREVIEWED GIT BODY\n", 0o644)
			rollbackGit(t, origin, "add", ".")
			rollbackGit(t, origin, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "collision")
			trackInstallOverlayStaging(t)
			_, err = CommitPreparedMarketplaceInstall(context.Background(), prepared)
			if !errors.Is(err, ErrMarketplaceInstallCollision) {
				t.Fatalf("reconciled collision = %v", err)
			}
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != original {
				t.Fatalf("config not restored: %v\n%s", readErr, got)
			}
			if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("config mode not restored: %v, %v", info, statErr)
			}
			if len(published) == 0 || acquired == "" {
				t.Fatal("dependency reconciliation did not acquire and publish")
			}
			got := layerMarketplaceLockDoc(t, lockPath)
			if len(got.Marketplaces) != 0 || len(got.Registries) != 1 || got.Registries[0].Commit == old.Commit {
				t.Fatalf("rollback did not retain reconciled Git and remove prepared evidence: %#v", got)
			}
			gitBefore, _, _ := strings.Cut(string(published), "\nmarketplaces:")
			lockBytes, readErr := os.ReadFile(lockPath)
			if readErr != nil || strings.TrimSpace(string(lockBytes)) != strings.TrimSpace(gitBefore) {
				t.Fatalf("reconciled Git record bytes changed: %v\n%s", readErr, lockBytes)
			}
			if ec := LoadEffective(context.Background(), ConfigLoadStrict, config.LoadEffectiveOptions{RepoRoot: repo}, nil); len(ec.Errors) > 0 {
				t.Fatalf("restored config/lock are incongruent: %v", ec.Errors)
			}
			layerAssertFileUnchanged(t, keeper, keeperBytes, keeperInfo)
			layerAssertFileUnchanged(t, ownership, ownershipBytes, ownershipInfo)
			if got, readErr := os.ReadFile(handwritten); readErr != nil || string(got) != "HANDWRITTEN\n" {
				t.Fatalf("handwritten output changed: %q, %v", got, readErr)
			}
			if _, statErr := os.Stat(filepath.Join(repo, ".factory", "skills", "review")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("refused resource materialized: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(acquired, "skills", "review", "SKILL.md")); statErr != nil {
				t.Fatalf("rollback removed newly acquired cache: %v", statErr)
			}
		})
	}
}

func TestMarketplaceRollbackHealthyLockAndNoop(t *testing.T) {
	isolateConsumerPipeline(t)
	repo := t.TempDir()
	path := config.ProjectConfigPath(repo)
	writeFileMode(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "keeper")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	keeper := layerPreparePins(t, fake, "skill", "keeper")
	mutate := func(pins []config.MarketplaceResourcePin, sync func() ([]error, error)) error {
		_, err := MutateMarketplaceLayer(context.Background(), repo, repo, path, config.RepoCommitted, func(m *MarketplaceLayerMutation) error {
			return m.IncludePins(entry, pins)
		}, sync)
		return err
	}
	if err := mutate(keeper, nil); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	configBefore, configInfo := layerFreezeFile(t, path)
	lockBefore, lockInfo := layerFreezeFile(t, lockPath)
	if err := mutate(keeper, func() ([]error, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	layerAssertFileUnchanged(t, path, configBefore, configInfo)
	layerAssertFileUnchanged(t, lockPath, lockBefore, lockInfo)
	failure := errors.New("materialization refused")
	if err := mutate(keeper, func() ([]error, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Fatalf("no-op failure = %v", err)
	}
	layerAssertFileUnchanged(t, path, configBefore, configInfo)
	layerAssertFileUnchanged(t, lockPath, lockBefore, lockInfo)
	review := layerPreparePins(t, fake, "skill", "review")
	if err := mutate(review, func() ([]error, error) { return nil, failure }); !errors.Is(err, failure) || errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("healthy-lock failure = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, configBefore) {
		t.Fatalf("existing config not restored: %v\n%s", err, got)
	}
	if got, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(got, lockBefore) {
		t.Fatalf("existing exact evidence not restored: %v\n%s", err, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := MutateMarketplaceLayer(ctx, repo, repo, path, config.RepoCommitted, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, review)
	}, func() ([]error, error) {
		cancel()
		return nil, ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled install = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, configBefore) {
		t.Fatalf("cancelled install did not restore config: %v\n%s", err, got)
	}
	if got, err := os.ReadFile(lockPath); err != nil || !bytes.Equal(got, lockBefore) {
		t.Fatalf("cancelled install did not restore exact evidence: %v\n%s", err, got)
	}
}

func TestMarketplaceRollbackConcurrentWriterIsAtomic(t *testing.T) {
	for _, edit := range []string{"config", "config_success", "config_mode", "include", "lock", "lock_mode", "changed_pin", "removed_lock", "foreign_git_lock"} {
		t.Run(edit, func(t *testing.T) {
			isolateConsumerPipeline(t)
			repo := t.TempDir()
			path := config.ProjectConfigPath(repo)
			writeFileMode(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
			fake := newLayerMarketplaceServer(t)
			registry := fake.addRegistry(t, "source")
			fake.addSkill(t, registry, "review")
			pins := layerPreparePins(t, fake, "skill", "review")
			lockPath := filepath.Join(repo, ".agents", "lock.yaml")
			failure := errors.New("sync failed")
			fragment := filepath.Join(repo, ".agents", "selection.yaml")
			if edit == "include" {
				writeFileMode(t, fragment, "harnesses: [factory]\n", 0o640)
				writeFileMode(t, path, "apiVersion: agent-marketplace/v4\ninclude: selection.yaml\n", 0o600)
			}
			trackInstallOverlayStaging(t)
			var configDuring, lockDuring []byte
			_, err := MutateMarketplaceLayer(context.Background(), repo, repo, path, config.RepoCommitted, func(m *MarketplaceLayerMutation) error {
				return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins)
			}, func() ([]error, error) {
				switch edit {
				case "config", "config_success":
					data, _ := os.ReadFile(path)
					writeFileMode(t, path, string(data)+"# independent edit\n", 0o600)
				case "config_mode":
					if err := os.Chmod(path, 0o640); err != nil {
						t.Fatal(err)
					}
				case "include":
					writeFileMode(t, fragment, "harnesses: [factory]\n# independent edit\n", 0o640)
				case "changed_pin":
					doc := layerMarketplaceLockDoc(t, lockPath)
					doc.Marketplaces[0].Resources[0].Name = "foreign-name"
					data, encodeErr := lockfile.Encode(doc)
					if encodeErr != nil {
						t.Fatal(encodeErr)
					}
					writeFileMode(t, lockPath, string(data), 0o644)
				case "lock":
					data, _ := os.ReadFile(lockPath)
					writeFileMode(t, lockPath, string(data)+"# independent edit\n", 0o644)
				case "lock_mode":
					if err := os.Chmod(lockPath, 0o600); err != nil {
						t.Fatal(err)
					}
				case "removed_lock":
					if err := os.Remove(lockPath); err != nil {
						t.Fatal(err)
					}
				case "foreign_git_lock":
					// A parseable replacement lock is not proof that this callback
					// published it, even if an exact cache already exists for it.
					t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
					origin := t.TempDir()
					writeFileMode(t, filepath.Join(origin, "skills", "other", "SKILL.md"), "---\nname: other\ndescription: other\n---\n", 0o644)
					rollbackGit(t, origin, "init", "-b", "main")
					rollbackGit(t, origin, "add", ".")
					rollbackGit(t, origin, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "foreign")
					git, _ := rollbackAcquireGit(t, origin)
					doc := layerMarketplaceLockDoc(t, lockPath)
					doc.Registries = []lockfile.GitRegistry{git}
					data, encodeErr := lockfile.Encode(doc)
					if encodeErr != nil {
						t.Fatal(encodeErr)
					}
					writeFileMode(t, lockPath, string(data), 0o644)
				}
				configDuring, _ = os.ReadFile(path)
				lockDuring, _ = os.ReadFile(lockPath)
				if edit == "config_success" {
					return nil, nil
				}
				return nil, &DependencyCommittedSyncError{Err: failure}
			})
			if (edit != "config_success" && !errors.Is(err, failure)) || !errors.Is(err, dependencytxn.ErrGenerationConflict) {
				t.Errorf("expected sync failure plus typed rollback conflict: %v", err)
			}
			configAfter, _ := os.ReadFile(path)
			lockAfter, _ := os.ReadFile(lockPath)
			if !bytes.Equal(configAfter, configDuring) || !bytes.Equal(lockAfter, lockDuring) {
				t.Fatalf("partial rollback overwrote the concurrent generation:\nconfig %s\nlock %s", configAfter, lockAfter)
			}
			if edit == "lock_mode" {
				if info, statErr := os.Stat(lockPath); statErr != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("concurrent lock mode was overwritten: %v, %v", info, statErr)
				}
			}
			state, stateErr := dependencytxn.StateDir()
			if stateErr != nil {
				t.Fatal(stateErr)
			}
			if err := dependencytxn.CheckPending(state); err != nil {
				t.Fatalf("rollback left pending transaction: %v", err)
			}
		})
	}
}
