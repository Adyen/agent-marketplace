package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestWritingCommandsCreateMissingV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"sync", "update", "lock"} {
		for _, scope := range []lockfile.Scope{lockfile.ScopeUser, lockfile.ScopeRepo} {
			t.Run(command+"/"+string(scope), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				server := newV4MarketplaceLockServer(t)
				defer server.Close()
				repo := t.TempDir()
				configRoot := repo
				if scope == lockfile.ScopeUser {
					configRoot = home
				}
				configPath := filepath.Join(configRoot, ".agents", "config.yaml")
				writeConfig(t, configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
				before, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				withWorkingDir(t, repo)

				output, err := executeCommand(command)
				if err != nil {
					t.Fatalf("%s: %v\n%s", command, err, output)
				}
				after, err := os.ReadFile(configPath)
				if err != nil || string(after) != string(before) {
					t.Fatalf("%s changed authored config: %v\nbefore:\n%s\nafter:\n%s", command, err, before, after)
				}
				doc := readLockDocument(t, filepath.Join(configRoot, ".agents", "lock.yaml"))
				if doc.APIVersion != lockfile.APIVersionV2 || doc.Scope != scope || len(doc.Marketplaces) != 1 {
					t.Fatalf("%s lock header = %#v", command, doc)
				}
				locked := doc.Marketplaces[0]
				if locked.URL != server.URL || locked.InstanceID != "mp" || len(locked.Resources) != 1 {
					t.Fatalf("%s Marketplace lock = %#v", command, locked)
				}
				resource := locked.Resources[0]
				if resource.RegistryName != "tools" || resource.RegistryID != "src" || resource.Kind != "skill" || resource.Name != "review" || resource.ArchiveDigest == "" {
					t.Fatalf("%s resource lock = %#v", command, resource)
				}
				if command == "sync" && scope == lockfile.ScopeRepo {
					if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "review", "SKILL.md")); err != nil {
						t.Fatalf("%s did not materialize review: %v", command, err)
					}
				}
			})
		}
	}
}

func TestSyncBootstrapsWritableMarketplaceLockWithTransientOrganizationMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	orgServer := newV4MarketplaceLockServer(t)
	defer orgServer.Close()
	repoServer := newV4MarketplaceLockServer(t)
	defer repoServer.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: `+orgServer.URL+`}
resources:
  organization: {include: [tools/skill/review]}
`)
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)

	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  writable: {marketplace: `+repoServer.URL+`}
`)
	withWorkingDir(t, repo)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	doc := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Marketplaces) != 1 || doc.Marketplaces[0].URL != repoServer.URL {
		t.Fatalf("writable Marketplace lock = %#v", doc)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("organization resource was not materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync wrote organization lock: %v", err)
	}
}

func TestConfiguredInstallPreparesDependenciesWithTransientOrganizationMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	orgServer := newV4MarketplaceLockServer(t)
	defer orgServer.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: `+orgServer.URL+`}
resources:
  organization: {include: [tools/skill/review]}
`)
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)

	origin, working := createBareRepoWithWorkingClone(t, "install-source.git")
	writeSkill(t, working, "git-review", "Git review", nil)
	commitAndPushAll(t, working, "seed install source")
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repo)

	output, err := executeCommand("install", "git-review")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "git-review", "SKILL.md")); err != nil {
		t.Fatalf("git-review was not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("organization review was not materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install wrote organization lock: %v", err)
	}
}

func TestMarketplaceInstallRevalidatesAfterConsentWithTransientOrganizationMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	orgServer := newV4MarketplaceLockServer(t)
	defer orgServer.Close()
	targetServer := newV4MarketplaceLockServerWithIdentity(t, "target-mp", "target-review")
	defer targetServer.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: `+orgServer.URL+`}
resources:
  organization: {include: [tools/skill/review]}
`)
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses: [factory]
marketplaces:
  - url: `+targetServer.URL+`
    instance_id: target-mp
    alias: target
`)
	repo := t.TempDir()
	withWorkingDir(t, repo)

	output, err := executeCommand("install", "tools/skill/target-review", "--marketplace", "target", "--yes")
	if err != nil {
		t.Fatalf("Marketplace install: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("organization review was not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "target-review", "SKILL.md")); err != nil {
		t.Fatalf("target-review was not materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install wrote organization lock: %v", err)
	}
}

func TestInstallCommitReuseDoesNotAcquireTransientOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: `+server.URL+`}
resources:
  organization: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")

	reuse := &preparedInstallLocks{
		documents: make(map[string]lockfile.Document),
		lockPaths: make(map[config.Source]string),
		bindings:  make(map[string][]lockfile.GitRegistry),
	}
	_, _, _, err := prepareRuntimeLocks(context.Background(), t.TempDir(), "", lockMode{reuseInstall: reuse})
	if err == nil || !marketplacelock.HasRequiredError([]error{err}) {
		t.Fatalf("install commit without prepared transient state error = %v, want v4 Marketplace lock requirement", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("install commit cache-only revalidation made %d Marketplace requests", got)
	}
}

func TestSyncAcquiresMissingV4OrganizationMarketplaceLockTransiently(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	orgConfig := []byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: ` + server.URL + `}
resources:
  hosted: {include: [tools/skill/review]}
`)
	if err := os.WriteFile(orgPath, orgConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync rejected organization Marketplace config without a deployed lock: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("sync did not materialize organization Marketplace resource: %v\n%s", err, output)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("consumer created organization lock: %v", err)
	}
	after, err := os.ReadFile(orgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(orgConfig) {
		t.Fatalf("sync changed organization config:\nbefore:\n%s\nafter:\n%s", orgConfig, after)
	}
	if got := requests.Load(); got != 5 {
		t.Fatalf("sync made %d Marketplace requests, want one five-request acquisition without a second resolution pass", got)
	}
}

func TestTUISyncReusesTransientOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	if err := os.Chmod(authority, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)

	if err := runTUIDependencyLifecycle(context.Background(), repo, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("organization resource was not materialized: %v", err)
	}
	if got := requests.Load(); got != 5 {
		t.Fatalf("TUI sync made %d Marketplace requests, want one five-request invocation-scoped acquisition", got)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("TUI sync wrote organization lock: %v", err)
	}
}

func TestTransientOrganizationMarketplaceLockIsReusedWhileInputsMatch(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	opts := pipeline.ResolveOptions{
		RepoRoot: repo,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         filepath.Join(repo, ".agents", "config.yaml"),
			LocalPath:        filepath.Join(repo, ".agents", "config.local.yaml"),
		},
	}
	ctx := pipeline.WithTransientOrganizationMarketplaceLockCache(context.Background())
	for run := range 2 {
		result := pipeline.Resolve(ctx, opts)
		if err := errors.Join(result.Errors...); err != nil {
			t.Fatalf("resolve %d: %v", run+1, err)
		}
	}
	if got := requests.Load(); got != 5 {
		t.Fatalf("two matching resolves made %d Marketplace requests, want one five-request invocation-scoped acquisition", got)
	}
}

func TestSyncNeverReplacesPresentOrganizationMarketplaceLockTransiently(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name string
		lock func(*testing.T, string) []byte
	}{
		{
			name: "malformed",
			lock: func(*testing.T, string) []byte {
				return []byte("not: [valid\n")
			},
		},
		{
			name: "mismatched endpoint",
			lock: func(t *testing.T, _ string) []byte {
				t.Helper()
				raw, err := lockfile.Encode(lockfile.Document{
					APIVersion: lockfile.APIVersionV2,
					Scope:      lockfile.ScopeOrganization,
					Marketplaces: []lockfile.MarketplaceLock{{
						URL:        "https://different.example",
						InstanceID: "other",
					}},
				})
				if err != nil {
					t.Fatal(err)
				}
				return raw
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			authority := t.TempDir()
			orgPath := filepath.Join(authority, "config.yaml")
			writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
			lockPath := filepath.Join(authority, "lock.yaml")
			before := test.lock(t, server.URL)
			if err := os.WriteFile(lockPath, before, 0o644); err != nil {
				t.Fatal(err)
			}
			config.SetTestOrganizationConfigPath(t, orgPath)
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			withWorkingDir(t, t.TempDir())

			for _, args := range [][]string{{"sync"}, {"update"}, {"install", server.URL + "/r/tools/skill/review"}} {
				if output, err := executeCommand(args...); err == nil {
					t.Fatalf("%s unexpectedly replaced a present organization lock:\n%s", args[0], output)
				}
			}
			if requests.Load() != 0 {
				t.Fatalf("present invalid lock triggered %d Marketplace requests", requests.Load())
			}
			after, err := os.ReadFile(lockPath)
			if err != nil || string(after) != string(before) {
				t.Fatalf("organization lock changed: %q, %v", after, err)
			}
		})
	}
}

func TestSyncOfflineDoesNotAcquireMissingOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("sync", "--offline"); err == nil {
		t.Fatalf("offline sync unexpectedly acquired a transient organization lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("offline sync made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("offline sync created organization lock: %v", err)
	}
}

func TestSyncCheckDoesNotAcquireMissingOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("sync", "--check"); err == nil {
		t.Fatalf("sync --check unexpectedly acquired a transient organization lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("sync --check made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync --check created organization lock: %v", err)
	}
}

func TestLockCheckDoesNotAcquireMissingOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("lock", "--check"); err == nil {
		t.Fatalf("lock --check unexpectedly accepted a missing organization lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("lock --check made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("lock --check created organization lock: %v", err)
	}
}

func TestSyncTransientOrganizationLockHonorsRevocationOverride(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var coordinate string
	server := newV4MarketplaceLockServerWithRevision(t, nil, func(revision *store.AssessedResourceRevision) {
		coordinate = "mp/src/skill/review@" + string(revision.ContentDigest)
		revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: string(revision.ContentDigest), Reason: "test"}
	})
	defer server.Close()
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("sync", "--allow-revoked", coordinate); err != nil {
		t.Fatalf("sync with exact revocation override: %v\n%s", err, output)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync created organization lock: %v", err)
	}
}

func TestSyncWritableMarketplaceLockHonorsRevocationOverride(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var coordinate string
	server := newV4MarketplaceLockServerWithRevision(t, nil, func(revision *store.AssessedResourceRevision) {
		coordinate = "mp/src/skill/review@" + string(revision.ContentDigest)
		revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: string(revision.ContentDigest), Reason: "test"}
	})
	defer server.Close()
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync", "--allow-revoked", coordinate); err != nil {
		t.Fatalf("sync with exact revocation override: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); err != nil {
		t.Fatalf("sync did not create writable Marketplace lock: %v", err)
	}
}

func TestSyncFailsClosedWhenOrganizationConfigChangesDuringTransientAcquisition(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	mutated := make(chan error, 1)
	var once atomic.Bool
	var orgConfig []byte
	server := newV4MarketplaceLockServerWithHook(t, func() {
		if !once.CompareAndSwap(false, true) {
			return
		}
		mutated <- os.WriteFile(orgPath, append(append([]byte(nil), orgConfig...), []byte("# changed concurrently\n")...), 0o644)
	})
	defer server.Close()
	orgConfig = []byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: ` + server.URL + `}
