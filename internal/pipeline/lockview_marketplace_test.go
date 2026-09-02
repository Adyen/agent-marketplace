package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

const (
	lockViewRevision = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	lockViewArchive  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestExactLockViewMarketplaceLayerStates(t *testing.T) {
	tests := []struct {
		name       string
		writeLock  bool
		configured string
		lockedName string
		lockedRev  string
		wantCode   string
	}{
		{name: "missing", configured: "review", lockedName: "review", lockedRev: lockViewRevision, wantCode: "lock.missing"},
		{name: "stale requirement", writeLock: true, configured: "deploy", lockedName: "review", lockedRev: lockViewRevision, wantCode: "lock.stale"},
		{name: "moved exact revision", writeLock: true, configured: "review", lockedName: "review", lockedRev: lockViewRevision, wantCode: "lock.stale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{{SourceID: "src", Kind: "skill", Name: tt.configured}}}
			if tt.name == "moved exact revision" {
				entry.Resources[0].Revision = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			}
			if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if tt.writeLock {
				writeMarketplaceLockViewFixture(t, filepath.Join(dir, "lock.yaml"), tt.lockedName, tt.lockedRev)
			}
			ec := config.EffectiveConfig{Repo: config.LayerResult{Source: config.RepoCommitted, Path: configPath, Config: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}}
			view := exactLockView(ec)
			if len(view.errors) != 1 || len(view.marketplaceMetadata) != 1 || !view.marketplaceMetadata[0].Blocked {
				t.Fatalf("view = errors:%v metadata:%#v", view.errors, view.marketplaceMetadata)
			}
			var viewErr *LockViewError
			if !errors.As(view.errors[0], &viewErr) || viewErr.Code != tt.wantCode || view.marketplaceMetadata[0].Code != tt.wantCode {
				t.Fatalf("lock error = %v metadata=%#v, want %s", view.errors[0], view.marketplaceMetadata[0], tt.wantCode)
			}
			if got := view.marketplaceView(&config.Config{Marketplaces: []config.MarketplaceEntry{entry}}); len(got.Marketplaces) != 0 {
				t.Fatalf("blocked Marketplace remained resolvable: %#v", got.Marketplaces)
			}
		})
	}
}

func TestExactLockViewRejectsIncompleteMarketplaceAcquisitionAuthority(t *testing.T) {
	tests := []struct {
		name        string
		acquisition string
	}{
		{name: "mirror without archive", acquisition: "mirror"},
		{name: "index-only without source occurrence", acquisition: "index-only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			lock := "apiVersion: agent-marketplace-lock/v1\nscope: repo\nmarketplaces:\n  - url: https://market.example\n    instance_id: mp\n    resources:\n      - source_id: src\n        kind: skill\n        name: review\n        revision: " + lockViewRevision + "\n        acquisition: " + tt.acquisition + "\n        direct: true\n"
			if err := os.WriteFile(filepath.Join(dir, "lock.yaml"), []byte(lock), 0o644); err != nil {
				t.Fatal(err)
			}
			entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{{SourceID: "src", Kind: "skill", Name: "review"}}}
			view := exactLockView(config.EffectiveConfig{Repo: config.LayerResult{Source: config.RepoCommitted, Path: configPath, Config: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}})
			var viewErr *LockViewError
			if len(view.errors) != 1 || !errors.As(view.errors[0], &viewErr) || viewErr.Code != "lock.invalid" {
				t.Fatalf("incomplete %s authority accepted: %v", tt.acquisition, view.errors)
			}
		})
	}
}

func TestExactLockViewBuildsEphemeralMarketplacePins(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMarketplaceLockViewFixture(t, filepath.Join(dir, "lock.yaml"), "review", lockViewRevision)
	entry := config.MarketplaceEntry{URL: "https://market.example/", InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{{SourceID: "src", Kind: "skill", Name: "review", InstalledName: "team-review"}}}
	ec := config.EffectiveConfig{Repo: config.LayerResult{Source: config.RepoCommitted, Path: configPath, Config: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}}
	view := exactLockView(ec)
	if len(view.errors) != 0 || len(view.marketplaceMetadata) != 1 || view.marketplaceMetadata[0].Blocked {
		t.Fatalf("view = errors:%v metadata:%#v", view.errors, view.marketplaceMetadata)
	}
	got := view.marketplaceView(&config.Config{Marketplaces: []config.MarketplaceEntry{entry}})
	pin := got.Marketplaces[0].Resources[0]
	if pin.Revision != lockViewRevision || pin.Digest != lockViewRevision || pin.ArchiveDigest != lockViewArchive || pin.InstalledName != "team-review" || pin.Acquisition != config.MarketplaceAcquisitionMirror {
		t.Fatalf("exact pin = %#v", pin)
	}
	if entry.Resources[0].Revision != "" || entry.Resources[0].ArchiveDigest != "" {
		t.Fatalf("configured requirement mutated: %#v", entry.Resources[0])
	}
}

func TestExactLockViewReadDoesNotCreateExactGitLocks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Registries: []lockfile.GitRegistry{{URL: "https://git.example/repo.git", Ref: "main", Commit: mustLockViewOID(t), Roots: []lockfile.GitRoot{{Path: ".", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: lockfile.Digest(lockViewRevision)}}}}}
	writeLockViewDocument(t, filepath.Join(dir, "lock.yaml"), doc)
	entry := config.RegistryEntry{URL: "https://git.example/repo.git", Ref: "main", Source: config.RepoCommitted}
	exactLockView(config.EffectiveConfig{Repo: config.LayerResult{Source: config.RepoCommitted, Path: configPath, Config: &config.Config{Registries: []config.RegistryEntry{entry}}}})
	if _, err := os.Stat(filepath.Join(home, "cache", "agent-marketplace", "exact-git", "locks")); !os.IsNotExist(err) {
		t.Fatalf("read-only inspection created exact-git lock namespace: %v", err)
	}
}

func writeMarketplaceLockViewFixture(t *testing.T, path, name, revision string) {
	t.Helper()
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.Marketplace{{URL: "https://market.example", InstanceID: "mp", Resources: []lockfile.MarketplaceResource{{SourceID: "src", Kind: "skill", Name: name, Revision: lockfile.Digest(revision), Acquisition: lockfile.AcquisitionMirror, ArchiveDigest: lockfile.Digest(lockViewArchive), Direct: true}}}}}
	writeLockViewDocument(t, path, doc)
}

func writeLockViewDocument(t *testing.T, path string, doc lockfile.Document) {
	t.Helper()
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustLockViewOID(t *testing.T) lockfile.GitOID {
	t.Helper()
	oid, err := lockfile.ParseGitOID("sha1:" + strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	return oid
}
