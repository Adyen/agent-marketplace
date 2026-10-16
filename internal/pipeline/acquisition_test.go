package pipeline

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
)

func deployedLockFixture(t *testing.T, configData, lockData string) (configPath, lockPath string) {
	t.Helper()
	dir := t.TempDir()
	configPath = filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configData), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath = filepath.Join(dir, "lock.yaml")
	if lockData != "" {
		if err := os.WriteFile(lockPath, []byte(lockData), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return configPath, lockPath
}

func readDeployedLocksIntent(t *testing.T, opts config.LoadEffectiveOptions) config.Intent {
	t.Helper()
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	if opts.RepoRoot == "" {
		opts.RepoRoot = root
	}
	if opts.LocalConfigDir == "" {
		opts.LocalConfigDir = root
	}
	if opts.OrganizationPath == "" {
		opts.OrganizationPath = missing
	}
	if opts.GlobalPath == "" {
		opts.GlobalPath = missing
	}
	if opts.RepoPath == "" {
		opts.RepoPath = missing
	}
	if opts.LocalPath == "" {
		opts.LocalPath = missing
	}
	intent := config.ReadIntent(context.Background(), opts)
	if len(intent.Errors) > 0 {
		t.Fatalf("intent errors = %v", intent.Errors)
	}
	return intent
}

// spec: deployed-acquisition-locks/absent-organization-lock
func TestReadDeployedLocksAbsentOrganizationIsNil(t *testing.T) {
	orgPath, _ := deployedLockFixture(t, "apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\nresources: {m: [src/skill/review]}\n", "")
	intent := readDeployedLocksIntent(t, config.LoadEffectiveOptions{OrganizationPath: orgPath})
	if !intent.Layers[config.Organization].NeedsMarketplaceLock() {
		t.Fatal("fixture organization layer does not need a Marketplace lock")
	}

	evidence, err := ReadDeployedLocks(context.Background(), intent)
	if err != nil {
		t.Fatalf("ReadDeployedLocks: %v", err)
	}
	if evidence.Locks[config.Organization] != nil {
		t.Fatalf("organization lock = %#v, want nil for an absent lock", evidence.Locks[config.Organization])
	}
}

func TestLoadEffectiveOnlineAllowsMissingWritableGitLock(t *testing.T) {
	repoPath, _ := deployedLockFixture(t, "apiVersion: agent-marketplace/v4\nsources:\n  registry: {git: https://example.test/a.git, ref: main}\n", "")
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	opts := config.LoadEffectiveOptions{
		RepoRoot:         root,
		LocalConfigDir:   root,
		OrganizationPath: missing,
		GlobalPath:       missing,
		RepoPath:         repoPath,
		LocalPath:        missing,
	}

	online := LoadEffective(context.Background(), ConfigLoadOnline, opts, nil)
	if len(online.Errors) > 0 {
		t.Fatalf("ConfigLoadOnline errors = %v, want a bound repo config with no hard errors", online.Errors)
	}
	if online.Repo.Config == nil || len(online.Repo.Config.Registries) != 1 {
		t.Fatalf("repo config = %#v, want the bound v4 registry", online.Repo.Config)
	}

	strict := LoadEffective(context.Background(), ConfigLoadStrict, opts, nil)
	found := false
	for _, err := range strict.Errors {
		var lockErr *LockViewError
		if errors.As(err, &lockErr) && lockErr.Code == "lock.missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ConfigLoadStrict errors = %v, want LockViewError lock.missing", strict.Errors)
	}
}

func TestLoadEffectiveUsesOverlayForMissingWritableConfig(t *testing.T) {
	repo := t.TempDir()
	localDir := filepath.Join(repo, ".agents")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(localDir, "config.local.yaml")
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	opts := config.LoadEffectiveOptions{
		RepoRoot:         repo,
		LocalConfigDir:   localDir,
		OrganizationPath: missing,
		GlobalPath:       missing,
		RepoPath:         missing,
		LocalPath:        localPath,
		FileOverlays: map[string][]byte{
			localPath: []byte("apiVersion: agent-marketplace/v4\nsources:\n  registry: {git: https://example.test/a.git, ref: main}\n"),
		},
	}
	online := LoadEffective(context.Background(), ConfigLoadOnline, opts, nil)
	if len(online.Errors) > 0 {
		t.Fatalf("ConfigLoadOnline errors = %v, want a bound local config with no hard errors", online.Errors)
	}
	if online.Local == nil || len(online.Local.Registries) != 1 {
		t.Fatalf("local config = %#v, want the bound v4 registry", online.Local)
	}

	strict := LoadEffective(context.Background(), ConfigLoadStrict, opts, nil)
	found := false
	for _, err := range strict.Errors {
		var lockErr *LockViewError
		if errors.As(err, &lockErr) && lockErr.Code == "lock.missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ConfigLoadStrict errors = %v, want LockViewError lock.missing", strict.Errors)
	}
}