resources:
  hosted: {include: [tools/skill/review]}
`)
	if err := os.WriteFile(orgPath, orgConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("sync")
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("sync error = %v, want generation conflict\n%s", err, output)
	}
	if !once.Load() {
		t.Fatal("transient acquisition did not reach Marketplace")
	}
	if mutationErr := <-mutated; mutationErr != nil {
		t.Fatalf("mutate organization config: %v", mutationErr)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync created organization lock after config race: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".factory", "skills", "review")); !os.IsNotExist(err) {
		t.Fatalf("sync materialized from raced organization config: %v", err)
	}
}

func TestMissingV4MarketplaceLockHonorsAllowlistBeforeNetwork(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v2\nallowed_marketplaces: ['^https://allowed\\.example$']\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync"); err == nil {
		t.Fatalf("sync unexpectedly bypassed Marketplace allowlist:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("disallowed Marketplace received %d requests", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("disallowed Marketplace created a lock: %v", err)
	}
}

func TestSyncLockedDoesNotBootstrapMissingV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync", "--locked"); err == nil {
		t.Fatalf("sync --locked unexpectedly bootstrapped a lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("sync --locked made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync --locked created a lock: %v", err)
	}
}

func TestLockCreatesMissingEndpointOnlyV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
	}))
	defer server.Close()
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: "+server.URL+"}\n")
	withWorkingDir(t, repo)

	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("lock endpoint-only source: %v\n%s", err, output)
	}
	doc := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != "mp" || len(doc.Marketplaces[0].Resources) != 0 {
		t.Fatalf("endpoint-only lock = %#v", doc)
	}
}

func TestInstallFromBootstrapsMissingWritableV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, initial := range []string{"absent", "v1"} {
		t.Run(initial, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			server := newV4MarketplaceLockServer(t)
			defer server.Close()

			repo := t.TempDir()
			repoConfigPath := filepath.Join(repo, ".agents", "config.yaml")
			writeConfig(t, repoConfigPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  marketplace: {marketplace: `+server.URL+`}
resources:
  marketplace: {include: [tools/skill/review]}
`)
			repoLockPath := filepath.Join(repo, ".agents", "lock.yaml")
			if initial == "v1" {
				data, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo})
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, repoLockPath, string(data))
			}
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses: [factory]
`)
			withWorkingDir(t, repo)
			registry := t.TempDir()
			writeFile(t, filepath.Join(registry, "pstack", "skills", "show-me-your-work", "SKILL.md"), `---
name: show-me-your-work
description: Show your work
---
Show your work.
`)

			output, err := executeCommand("install", "--from", registry, "--path", "pstack/skills/show-me-your-work", "--scope", "user")
			if err != nil {
				t.Fatalf("install --from: %v: %s", err, output)
			}
			doc := readLockDocument(t, repoLockPath)
			if doc.APIVersion != lockfile.APIVersionV2 || doc.Scope != lockfile.ScopeRepo || len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
				t.Fatalf("bootstrapped repo lock = %#v", doc)
			}
			if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "review", "SKILL.md")); err != nil {
				t.Fatalf("Marketplace resource was not materialized: %v", err)
			}
			if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "show-me-your-work", "SKILL.md")); err != nil {
				t.Fatalf("--from resource was not materialized: %v", err)
			}
		})
	}
}

func TestInstallFromDirectoryIntoV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceLockServer(t)
	defer server.Close()
	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userConfigPath, `# preserve this user comment
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  marketplace: {marketplace: `+server.URL+`}
resources:
  marketplace: {include: [tools/skill/review]}
`)
	repo := t.TempDir()
	withWorkingDir(t, repo)
	registry := t.TempDir()
	skillRoot := filepath.Join(registry, "pstack", "skills", "show-me-your-work")
	writeFile(t, filepath.Join(skillRoot, "SKILL.md"), `---
name: show-me-your-work
description: Show your work
---
Show your work.
`)

	output, err := executeCommand("install", "--from", registry, "--path", "pstack/skills/show-me-your-work/", "--scope", "user")
	if err != nil {
		t.Fatalf("install into v4 user config: %v: %s", err, output)
	}
	data, err := os.ReadFile(userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# preserve this user comment") || strings.Contains(string(data), "registries:") {
		t.Fatalf("v4 user config lost authored content or gained legacy registries: %s", data)
	}
	v4, err := config.LoadV4ConfigFromPath(userConfigPath)
	if err != nil {
		t.Fatalf("edited user config is not valid v4: %v", err)
	}
	if v4.Sources["marketplace"].Kind != config.V4SourceMarketplace {
		t.Fatalf("existing Marketplace source lost: %#v", v4.Sources)
	}
	var directoryName string
	for name, source := range v4.Sources {
		if source.Kind == config.V4SourceDirectory && source.Directory == skillRoot {
			directoryName = name
		}
	}
	if directoryName == "" {
		t.Fatalf("directory source %q not saved: %#v", skillRoot, v4.Sources)
	}
	group := v4.Groups[directoryName]
	if len(group.Include) != 1 || group.Include[0].Locator != "skill/show-me-your-work" {
		t.Fatalf("requested skill not selected in v4: %#v", group)
	}
	doc := readLockDocument(t, filepath.Join(home, ".agents", "lock.yaml"))
	if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
		t.Fatalf("v4 Marketplace lock changed incorrectly: %#v", doc)
	}
	for _, name := range []string{"review", "show-me-your-work"} {
		if _, err := os.Stat(filepath.Join(home, ".factory", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("%s was not materialized at user scope: %v", name, err)
		}
	}
}

func TestConfiguredInstallIntoV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	repo := t.TempDir()
	registry := t.TempDir()
	writeFile(t, filepath.Join(registry, "skills", "user-review", "SKILL.md"), `---
