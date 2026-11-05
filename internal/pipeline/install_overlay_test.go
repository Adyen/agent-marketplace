package pipeline

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func trackInstallOverlayStaging(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Cleanup(func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Error(err)
			return
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".agent-marketplace-config-") {
				t.Errorf("private config staging residue: %s", entry.Name())
			}
		}
	})
}

func TestPreparedMarketplaceOverlayBindsFutureSelectionWithoutPublishing(t *testing.T) {
	for _, included := range []bool{false, true} {
		name := "root_source"
		if included {
			name = "included_source"
		}
		t.Run(name, func(t *testing.T) {
			isolateConsumerPipeline(t)
			repo := t.TempDir()
			t.Chdir(repo)
			fake := newLayerMarketplaceServer(t)
			registry := fake.addRegistry(t, "source")
			fake.addSkill(t, registry, "review")
			path := config.ProjectConfigPath(repo)
			source := "sources:\n  public: {marketplace: " + fake.URL + "}\n"
			root := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
			fragment := filepath.Join(filepath.Dir(path), "shared.yaml")
			if included {
				root += "# Keep the authored include relative.\ninclude: shared.yaml\n"
				writeFileMode(t, fragment, source, 0o640)
			} else {
				root += source
			}
			writeFileMode(t, path, root, 0o600)
			before, info := layerFreezeFile(t, path)
			opts := InstallOptions{Args: []string{"public:source/skill/review"}, RepoRoot: repo, Scope: ScopeRepo}
			prepared, err := PrepareMarketplaceInstall(context.Background(), opts)
			if err != nil {
				t.Fatalf("prepare verified control: %v", err)
			}
			trackInstallOverlayStaging(t)
			requests := fake.requests.Load()
			overlays, err := preparedMarketplaceLockOverlay(path, config.RepoCommitted, prepared.plan.Entry, marketplaceInstallPins(prepared.plan))
			if err != nil {
				t.Fatalf("prepare future overlay: %v", err)
			}
			future := LoadEffective(context.Background(), ConfigLoadStrict, config.LoadEffectiveOptions{RepoRoot: repo, FileOverlays: overlays}, nil)
			if len(future.Errors) != 0 {
				t.Fatalf("future config and lock are not congruent: %v", future.Errors)
			}
			if len(future.Merged.Marketplaces) != 1 || len(future.Merged.Marketplaces[0].Resources) != 1 || future.Merged.Marketplaces[0].Resources[0].Name != "review" {
				t.Fatalf("future selection missing: %#v", future.Merged.Marketplaces)
			}
			if err := checkMarketplaceInstallCollision(context.Background(), opts, &future, prepared.plan, overlays); err != nil {
				t.Fatalf("collision lookup of valid future: %v", err)
			}
			if fake.requests.Load() != requests {
				t.Fatal("future overlay acquired instead of using prepared evidence")
			}
			layerAssertFileUnchanged(t, path, before, info)
			if included {
				if got, err := os.ReadFile(fragment); err != nil || string(got) != source {
					t.Fatalf("included source changed: %q, %v", got, err)
				}
				if !bytes.Contains(overlays[path], []byte("include: shared.yaml")) || bytes.Contains(overlays[path], []byte("marketplace: ")) {
					t.Fatalf("future root relocated or copied the included source:\n%s", overlays[path])
				}
			}
			lockPath, err := marketplaceLayerLockPath(path)
			if err != nil {
				t.Fatal(err)
			}
			// The matching future is a positive control for exact congruence:
			// a well-formed lock with an unreachable resource must still refuse.
			mismatched, err := lockfile.Decode(overlays[lockPath])
			if err != nil {
				t.Fatal(err)
			}
			mismatched.Marketplaces[0].Resources[0].Name = "not-requested"
			badLock, err := lockfile.Encode(mismatched)
			if err != nil {
				t.Fatal(err)
			}
			exactLock := overlays[lockPath]
			overlays[lockPath] = badLock
			err = checkMarketplaceInstallCollision(context.Background(), opts, &future, prepared.plan, overlays)
			var validation *config.V4ValidationError
			if !errors.As(err, &validation) || validation.Code != "v4.marketplace-lock-mismatch" {
				t.Fatalf("mismatched future evidence = %v", err)
			}
			if fake.requests.Load() != requests {
				t.Fatal("mismatched future evidence triggered acquisition")
			}
			overlays[lockPath] = exactLock
			if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preflight published lock: %v", err)
			}
			if _, err := os.Stat(filepath.Join(repo, ".factory")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preflight materialized output: %v", err)
			}
			if _, err := CommitPreparedMarketplaceInstall(context.Background(), prepared); err != nil {
				t.Fatalf("commit valid future: %v", err)
			}
			if got, err := os.ReadFile(filepath.Join(repo, ".factory", "skills", "review", "SKILL.md")); err != nil || !bytes.Contains(got, []byte("review body")) {
				t.Fatalf("committed artifact missing: %q, %v", got, err)
			}
		})
	}
}

