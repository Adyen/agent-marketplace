package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// TestInspectResolvePreservesInstalledMarketplaceArtifact covers the public
// result seam behind `agent-marketplace status`: an inspect-mode Resolve must
// hand generic resolution and materialization dry-run the same resolved
// Marketplace resource that `sync --check` produces, so a clean installed pin
// is not reported as orphaned output or removal drift.
func TestInspectResolvePreservesInstalledMarketplaceArtifact(t *testing.T) {
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
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = contentDigest
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	revision := testAssessedResourceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}, store.AssessmentDecisionAllow, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	cache := marketplace.DefaultCache()
	if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("write revision metadata: %v", err)
	}
	if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("mark artifact verified: %v", err)
	}
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest,
	}}}

	result := Resolve(context.Background(), ResolveOptions{
		RepoRoot:    repoRoot,
		CatalogMode: catalog.LoadModeInspect,
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
		t.Fatalf("inspect resolve errors = %v", result.Errors)
	}
	if result.MarketplaceInspection == nil || len(result.MarketplaceStatuses) != 1 {
		t.Fatalf("inspection = %#v, want one pin status", result.MarketplaceInspection)
	}
	resolvedCount := 0
	for _, artifact := range result.Resolved {
		if !artifact.IsMarketplace {
			continue
		}
		resolvedCount++
		if artifact.Name != "review" || artifact.Kind != ir.KindSkill || artifact.Resource == nil || artifact.Unresolved {
			t.Fatalf("resolved Marketplace artifact = %#v, want materializable review skill", artifact)
		}
	}
	if resolvedCount != 1 {
		t.Fatalf("resolved artifacts = %#v, want the installed Marketplace pin", result.Resolved)
	}
}

// TestInspectResolvePromotesUnhealthyRequiredPin keeps the required-pin
// diagnostic at the same seam: an org-owned pin that cannot be verified must
// remain a hard pipeline error, never a silently dropped artifact.
func TestInspectResolvePromotesUnhealthyRequiredPin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	repoRoot := t.TempDir()

	contentDigest := "sha256:" + strings.Repeat("a", 64)
	archiveDigest := "sha256:" + strings.Repeat("b", 64)
	revision := testAssessedResourceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
	}, store.AssessmentDecisionAllow, nil)
	revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: "src", Reason: "test", AppliedAt: time.Unix(1, 0).UTC()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{{
		RegistryID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest,
	}}}

	result := Resolve(context.Background(), ResolveOptions{
		RepoRoot:    repoRoot,
		CatalogMode: catalog.LoadModeInspect,
		Loaders: Loaders{
			Org: func() (*config.Config, error) {
				return &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil
			},
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo:  func() (*config.Config, error) { return &config.Config{}, nil },
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	})
	var pinErr *marketplace.PinStatusError
	found := false
	for _, err := range result.Errors {
		if errors.As(err, &pinErr) {
			found = true
		}
	}
	if !found {
		t.Fatalf("inspect resolve errors = %v, want a required PinStatusError", result.Errors)
	}
	for _, artifact := range result.Resolved {
		if artifact.IsMarketplace {
			t.Fatalf("unhealthy required pin materialized an artifact: %#v", artifact)
		}
	}
}
