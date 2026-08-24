package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestOfflineSyncUsesWarmMarketplaceMirror(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	repoRoot := t.TempDir()

	resource := ir.NewSkill("review", "Review code", "Review the code carefully.", nil, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.SourceID = "src"
	canonical.Manifest.Revision = contentDigest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	cache := marketplace.DefaultCache()
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatal(err)
	}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		SourceID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest,
	}}}
	if _, err := cache.WriteResourceRevision(entry.URL, "src", "skill", "review", contentDigest, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}); err != nil {
		t.Fatal(err)
	}

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Offline:  true,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo: func() (*config.Config, error) {
				return &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil
			},
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})
	if len(result.Errors) != 0 {
		t.Fatalf("offline sync errors = %v", result.Errors)
	}
	if result.Report == nil || result.Report.TotalWritten() == 0 {
		t.Fatalf("offline sync report = %#v, want materialized output", result.Report)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("materialized skill missing: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("offline sync did not emit revocation freshness warning")
	}
	var freshnessWarning bool
	for _, warning := range result.Warnings {
		if strings.Contains(warning.Error(), "cannot observe newer revocations") {
			freshnessWarning = true
		}
	}
	if !freshnessWarning {
		t.Fatalf("offline warnings = %v, want cached revocation freshness warning", result.Warnings)
	}
}

func TestOfflineSyncUsesCollectionPreparationMetadataCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	repoRoot := t.TempDir()

	resource := ir.NewSkill("review", "Review code", "Review the code carefully.", nil, nil, ir.SourceRef{})
	artifact, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	leafDigest := artifact.Manifest.ContentDigest
	artifact.Manifest.SourceID = "src"
	artifact.Manifest.Revision = leafDigest
	artifactRaw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw, rootDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "bundle",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "review", SourceID: "src", Revision: leafDigest}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	rootRevision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "collection", Name: "bundle"},
		ContentDigest: store.RevisionDigest(rootDigest),
	}
	leafRevision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(leafDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://market.example"))
		case "/api/v1/sources/src":
			_ = json.NewEncoder(w).Encode(mpapi.SourceDTO{ID: "src", Slug: "src", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case "/api/v1/resources/src/collection/bundle":
			_ = json.NewEncoder(w).Encode(rootRevision)
		case "/api/v1/manifests/" + rootDigest:
			_, _ = w.Write(manifestRaw)
		case "/api/v1/resources/src/skill/review/revisions/" + leafDigest:
			_ = json.NewEncoder(w).Encode(leafRevision)
		case "/api/v1/artifacts/sha256/" + strings.TrimPrefix(archiveDigest, "sha256:"):
			_, _ = w.Write(artifactRaw)
		default:
			http.NotFound(w, r)
		}
	}))

	entry := config.MarketplaceEntry{URL: server.URL + "/", InstanceID: "mp"}
	plan, err := (&marketplace.Resolver{HTTPClient: server.Client()}).PrepareLocator(context.Background(), entry, marketplace.MarketplaceLocator{Source: "src", Kind: "collection", Name: "bundle"}, nil)
	if err != nil {
		server.Close()
		t.Fatalf("PrepareLocator: %v", err)
	}
	entry = plan.Entry
	entry.Resources = marketplaceInstallPins(plan)
	server.Close()

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Offline:  true,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo: func() (*config.Config, error) {
				return &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil
			},
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})
	if len(result.Errors) != 0 {
		t.Fatalf("offline collection sync errors = %v", result.Errors)
	}
	if result.Report == nil || result.Report.TotalWritten() == 0 {
		t.Fatalf("offline collection sync report = %#v, want materialized output", result.Report)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("materialized collection leaf missing: %v", err)
	}
}

func TestOfflineSyncFailsBeforeMaterializationWhenGitCacheMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	repoRoot := t.TempDir()
	output := filepath.Join(repoRoot, ".factory", "skills", "existing", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Offline:  true,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo: func() (*config.Config, error) {
				return &config.Config{Registries: []config.RegistryEntry{{
					URL: "https://registry.example/repo.git", Ref: "main", Skills: config.KindSelector{Include: []string{"existing"}},
				}}}, nil
			},
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})
	if len(result.Errors) == 0 || result.Report != nil {
		t.Fatalf("missing offline Git cache result = errors %v, report %#v; want fail before materialization", result.Errors, result.Report)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("existing generated output changed to %q", data)
	}
}