func TestPreparedMarketplaceOverlayRejectsInvalidFutureWithoutAcquisition(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit string
		code string
	}{
		{name: "wildcard_suppresses_future_request", edit: "resources:\n  public: {exclude: ['source/skill/*']}\n", code: "v4.marketplace-runtime-unsupported"},
		{name: "future_has_unverified_request", edit: "resources:\n  public: {include: [source/skill/missing]}\n", code: "v4.marketplace-lock-mismatch"},
		{name: "future_has_conflicting_revision", edit: "resources:\n  public:\n    include:\n      - source/skill/review: {revision: 'sha256:" + strings.Repeat("f", 64) + "'}\n", code: "v4.duplicate-request"},
		{name: "future_has_unsupported_selector", edit: "resources:\n  public: {all: true, registries: [source]}\n", code: "v4.marketplace-runtime-unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConsumerPipeline(t)
			repo := t.TempDir()
			t.Chdir(repo)
			fake := newLayerMarketplaceServer(t)
			registry := fake.addRegistry(t, "source")
			fake.addSkill(t, registry, "review")
			path, err := config.GlobalConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			root := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  public: {marketplace: " + fake.URL + "}\n"
			writeFileMode(t, path, root, 0o600)
			prepared, err := PrepareMarketplaceInstall(context.Background(), InstallOptions{Args: []string{"public:source/skill/review"}, RepoRoot: repo, Scope: ScopeUser})
			if err != nil {
				t.Fatalf("prepare valid evidence: %v", err)
			}
			writeFileMode(t, path, root+tc.edit, 0o600)
			before, info := layerFreezeFile(t, path)
			trackInstallOverlayStaging(t)
			requests := fake.requests.Load()
			_, err = CommitPreparedMarketplaceInstall(context.Background(), prepared)
			var validation *config.V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.code {
				t.Fatalf("refusal = %v, want %s", err, tc.code)
			}
			if fake.requests.Load() != requests {
				t.Fatal("refused future configuration triggered acquisition")
			}
			layerAssertFileUnchanged(t, path, before, info)
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refusal published lock: %v", err)
			}
			for _, root := range []string{repo, filepath.Dir(filepath.Dir(path))} {
				if _, err := os.Stat(filepath.Join(root, ".factory")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("refusal materialized output under %s: %v", root, err)
				}
			}
		})
	}
}

func TestPreparedMarketplaceOverlayChecksExistingSelectorBeforeAcquisition(t *testing.T) {
	isolateConsumerPipeline(t)
	repo := t.TempDir()
	t.Chdir(repo)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	path := config.ProjectConfigPath(repo)
	root := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  public: {marketplace: " + fake.URL + "}\n"
	writeFileMode(t, path, root, 0o600)
	pins := layerPreparePins(t, fake, "skill", "review")
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, path, config.RepoCommitted, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins)
	}, nil); err != nil {
		t.Fatal(err)
	}
	// This is a valid existing selector backed by exact evidence. Its explicit
	// authored revision cannot be silently replaced by a bare include request.
	writeFileMode(t, path, root+"resources:\n  public:\n    include:\n      - source/skill/review: {revision: '"+pins[0].Revision+"'}\n", 0o600)
	prepared, err := PrepareMarketplaceInstall(context.Background(), InstallOptions{Args: []string{"public:source/skill/review"}, RepoRoot: repo, Scope: ScopeRepo})
	if err != nil {
		t.Fatalf("prepare existing exact selector: %v", err)
	}
	before, info := layerFreezeFile(t, path)
	lockPath := filepath.Join(filepath.Dir(path), "lock.yaml")
	lockBefore, lockInfo := layerFreezeFile(t, lockPath)
	trackInstallOverlayStaging(t)
	requests := fake.requests.Load()
	_, err = CommitPreparedMarketplaceInstall(context.Background(), prepared)
	var validation *config.V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.duplicate-request" {
		t.Fatalf("conflicting future request = %v", err)
	}
	if fake.requests.Load() != requests {
		t.Fatal("future edit refusal followed acquisition from an existing exact selection")
	}
	layerAssertFileUnchanged(t, path, before, info)
	layerAssertFileUnchanged(t, lockPath, lockBefore, lockInfo)
	if _, err := os.Stat(filepath.Join(repo, ".factory")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refusal materialized output: %v", err)
	}
}