name: user-review
description: Review changes
---
Review changes.
`)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  local: {directory: `+registry+`}
`)
	withWorkingDir(t, repo)

	output, err := executeCommand("install", "user-review", "--scope", "user")
	if err != nil {
		t.Fatalf("configured install into v4: %v: %s", err, output)
	}
	v4, err := config.LoadV4ConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Groups["local"].Include) != 1 || v4.Groups["local"].Include[0].Locator != "skill/user-review" {
		t.Fatalf("v4 selection missing: %#v", v4.Groups["local"])
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "user-review", "SKILL.md")); err != nil {
		t.Fatalf("configured resource was not materialized: %v", err)
	}
}

func TestInstallGitFromIntoV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "v4-git-install.git")
	writeSkill(t, working, "git-review", "Review from Git", nil)
	commitAndPushAll(t, working, "seed Git skill")
	origin = "file://" + filepath.ToSlash(origin)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", "git-review", "--from", origin, "--ref", "main", "--scope", "user")
	if err != nil {
		t.Fatalf("Git install into v4 user config: %v: %s", err, output)
	}
	v4, err := config.LoadV4ConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Sources) != 1 {
		t.Fatalf("git sources = %#v", v4.Sources)
	}
	for name, source := range v4.Sources {
		if source.Kind != config.V4SourceGit || source.Git != origin || source.Ref != "main" {
			t.Fatalf("git source = %#v", source)
		}
		group := v4.Groups[name]
		if len(group.Include) != 1 || group.Include[0].Locator != "skill/git-review" {
			t.Fatalf("git selection = %#v", group)
		}
	}
	lock := readLockDocument(t, filepath.Join(home, ".agents", "lock.yaml"))
	if len(lock.Registries) != 1 || lock.Registries[0].Ref != "main" {
		t.Fatalf("Git acquisition lock = %#v", lock)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "git-review", "SKILL.md")); err != nil {
		t.Fatalf("Git skill was not materialized: %v", err)
	}
}

func TestInstallDirectoryBundleIntoV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	registry := t.TempDir()
	for _, name := range []string{"first-review", "second-review"} {
		writeFile(t, filepath.Join(registry, "skills", name, "SKILL.md"), "---\nname: "+name+"\ndescription: Review\n---\nReview.\n")
	}
	withWorkingDir(t, t.TempDir())

	for run := range 2 {
		output, err := executeCommand("install", "--from", registry, "--scope", "user")
		if err != nil {
			t.Fatalf("bundle install %d: %v: %s", run+1, err, output)
		}
		v4, err := config.LoadV4ConfigFromPath(userPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(v4.Sources) != 1 {
			t.Fatalf("bundle sources = %#v", v4.Sources)
		}
		for sourceName, source := range v4.Sources {
			if source.Kind != config.V4SourceDirectory || source.Directory != registry {
				t.Fatalf("bundle directory = %#v", source)
			}
			group := v4.Groups[sourceName]
			if !group.HasAll || group.All == nil || !*group.All {
				t.Fatalf("bundle was not selected with all: %#v", group)
			}
		}
	}
	for _, name := range []string{"first-review", "second-review"} {
		if _, err := os.Stat(filepath.Join(home, ".factory", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("%s was not materialized: %v", name, err)
		}
	}
}

func TestV4ConfiguredInstallSelectionModes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "all", args: []string{"install", "--all", "--kind", "skill"}, want: []string{"first-review", "second-review"}},
		{name: "all-filtered", args: []string{"install", "--all", "--kind", "skill"}, want: []string{"first-review", "second-review"}},
		{name: "tag", args: []string{"install", "--tag", "ops"}, want: []string{"first-review"}},
		{name: "collection", args: []string{"install", "review-bundle"}, want: []string{"first-review"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			registry := t.TempDir()
			writeSkill(t, registry, "first-review", "Review first", []string{"ops"})
			writeSkill(t, registry, "second-review", "Review second", []string{"dev"})
			writeCollection(t, registry, "review-bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: review-bundle
version: 1.0.0
skills:
  - name: first-review
`)
			repo := t.TempDir()
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			writeConfig(t, configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  local: {directory: `+registry+`}
`)
			withWorkingDir(t, repo)

			args := test.args
			if test.name == "all-filtered" {
				args = append(append([]string(nil), args...), "--registry", registry)
			}
			output, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("%s install into v4 config: %v: %s", test.name, err, output)
			}
			v4, err := config.LoadV4ConfigFromPath(configPath)
			if err != nil {
				t.Fatal(err)
			}
			group := v4.Groups["local"]
			switch test.name {
			case "all", "all-filtered":
				if len(group.Include) != 2 || group.Include[0].Locator != "skill/first-review" || group.Include[1].Locator != "skill/second-review" {
					t.Fatalf("all selection = %#v", group)
				}
			case "tag":
				if !group.HasTags || len(group.Tags) != 1 || group.Tags[0] != "ops" {
					t.Fatalf("tag selection = %#v", group)
				}
			case "collection":
				if len(group.Include) != 1 || group.Include[0].Locator != "collection/review-bundle" {
					t.Fatalf("collection selection = %#v", group)
				}
			}
			for _, name := range test.want {
				if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", name, "SKILL.md")); err != nil {
					t.Fatalf("%s was not materialized: %v", name, err)
				}
			}
			if test.name == "tag" {
				if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "second-review")); !os.IsNotExist(err) {
					t.Fatalf("non-matching skill was materialized: %v", err)
				}
			}
		})
	}
}