func TestOfflineSyncRejectsSymlinkedLocalRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	repoRoot := t.TempDir()
	external := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(t.TempDir(), "local-registry")
	if err := os.Symlink(external, localPath); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(repoRoot, ".factory", "skills", "existing", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Offline:  true,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo: func() (*config.Config, error) {
				return &config.Config{Registries: []config.RegistryEntry{{
					URL: localPath, Source: config.RepoCommitted,
					Skills: config.KindSelector{Include: []string{"existing"}},
				}}}, nil
			},
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})
	if len(result.Errors) == 0 || result.Report != nil {
		t.Fatalf("symlinked local registry result = errors %v, report %#v; want fail before materialization", result.Errors, result.Report)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("existing generated output changed to %q", data)
	}
}

func TestOfflineSyncFailsBeforeMaterializationForIncompleteSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	repoRoot := t.TempDir()
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "present", "Present", nil)
	writeFile(t, filepath.Join(registryRoot, "skills", "broken", "SKILL.md"), "---\nname: broken\ndescription: \"unterminated\n---\n")

	output := filepath.Join(repoRoot, ".factory", "skills", "existing", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Offline:  true,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo: func() (*config.Config, error) {
				return &config.Config{Registries: []config.RegistryEntry{{
					URL: registryRoot, Source: config.RepoCommitted,
					Skills: config.KindSelector{Include: []string{"missing", "broken"}},
				}}}, nil
			},
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})

	if len(result.Errors) != 2 {
		t.Fatalf("offline incomplete snapshot errors = %v, want missing and malformed errors", result.Errors)
	}
	var notFound *resolver.SelectedNotFoundError
	var malformed *resolver.SelectedMalformedError
	if !errors.As(result.Errors[0], &notFound) && !errors.As(result.Errors[1], &notFound) {
		t.Fatalf("offline errors = %v, want SelectedNotFoundError", result.Errors)
	}
	if !errors.As(result.Errors[0], &malformed) && !errors.As(result.Errors[1], &malformed) {
		t.Fatalf("offline errors = %v, want SelectedMalformedError", result.Errors)
	}
	if result.Report != nil {
		t.Fatalf("offline incomplete snapshot report = %#v, want no materialization", result.Report)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("existing generated output changed to %q", data)
	}
	for _, warning := range result.Warnings {
		if resolver.IsSoftSelectedNotFound(warning) || resolver.IsSoftSelectedMalformed(warning) {
			t.Fatalf("offline unresolved selection remained a warning: %v", warning)
		}
	}
}

func TestOfflineSyncMarketplaceCacheFailurePreservesOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	repoRoot := t.TempDir()
	output := filepath.Join(repoRoot, ".factory", "skills", "existing", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		SourceID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: digest,
	}}}
	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Offline:  true,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo: func() (*config.Config, error) {
				return &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil
			},
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})
	if len(result.Errors) == 0 || result.Report != nil {
		t.Fatalf("missing Marketplace cache result = errors %v, report %#v; want fail before materialization", result.Errors, result.Report)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("existing generated output changed to %q", data)
	}
}

func TestOfflineSyncPreservesExactMarketplaceSelectorsOnFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	digest := "sha256:" + strings.Repeat("a", 64)
	original := []byte("apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: https://market.example\n    instance_id: mp\n    resources:\n      - source_id: collections\n        kind: collection\n        name: bundle\n        revision: " + digest + "\n        digest: " + digest + "\n      - source_id: source\n        kind: skill\n        name: review\n        revision: " + digest + "\n        digest: " + digest + "\n        direct: true\n        selected_by:\n          - source_id: collections\n            name: bundle\n            revision: " + digest + "\n")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	result := Sync(context.Background(), ResolveOptions{
		RepoRoot: repoRoot,
		NoBackup: true,
		Offline:  true,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo:  func() (*config.Config, error) { return config.LoadConfigFromPath(configPath) },
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})
	if len(result.Errors) == 0 {
		t.Fatal("offline sync unexpectedly succeeded without the exact Marketplace cache")
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("offline sync changed exact selectors:\nbefore:\n%s\nafter:\n%s", original, got)
	}
}

func TestOfflineOverridesExplicitMarketplaceRefresh(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unexpected offline request", http.StatusInternalServerError)
	}))
	defer server.Close()

	explicitMode := resolver.MarketplaceResolveModeRefresh
	result := Resolve(context.Background(), ResolveOptions{
		RepoRoot:        t.TempDir(),
		Offline:         true,
		MarketplaceMode: &explicitMode,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo: func() (*config.Config, error) {
				return &config.Config{Marketplaces: []config.MarketplaceEntry{{
					URL: server.URL, InstanceID: "mp",
					Resources: []config.MarketplaceResourcePin{{
						SourceID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: "sha256:" + strings.Repeat("a", 64),
					}},
				}}}, nil
			},
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})
	if requests != 0 {
		t.Fatalf("offline resolve made %d Marketplace requests despite explicit refresh mode", requests)
	}
	if len(result.Errors) == 0 {
		t.Fatal("offline resolve unexpectedly succeeded without cached Marketplace metadata")
	}
}