func TestPreparedMarketplaceOverlayPreservesLateLocalCollision(t *testing.T) {
	isolateConsumerPipeline(t)
	repo := t.TempDir()
	t.Chdir(repo)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	path := config.ProjectConfigPath(repo)
	root := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  public: {marketplace: " + fake.URL + "}\n"
	writeFileMode(t, path, root, 0o600)
	prepared, err := PrepareMarketplaceInstall(context.Background(), InstallOptions{Args: []string{"public:source/skill/review"}, RepoRoot: repo, Scope: ScopeRepo})
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(repo, "local")
	writeFileMode(t, filepath.Join(local, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: local review\n---\nLOCAL COLLIDER\n", 0o644)
	// Put the future Marketplace request after the local selection. The future
	// winner must not conceal a pre-existing same-scope contender.
	writeFileMode(t, path, root+"  local: {directory: ./local, source_harness: claude}\nresources:\n  local: {include: [skill/review]}\n", 0o600)
	before, info := layerFreezeFile(t, path)
	trackInstallOverlayStaging(t)
	_, err = CommitPreparedMarketplaceInstall(context.Background(), prepared)
	if !errors.Is(err, ErrMarketplaceInstallCollision) {
		t.Fatalf("late local collision = %v, want marketplace.install-collision", err)
	}
	layerAssertFileUnchanged(t, path, before, info)
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("collision published lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("collision materialized output: %v", err)
	}
}

func TestPreparedMarketplaceInstallPreservesExistingSelectorsAndGitLockBytes(t *testing.T) {
	isolateConsumerPipeline(t)
	repo := t.TempDir()
	t.Chdir(repo)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "keeper")
	fake.addSkill(t, registry, "review")
	path := config.ProjectConfigPath(repo)
	writeFileMode(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
	first, err := PrepareMarketplaceInstall(context.Background(), InstallOptions{Args: []string{fake.URL + "/r/source/skill/keeper"}, RepoRoot: repo, Scope: ScopeRepo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommitPreparedMarketplaceInstall(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(path), "lock.yaml")
	doc := layerMarketplaceLockDoc(t, lockPath)
	keeper := *marketplaceLockResource(doc, "skill", "keeper")
	oid, err := lockfile.ParseGitOID("sha1:" + strings.Repeat("f", 40))
	if err != nil {
		t.Fatal(err)
	}
	doc.Registries = []lockfile.GitRegistry{{URL: "https://example.test/unrelated.git", Ref: "main", Commit: oid, Roots: []lockfile.GitRoot{{Path: ".agents", SourceHarness: "claude", RevisionSchema: "v4", RevisionDigest: lockfile.Digest("sha256:" + strings.Repeat("e", 64))}}}}
	lockBytes, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, lockPath, string(lockBytes), 0o600)
	second, err := PrepareMarketplaceInstall(context.Background(), InstallOptions{Args: []string{fake.URL + "/r/source/skill/review"}, RepoRoot: repo, Scope: ScopeRepo})
	if err != nil {
		t.Fatal(err)
	}
	before, info := layerFreezeFile(t, lockPath)
	trackInstallOverlayStaging(t)
	overlays, err := preparedMarketplaceLockOverlay(path, config.RepoCommitted, second.plan.Entry, marketplaceInstallPins(second.plan))
	if err != nil {
		t.Fatal(err)
	}
	layerAssertFileUnchanged(t, lockPath, before, info)
	if len(overlays) > 0 {
		future := LoadEffective(context.Background(), ConfigLoadStrict, config.LoadEffectiveOptions{RepoRoot: repo, FileOverlays: overlays}, nil)
		if len(future.Errors) != 0 {
			t.Fatalf("existing selector future = %v", future.Errors)
		}
	}
	if _, err := CommitPreparedMarketplaceInstall(context.Background(), second); err != nil {
		t.Fatalf("add second selector: %v", err)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	gitBefore, _, _ := strings.Cut(string(lockBytes), "\nmarketplaces:")
	gitAfter, _, _ := strings.Cut(string(after), "\nmarketplaces:")
	if gitBefore != gitAfter {
		t.Fatalf("unrelated Git lock bytes changed:\nbefore:\n%s\nafter:\n%s", gitBefore, gitAfter)
	}
	updated := layerMarketplaceLockDoc(t, lockPath)
	got := marketplaceLockResource(updated, "skill", "keeper")
	if got == nil || got.ContentDigest != keeper.ContentDigest || got.ArchiveDigest != keeper.ArchiveDigest || got.Revision != keeper.Revision || marketplaceLockResource(updated, "skill", "review") == nil {
		t.Fatalf("existing/new exact resources lost: %#v", updated.Marketplaces)
	}
}