func TestRegisterMarketplaceEndpointIntoV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", server.URL))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("install", server.URL, "--scope", "user"); err != nil {
		t.Fatalf("register Marketplace in v4 user config: %v: %s", err, output)
	}
	v4, err := config.LoadV4ConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Sources) != 1 {
		t.Fatalf("v4 Marketplace source missing: %#v", v4.Sources)
	}
	for _, source := range v4.Sources {
		if source.Kind != config.V4SourceMarketplace || source.Marketplace != server.URL {
			t.Fatalf("registered Marketplace source = %#v", source)
		}
	}
	lock := readLockDocument(t, filepath.Join(home, ".agents", "lock.yaml"))
	if lock.APIVersion != lockfile.APIVersionV2 || lock.Scope != lockfile.ScopeUser || len(lock.Marketplaces) != 1 || lock.Marketplaces[0].InstanceID != "mp" || len(lock.Marketplaces[0].Resources) != 0 {
		t.Fatalf("registered Marketplace acquisition lock = %#v", lock)
	}
	if _, err := config.LoadConfigFromPath(userPath); err != nil {
		t.Fatalf("new v4 Marketplace source cannot be loaded: %v", err)
	}
}

func TestInstallMarketplaceResourceIntoV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceLockServer(t)
	defer server.Close()
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {hosted: {marketplace: "+server.URL+"}}\n")
	data, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser, Marketplaces: []lockfile.MarketplaceLock{{URL: server.URL, InstanceID: "mp"}}})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFile(t, lockPath, string(data))
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("install", "tools/skill/review", "--marketplace", "hosted", "--scope", "user", "--yes"); err != nil {
		t.Fatalf("install Marketplace resource in v4: %v: %s", err, output)
	}
	v4, err := config.LoadV4ConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Groups["hosted"].Include) != 1 || v4.Groups["hosted"].Include[0].Locator != "tools/skill/review" {
		t.Fatalf("v4 Marketplace request missing: %#v", v4.Groups["hosted"])
	}
	lock := readLockDocument(t, lockPath)
	if lock.APIVersion != lockfile.APIVersionV2 || len(lock.Marketplaces) != 1 || len(lock.Marketplaces[0].Resources) != 1 {
		t.Fatalf("Marketplace lock missing verified resource: %#v", lock)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("v4 Marketplace skill was not materialized: %v", err)
	}
	beforeConfig, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := executeCommand("install", "tools/skill/review", "--marketplace", "hosted", "--scope", "user", "--yes"); err != nil {
		t.Fatalf("repeat Marketplace install into v4: %v: %s", err, output)
	}
	if after, err := os.ReadFile(userPath); err != nil || string(after) != string(beforeConfig) {
		t.Fatalf("repeat install rewrote v4 selection: %v", err)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(beforeLock) {
		t.Fatalf("repeat install rewrote acquisition lock: %v", err)
	}
}

func TestInstallMarketplaceResourceIntoV4RepoAndLocalConfigs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		scope      string
		configName string
		lockName   string
		lockScope  lockfile.Scope
	}{
		{scope: "repo", configName: "config.yaml", lockName: "lock.yaml", lockScope: lockfile.ScopeRepo},
		{scope: "local", configName: "config.local.yaml", lockName: "lock.local.yaml", lockScope: lockfile.ScopeLocal},
	} {
		t.Run(test.scope, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			server := newV4MarketplaceLockServer(t)
			defer server.Close()
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			repo := t.TempDir()
			configPath := filepath.Join(repo, ".agents", test.configName)
			writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nsources: {hosted: {marketplace: "+server.URL+"}}\n")
			lockData, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: test.lockScope, Marketplaces: []lockfile.MarketplaceLock{{URL: server.URL, InstanceID: "mp"}}})
			if err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(repo, ".agents", test.lockName)
			writeFile(t, lockPath, string(lockData))
			withWorkingDir(t, repo)
			if output, err := executeCommand("install", "tools/skill/review", "--marketplace", "hosted", "--scope", test.scope, "--yes"); err != nil {
				t.Fatalf("Marketplace install into v4 %s scope: %v: %s", test.scope, err, output)
			}
			v4, err := config.LoadV4ConfigFromPath(configPath)
			if err != nil || len(v4.Groups["hosted"].Include) != 1 || v4.Groups["hosted"].Include[0].Locator != "tools/skill/review" {
				t.Fatalf("v4 %s request missing: %#v: %v", test.scope, v4, err)
			}
			lock := readLockDocument(t, lockPath)
			if lock.Scope != test.lockScope || len(lock.Marketplaces) != 1 || len(lock.Marketplaces[0].Resources) != 1 {
				t.Fatalf("v4 %s lock missing exact pin: %#v", test.scope, lock)
			}
			if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "review", "SKILL.md")); err != nil {
				t.Fatalf("v4 %s skill was not materialized: %v", test.scope, err)
			}
		})
	}
}