func TestLoadEffectiveSkipsLockPathForLayerWithoutRemoteSources(t *testing.T) {
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	if err := os.WriteFile(orgPath, []byte("apiVersion: agent-marketplace/v4\nharnesses:\n  - claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(t.TempDir(), "global.yaml")
	if err := os.WriteFile(globalPath, []byte("apiVersion: agent-marketplace/v4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	opts := config.LoadEffectiveOptions{
		RepoRoot:         root,
		LocalConfigDir:   root,
		OrganizationPath: orgPath,
		GlobalPath:       globalPath,
		RepoPath:         missing,
		LocalPath:        missing,
	}

	ec := LoadEffective(context.Background(), ConfigLoadOnline, opts, nil)
	if len(ec.Errors) > 0 {
		t.Fatalf("ConfigLoadOnline errors = %v, want success without lock path resolution", ec.Errors)
	}
	if ec.Organization.Config == nil || len(ec.Organization.Config.Harnesses) != 1 || ec.Organization.Config.Harnesses[0] != "claude" {
		t.Fatalf("organization config = %#v, want retained harnesses", ec.Organization.Config)
	}
	if ec.User.Config == nil {
		t.Fatalf("user config = %#v, want retained settings", ec.User.Config)
	}
}

// spec: deployed-acquisition-locks/missing-writable-acquisition-lock
func TestReadDeployedLocksMissingWritableGitLockFails(t *testing.T) {
	repoPath, _ := deployedLockFixture(t, "apiVersion: agent-marketplace/v4\nsources:\n  registry: {git: https://example.test/a.git, ref: main}\n", "")
	intent := readDeployedLocksIntent(t, config.LoadEffectiveOptions{RepoPath: repoPath})
	if !intent.Layers[config.RepoCommitted].NeedsLock() {
		t.Fatal("fixture repo layer does not need a lock")
	}

	_, err := ReadDeployedLocks(context.Background(), intent)
	var lockErr *LockViewError
	if !errors.As(err, &lockErr) || lockErr.Code != "lock.missing" {
		t.Fatalf("ReadDeployedLocks error = %v, want lock.missing for a writable layer", err)
	}
}

// spec: deployed-acquisition-locks/stale-organization-lock
func TestReadDeployedLocksStaleOrganizationGitLockFails(t *testing.T) {
	orgPath, _ := deployedLockFixture(t,
		"apiVersion: agent-marketplace/v4\nsources: {s: {git: https://example.test/a.git, ref: main}}\n",
		"apiVersion: agent-marketplace-lock/v1\nscope: organization\n")
	intent := readDeployedLocksIntent(t, config.LoadEffectiveOptions{OrganizationPath: orgPath})

	_, err := ReadDeployedLocks(context.Background(), intent)
	var lockErr *LockViewError
	if !errors.As(err, &lockErr) || lockErr.Code != "lock.stale" {
		t.Fatalf("ReadDeployedLocks error = %v, want lock.stale for a present stale organization lock", err)
	}

	missing := filepath.Join(t.TempDir(), "missing.yaml")
	root := t.TempDir()
	online := LoadEffective(context.Background(), ConfigLoadOnline, config.LoadEffectiveOptions{
		RepoRoot:         root,
		LocalConfigDir:   root,
		OrganizationPath: orgPath,
		GlobalPath:       missing,
		RepoPath:         missing,
		LocalPath:        missing,
	}, nil)
	found := false
	for _, err := range online.Errors {
		var lockErr *LockViewError
		if errors.As(err, &lockErr) && lockErr.Code == "lock.stale" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ConfigLoadOnline errors = %v, want lock.stale for a present stale organization lock", online.Errors)
	}
}

// spec: deployed-acquisition-locks/malformed-organization-lock
func TestReadDeployedLocksMalformedOrganizationFails(t *testing.T) {
	orgPath, _ := deployedLockFixture(t, "apiVersion: agent-marketplace/v4\nsources:\n  registry: {git: https://example.test/a.git, ref: main}\n", "not: [valid\n")
	intent := readDeployedLocksIntent(t, config.LoadEffectiveOptions{OrganizationPath: orgPath})

	_, err := ReadDeployedLocks(context.Background(), intent)
	var trustErr *config.OrganizationTrustError
	if !errors.As(err, &trustErr) || !strings.Contains(trustErr.Code, "malformed") {
		t.Fatalf("ReadDeployedLocks error = %v, want organization trust malformed error", err)
	}
}

// spec: deployed-acquisition-locks/missing-lock-preserves-independent-evidence
func TestLoadEffectiveUsesStagedMarketplaceLockOverlay(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	fixture := newMarketplaceUpdateFixture(t, false)
	userPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	userConfig := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: " + fixture.server.URL + "}\nresources:\n  hosted: {include: [renamed-source/skill/review]}\n")
	if err := os.WriteFile(userPath, userConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	opts := config.LoadEffectiveOptions{
		RepoRoot:         repo,
		LocalConfigDir:   repo,
		OrganizationPath: missing,
		GlobalPath:       userPath,
		RepoPath:         filepath.Join(repo, ".agents", "config.yaml"),
		LocalPath:        filepath.Join(repo, ".agents", "config.local.yaml"),
	}
	intent := config.ReadIntent(context.Background(), opts)
	if len(intent.Errors) > 0 {
		t.Fatalf("read intent errors = %v", intent.Errors)
	}
	doc, err := marketplacelock.Resolve(context.Background(), marketplace.NewResolver(), intent.Layers[config.UserPersonal], []string{"factory"}, nil)
	if err != nil {
		t.Fatalf("resolve user Marketplace lock: %v", err)
	}
	lockData, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(userPath), "lock.yaml")
	opts.FileOverlays = map[string][]byte{lockPath: lockData}

	loaded := LoadEffective(context.Background(), ConfigLoadStrict, opts, nil)
	if len(loaded.Errors) > 0 {
		t.Fatalf("staged-lock load errors = %v", loaded.Errors)
	}
	if loaded.User.Config == nil || len(loaded.User.Config.Marketplaces) != 1 {
		t.Fatalf("loaded user Marketplace config = %#v", loaded.User.Config)
	}
	found := false
	for _, read := range loaded.User.Reads {
		if read.RequestedPath == lockPath && read.Overlay && read.AcquisitionLock {
			found = true
		}
	}
	if !found {
		t.Fatalf("user layer reads do not include staged acquisition lock %q: %#v", lockPath, loaded.User.Reads)
	}
	if err := os.MkdirAll(filepath.Dir(opts.RepoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.RepoPath, []byte("apiVersion: agent-marketplace/v4\nsources: {git: {git: https://example.test/git, ref: main}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded = LoadEffective(context.Background(), ConfigLoadStrict, opts, nil)
	var missingLock *LockViewError
	if !errors.As(errors.Join(loaded.Errors...), &missingLock) || missingLock.Code != "lock.missing" {
		t.Fatalf("missing repository lock errors = %v, want lock.missing", loaded.Errors)
	}
	if loaded.User.Config == nil || len(loaded.User.Config.Marketplaces) != 1 {
		t.Fatalf("missing repository lock discarded verified user Marketplace evidence: %#v", loaded.User.Config)
	}
}

func TestReadLayerLockPreservesManagedAndLegacySymlinkPolicy(t *testing.T) {
	repo := t.TempDir()
	agents := filepath.Join(repo, ".agents")
	lockDir := filepath.Join(repo, "locks")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo})
	if err != nil {
		t.Fatal(err)
	}
	containedLock := filepath.Join(lockDir, "lock.yaml")
	if err := os.WriteFile(containedLock, data, 0o600); err != nil {
		t.Fatal(err)
	}
	managedLock := filepath.Join(agents, "lock.yaml")
	if err := os.Symlink(filepath.Join("..", "locks", "lock.yaml"), managedLock); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	doc, generation, exists, err := ReadLayerLock(managedLock, lockfile.ScopeRepo)
	if err != nil || !exists || doc == nil || doc.Scope != lockfile.ScopeRepo {
		t.Fatalf("contained managed lock = %#v, %#v, %t, %v", doc, generation, exists, err)
	}
	if generation.Digest != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("generation digest = %q, want SHA-256 of lock bytes", generation.Digest)
	}

	externalDir := t.TempDir()
	externalLock := filepath.Join(externalDir, "lock.yaml")
	if err := os.WriteFile(externalLock, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(managedLock); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalLock, managedLock); err != nil {
		t.Fatal(err)
	}
	doc, _, exists, err = ReadLayerLock(managedLock, lockfile.ScopeRepo)
	if err == nil || !errors.Is(err, dependencytxn.ErrUnsafePath) || doc != nil || exists {
		t.Fatalf("escaping managed lock = %#v, %t, %v; want unsafe path without evidence", doc, exists, err)
	}
	unchanged, err := os.ReadFile(externalLock)
	if err != nil || string(unchanged) != string(data) {
		t.Fatalf("external lock changed: bytes=%q err=%v", unchanged, err)
	}

	legacyLink := filepath.Join(repo, "legacy-lock.yaml")
	if err := os.Symlink(externalLock, legacyLink); err != nil {
		t.Fatal(err)
	}
	doc, _, exists, err = ReadLayerLock(legacyLink, lockfile.ScopeRepo)
	if err == nil || !errors.Is(err, lockfile.ErrUnsafeFile) || doc != nil || exists {
		t.Fatalf("legacy symlink lock = %#v, %t, %v; want lockfile.ErrUnsafeFile without evidence", doc, exists, err)
	}
}