func TestPreparedMarketplacePreviewReacquiresLegacyPinIntoV4Local(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceLockServer(t)
	defer server.Close()
	oldDigest := "sha256:" + strings.Repeat("a", 64)
	pin := config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: "tools", Kind: "skill", Name: "review", Revision: oldDigest, Digest: oldDigest, Acquisition: config.MarketplaceAcquisitionMirror, Direct: true}
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Alias: "hosted", Source: config.UserPersonal}
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	if _, _, err := config.EnsureMarketplacePinsInConfigWithSnapshotAndCommit(userPath, entry.URL, entry.InstanceID, entry.Alias, []config.MarketplaceResourcePin{pin}); err != nil {
		t.Fatal(err)
	}
	if _, err := marketplace.DefaultCache().VerifiedMarketplaceLockResource(entry, pin); err == nil {
		t.Fatal("fixture already has verified cache evidence")
	}
	beforeUser, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	withWorkingDir(t, repo)
	localPath := config.LocalConfigPath(repo)
	localData := []byte("apiVersion: agent-marketplace/v4\n")
	writeFile(t, localPath, string(localData))
	opts := pipeline.InstallOptions{RepoRoot: repo, Scope: pipeline.ScopeLocal, ScopeSpecified: true, PreserveScopeRoot: true, DeferMarketplaceSync: true,
		ReacquireLegacyPin: &pipeline.LegacyMarketplacePreview{Entry: entry, Pin: pin},
		Marketplace:        entry.Alias, ExactMarketplaceLocator: &marketplace.MarketplaceLocator{Marketplace: entry.Alias, Alias: entry.Alias, URL: entry.URL, Registry: "tools", Kind: "skill", Name: "review"}, Kind: ir.KindSkill, HasKind: true}
	otherDigest := "sha256:" + strings.Repeat("b", 64)
	other := pin
	other.Name, other.Revision, other.Digest = "other", otherDigest, otherDigest
	if _, _, err := config.EnsureMarketplacePinsInConfigWithSnapshotAndCommit(userPath, entry.URL, entry.InstanceID, entry.Alias, []config.MarketplaceResourcePin{other}); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.PrepareMarketplaceInstall(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "other") {
		t.Fatalf("unrelated invalid Marketplace pin was ignored by preview: %v", err)
	}
	if err := os.WriteFile(userPath, beforeUser, 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := pipeline.PrepareMarketplaceInstall(context.Background(), opts)
	if err != nil {
		t.Fatalf("prepare verified fallback preview: %v", err)
	}
	if !strings.Contains(prepared.Preview(), "review") {
		t.Fatalf("verified fallback preview omitted resource: %q", prepared.Preview())
	}
	if after, err := os.ReadFile(localPath); err != nil || string(after) != string(localData) {
		t.Fatalf("preparation mutated local v4 config: %v", err)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.local.yaml")
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("preparation published lock without consent: %v", err)
	}
	changedUser := []byte(strings.ReplaceAll(string(beforeUser), oldDigest, otherDigest))
	if err := os.WriteFile(userPath, changedUser, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := pipeline.CommitPreparedMarketplaceInstall(context.Background(), prepared); !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("stale preview was not rejected after source pin changed: %v", err)
	}
	if after, err := os.ReadFile(localPath); err != nil || string(after) != string(localData) {
		t.Fatalf("stale preview changed target config: %v", err)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("stale preview published target lock: %v", err)
	}
	if err := os.WriteFile(userPath, beforeUser, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.CommitPreparedMarketplaceInstall(context.Background(), prepared)
	if err != nil || result == nil || !result.PendingSync {
		t.Fatalf("commit accepted fallback plan without pending-sync state: %#v: %v", result, err)
	}
	v4, err := config.LoadV4ConfigFromPath(localPath)
	if err != nil || len(v4.Sources) != 1 {
		t.Fatalf("fallback local source missing: %#v: %v", v4, err)
	}
	for name := range v4.Sources {
		if group := v4.Groups[name]; len(group.Include) != 1 || group.Include[0].Locator != "tools/skill/review" {
			t.Fatalf("fallback local selection missing: %#v", group)
		}
	}
	locked := readLockDocument(t, lockPath)
	if locked.APIVersion != lockfile.APIVersionV2 || locked.Scope != lockfile.ScopeLocal || len(locked.Marketplaces) != 1 || len(locked.Marketplaces[0].Resources) != 1 {
		t.Fatalf("accepted fallback lock missing exact resource: %#v", locked)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "review")); !os.IsNotExist(err) {
		t.Fatalf("fallback materialized before explicit sync: %v", err)
	}
	if after, err := os.ReadFile(userPath); err != nil || string(after) != string(beforeUser) {
		t.Fatalf("accepted fallback rewrote legacy source: %v", err)
	}
}

func TestV4MarketplaceInstallRollsBackConfigAndLockWhenSyncFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceLockServer(t)
	defer server.Close()
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {hosted: {marketplace: "+server.URL+"}}\n")
	lockData, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser, Marketplaces: []lockfile.MarketplaceLock{{URL: server.URL, InstanceID: "mp"}}})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFile(t, lockPath, string(lockData))
	beforeConfig, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	withWorkingDir(t, repo)
	stop := errors.New("refuse sync after v4 Marketplace publication")
	_, err = pipeline.Install(context.Background(), pipeline.InstallOptions{
		RepoRoot: repo, Scope: pipeline.ScopeUser, ScopeSpecified: true,
		Args: []string{"tools/skill/review"}, Marketplace: "hosted", AcceptRisk: true,
		DependencyReconcile: func(context.Context, string, string, bool, []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
			return pipeline.DependencyLockView{}, stop
		},
	})
	if !errors.Is(err, stop) {
		t.Fatalf("install error = %v, want sync failure", err)
	}
	if after, err := os.ReadFile(userPath); err != nil || string(after) != string(beforeConfig) {
		t.Fatalf("failed v4 install changed user config: %v", err)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockData) {
		t.Fatalf("failed v4 install changed acquisition lock: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".factory", "skills", "review")); !os.IsNotExist(err) {
		t.Fatalf("failed v4 install created a skill: %v", err)
	}
}

func TestOnlineCommandsRecoverMissingOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"sync", "update", "lock", "install-marketplace", "install-directory"} {
		for _, missingScope := range []string{"none", "user", "repo", "local", "repo-with-locked-user"} {
			if strings.HasPrefix(command, "install-") && missingScope != "none" {
				continue
			}
			t.Run(command+"/"+missingScope, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				var revision string
				server := newV4MarketplaceLockServerWithRevision(t, nil, func(value *store.AssessedResourceRevision) { revision = string(value.ContentDigest) })
				defer server.Close()
				orgPath := filepath.Join(t.TempDir(), "config.yaml")
				orgData := "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: " + server.URL + "}\nresources:\n  hosted: {include: [tools/skill/review]}\n"
				writeConfig(t, orgPath, orgData)
				config.SetTestOrganizationConfigPath(t, orgPath)
				writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
				repo := t.TempDir()
				withWorkingDir(t, repo)
				var writablePath string
				switch missingScope {
				case "user":
					writablePath = filepath.Join(home, ".agents", "config.yaml")
				case "repo", "repo-with-locked-user":
					if missingScope == "repo-with-locked-user" {
						writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), orgData+"harnesses: [factory]\n")
						if _, err := bootstrapMissingMarketplaceLocks(context.Background(), repo, repo, nil, nil); err != nil {
							t.Fatal(err)
						}
					}
					writablePath = filepath.Join(repo, ".agents", "config.yaml")
				case "local":
					writablePath = filepath.Join(repo, ".agents", "config.local.yaml")
				}
				if writablePath != "" {
					writableData := orgData
					if missingScope == "user" {
						writableData += "harnesses: [factory]\n"
					}
					writeConfig(t, writablePath, writableData)
				}
				args := []string{command}
				switch command {
				case "install-marketplace":
					args = []string{"install", server.URL + "/r/tools/skill/review/revisions/" + revision, "--yes"}
				case "install-directory":
					registry := t.TempDir()
					writeFile(t, filepath.Join(registry, "skills", "local-review", "SKILL.md"), "---\nname: local-review\ndescription: Review locally\n---\nReview locally.\n")
					args = []string{"install", "local-review", "--from", registry}
				}
				output, err := executeCommand(args...)
				if err != nil {
					t.Fatalf("%s: %v\n%s", command, err, output)
				}
				if after, err := os.ReadFile(orgPath); err != nil || string(after) != orgData {
					t.Fatalf("organization config changed: %v", err)
				}
				if _, err := os.Lstat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !os.IsNotExist(err) {
					t.Fatalf("organization lock must remain absent: %v", err)
				}
				if writablePath != "" {
					paths, err := config.ResolveConfigLockPaths(writablePath)
					if err != nil {
						t.Fatal(err)
					}
					doc := readLockDocument(t, paths.CanonicalLock)
					if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
						t.Fatalf("missing writable acquisition evidence: %#v", doc)
					}
				}
				installedName := ""
				switch command {
				case "install-marketplace":
					installedName = "review"
				case "install-directory":
					installedName = "local-review"
				}
				if installedName != "" {
					if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", installedName, "SKILL.md")); err != nil {
						t.Fatalf("installed resource missing: %v", err)
					}
				}
			})
		}
	}
}

func TestOnlineOrganizationLockRevalidatesStagedPolicyAndInstance(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	var requests atomic.Int32
	server := newV4MarketplaceLockServerWithHook(t, func() { requests.Add(1) })
	defer server.Close()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted: {include: [tools/skill/review]}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	userData := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n"
	writeConfig(t, userPath, userData)
	repo := t.TempDir()
	withWorkingDir(t, repo)
	opts := config.LoadEffectiveOptions{RepoRoot: repo, LocalConfigDir: repo}
	ctx := pipeline.WithTransientOrganizationMarketplaceLockCache(context.Background())
	if ec := pipeline.LoadEffectiveOnline(ctx, opts, nil); len(ec.Errors) > 0 {
		t.Fatal(ec.Errors)
	}
	before := requests.Load()
	for _, staged := range []string{userData + "harnesses: [claude]\n", "apiVersion: agent-marketplace/v2\nharnesses: [claude]\nallowed_marketplaces: []\n"} {
		opts.FileOverlays = map[string][]byte{userPath: []byte(staged)}
		if ec := pipeline.LoadEffectiveOnline(ctx, opts, nil); len(ec.Errors) == 0 {
			t.Fatal("invalid staged policy reused cached organization acquisition")
		}
		if requests.Load() != before {
			t.Fatal("invalid staged policy crossed the network boundary")
		}
	}
	opts.FileOverlays = map[string][]byte{userPath: []byte("apiVersion: agent-marketplace/v2\nharnesses: [claude]\n")}
	if ec := pipeline.LoadEffectiveOnline(ctx, opts, nil); len(ec.Errors) > 0 || len(ec.TargetHarnesses) != 1 || ec.TargetHarnesses[0] != "claude" {
		t.Fatalf("valid staged policy not resolved: %#v, %v", ec.TargetHarnesses, ec.Errors)
	}
	if after, err := os.ReadFile(userPath); err != nil || string(after) != userData {
		t.Fatalf("staged loading wrote user config: %v", err)
	}
	opts.FileOverlays = nil
	writeConfig(t, userPath, userData+"marketplaces:\n  - url: "+server.URL+"\n    alias: hosted\n    instance_id: different-instance\n")
	if ec := pipeline.LoadEffectiveOnline(ctx, opts, nil); len(ec.Errors) == 0 {
		t.Fatal("provisional instance hint bypassed final verified instance validation")
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("organization lock must remain absent: %v", err)
	}
}

func TestMarketplaceBootstrapRejectsOrganizationLockAppearing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	orgLockPath := filepath.Join(filepath.Dir(orgPath), "lock.yaml")
	var appeared atomic.Bool
	server := newV4MarketplaceLockServerWithHook(t, func() {
		if appeared.CompareAndSwap(false, true) {
			if err := os.WriteFile(orgLockPath, []byte("deployed concurrently"), 0o644); err != nil {
				t.Error(err)
			}
		}
	})
	defer server.Close()
	data := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: " + server.URL + "}\nresources:\n  hosted: {include: [tools/skill/review]}\n"
	writeConfig(t, orgPath, data)
	config.SetTestOrganizationConfigPath(t, orgPath)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userPath, data)
	withWorkingDir(t, t.TempDir())
	output, err := executeCommand("update")
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("organization lock appearing was not rejected: %v\n%s", err, output)
	}
	if !appeared.Load() {
		t.Fatal("did not reach Marketplace acquisition")
	}
	if after, err := os.ReadFile(userPath); err != nil || string(after) != data {
		t.Fatalf("user config changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("writable lock published after organization generation conflict: %v", err)
	}
	if after, err := os.ReadFile(orgLockPath); err != nil || string(after) != "deployed concurrently" {
		t.Fatalf("new organization lock changed: %v", err)
	}
}

func TestMixedGitMarketplaceLocksRecoverAndRetainEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	origin, working := createBareRepoWithWorkingClone(t, "mixed.git")
	writeSkill(t, working, "git-review", "Review from Git", nil)
	commitAndPushAll(t, working, "seed mixed registry")
	origin = "file://" + filepath.ToSlash(origin)
	for _, command := range []string{"sync", "update", "lock"} {
		for _, initial := range []string{"absent", "v1", "v2"} {
			t.Run(command+"/"+initial, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				server := newV4MarketplaceLockServer(t)
				defer server.Close()
				orgPath := filepath.Join(t.TempDir(), "config.yaml")
				orgData := "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: " + server.URL + "}\nresources:\n  hosted: {include: [tools/skill/review]}\n"
				writeConfig(t, orgPath, orgData)
				config.SetTestOrganizationConfigPath(t, orgPath)
				writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
				repo := t.TempDir()
				withWorkingDir(t, repo)
				configPath := filepath.Join(repo, ".agents", "config.yaml")
				lockPath := filepath.Join(repo, ".agents", "lock.yaml")
				if initial == "v1" {
					writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nsources:\n  git: {git: "+origin+", ref: main}\nresources:\n  git: {include: [skill/git-review]}\n")
					if output, err := executeCommand("lock"); err != nil {
						t.Fatalf("seed Git lock: %v\n%s", err, output)
					}
				}
				mixed := "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: " + server.URL + "}\n  git: {git: " + origin + ", ref: main}\nresources:\n  hosted: {include: [tools/skill/review]}\n  git: {include: [skill/git-review]}\n"
				writeConfig(t, configPath, mixed)
				if initial == "v2" {
					if _, err := bootstrapMissingMarketplaceLocks(context.Background(), repo, repo, nil, nil); err != nil {
						t.Fatal(err)
					}
				}
				if initial != "absent" {
					if err := os.Chmod(lockPath, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				for run := range 2 {
					if output, err := executeCommand(command); err != nil {
						t.Fatalf("%s run %d: %v\n%s", command, run+1, err, output)
					}
					doc := readLockDocument(t, lockPath)
					if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 || len(doc.Registries) != 1 {
						t.Fatalf("mixed acquisition evidence lost: %#v", doc)
					}
					if initial != "absent" {
						if info, err := os.Stat(lockPath); err != nil || info.Mode().Perm() != 0o600 {
							t.Fatalf("existing lock permissions changed: %v", err)
						}
					}
				}
				if after, err := os.ReadFile(configPath); err != nil || string(after) != mixed {
					t.Fatalf("authored config changed: %v", err)
				}
				if after, err := os.ReadFile(orgPath); err != nil || string(after) != orgData {
					t.Fatalf("organization config changed: %v", err)
				}
				if _, err := os.Lstat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !os.IsNotExist(err) {
					t.Fatalf("organization lock must remain absent: %v", err)
				}
			})
		}
	}
}

func TestMergeInstallBindingsPreservesMarketplaceEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	base := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.MarketplaceLock{{URL: "https://market.example", InstanceID: "mp"}}}
	doc, err := mergeInstallBindings(base, lockfile.ScopeRepo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != "mp" {
		t.Fatalf("Git install binding merge discarded Marketplace evidence: %#v", doc)
	}
}

func newV4MarketplaceLockServer(t *testing.T) *httptest.Server {
	return newV4MarketplaceLockServerWithHook(t, nil)
}

func newV4MarketplaceLockServerWithHook(t *testing.T, hook func()) *httptest.Server {
	return newV4MarketplaceLockServerWithRevision(t, hook, nil)
}

func newV4MarketplaceLockServerWithRevision(t *testing.T, hook func(), mutate func(*store.AssessedResourceRevision)) *httptest.Server {
	return newV4MarketplaceLockServerWithOptions(t, "mp", "review", hook, mutate)
}

func newV4MarketplaceLockServerWithIdentity(t *testing.T, instanceID, resourceName string) *httptest.Server {
	return newV4MarketplaceLockServerWithOptions(t, instanceID, resourceName, nil, nil)
}

func newV4MarketplaceLockServerWithOptions(t *testing.T, instanceID, resourceName string, hook func(), mutate func(*store.AssessedResourceRevision)) *httptest.Server {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(resourceName, "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "tools"
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(instanceID), Registry: "src", Kind: "skill", Name: resourceName},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	if mutate != nil {
		mutate(&revision)
	}
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hook != nil {
			hook()
		}
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "https://example.com"))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/"+resourceName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/"+resourceName):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
}
