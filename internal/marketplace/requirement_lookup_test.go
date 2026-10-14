package marketplace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const (
	lookupCommitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	lookupCommitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	lookupHelperD = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

func lookupOccurrence(commit, manifestDigest string) mpapi.ExactRegistryOccurrence {
	return mpapi.ExactRegistryOccurrence{
		RegistryID:                     "src",
		Ref:                            "main",
		Roots:                          []store.RegistryRoot{{Path: "tools"}},
		Commit:                         commit,
		RegistryRevisionManifestDigest: store.RevisionDigest(manifestDigest),
		Acquisition:                    store.AcquisitionIndexOnly,
		AcquisitionOrigin:              "https://example.com/org/repo.git",
		OccurredAt:                     time.Unix(1, 0).UTC(),
	}
}

func lookupManifest(t *testing.T, resources ...marketplaceartifact.RevisionResource) ([]byte, string) {
	t.Helper()
	payload, digest, err := marketplaceartifact.EncodeRegistryRevisionManifest(marketplaceartifact.RegistryRevisionManifest{
		SchemaVersion: marketplaceartifact.RegistryRevisionSchemaV1,
		Generator:     marketplaceartifact.RegistryRevisionGeneratorV1,
		Resources:     resources,
	})
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	return payload, digest
}

func lookupEntry(server *httptest.Server) config.MarketplaceEntry {
	return config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}
}

// spec: resource-prerequisites/registry-commit-lookup-does-not-fall-back-to-the-current-revision
func TestLookupRequirementPinnedCommitNoCurrentFallback(t *testing.T) {
	manifest, digest := lookupManifest(t,
		marketplaceartifact.RevisionResource{Kind: "skill", Name: "other", ContentDigest: lookupHelperD},
	)
	var resourceRouteHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/registries/src/occurrences/"+lookupCommitA:
			_ = json.NewEncoder(w).Encode(lookupOccurrence(lookupCommitA, digest))
		case r.URL.Path == "/api/v1/manifests/"+digest:
			_, _ = w.Write(manifest)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/"):
			resourceRouteHit = true
			// Serve a current revision that would wrongly satisfy the lookup.
			revision := assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "helper"},
				ContentDigest: store.RevisionDigest(lookupHelperD),
			})
			_ = json.NewEncoder(w).Encode(revision)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := &Resolver{Cache: NewCache(t.TempDir())}
	result, err := r.LookupMarketplaceRequirement(t.Context(), resolver.MarketplaceRequirementQuery{
		Entry:          lookupEntry(server),
		RegistryID:     "src",
		SameRegistry:   true,
		RegistryCommit: lookupCommitA,
		Kind:           "skill",
		Name:           "helper",
	})
	if err != nil {
		t.Fatalf("lookup = %v", err)
	}
	if result.Found {
		t.Fatalf("lookup must not fall back to the current revision, got %+v", result)
	}
	if resourceRouteHit {
		t.Fatal("pinned-commit lookup touched the current resource route")
	}
}

// spec: resource-prerequisites/unestablished-source-root-leaves-a-path-hint-unresolved
func TestLookupRequirementUnestablishedSourceRoot(t *testing.T) {
	manifest, digest := lookupManifest(t,
		marketplaceartifact.RevisionResource{Kind: "skill", Name: "helper", ContentDigest: lookupHelperD},
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/registries/src/occurrences/" + lookupCommitA:
			_ = json.NewEncoder(w).Encode(lookupOccurrence(lookupCommitA, digest))
		case "/api/v1/manifests/" + digest:
			_, _ = w.Write(manifest)
		case "/api/v1/resources/src/skill/helper/revisions/" + lookupHelperD:
			// The served occurrence belongs to a different commit, so the
			// candidate's source root at the pinned commit cannot be derived.
			revision := assessedRevisionOccurrence(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "helper"},
				ContentDigest: store.RevisionDigest(lookupHelperD),
			}, lookupCommitB, digest)
			revision.Occurrence.SourcePath = "tools/helper"
			_ = json.NewEncoder(w).Encode(revision)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := &Resolver{Cache: NewCache(t.TempDir())}
	result, err := r.LookupMarketplaceRequirement(t.Context(), resolver.MarketplaceRequirementQuery{
		Entry:          lookupEntry(server),
		RegistryID:     "src",
		SameRegistry:   true,
		RegistryCommit: lookupCommitA,
		Kind:           "skill",
		Name:           "helper",
		Path:           "tools",
	})
	if err != nil {
		t.Fatalf("lookup = %v", err)
	}
	if result.Found {
		t.Fatalf("path hint with unestablished source root must stay unresolved, got %+v", result)
	}
}

// spec: resource-prerequisites/unhinted-marketplace-prerequisite-uses-the-requiring-registry
func TestLookupRequirementLatestRevision(t *testing.T) {
	revision := assessedRevisionOccurrence(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "helper"},
		ContentDigest: store.RevisionDigest(lookupHelperD),
	}, lookupCommitA, "sha256:"+strings.Repeat("e", 64))
	var historyHit, occurrenceHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/resources/src/skill/helper":
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/registries/src/history"):
			historyHit = true
			http.Error(w, "not found", http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/api/v1/registries/src/occurrences/"):
			occurrenceHit = true
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := &Resolver{Cache: NewCache(t.TempDir())}
	result, err := r.LookupMarketplaceRequirement(t.Context(), resolver.MarketplaceRequirementQuery{
		Entry:        lookupEntry(server),
		RegistryID:   "src",
		RegistrySlug: "src",
		SameRegistry: true,
		Kind:         "skill",
		Name:         "helper",
	})
	if err != nil {
		t.Fatalf("lookup = %v", err)
	}
	if !result.Found || result.ContentDigest != lookupHelperD {
		t.Fatalf("latest-revision lookup = %+v, want found %s", result, lookupHelperD)
	}
	if historyHit {
		t.Fatal("lookup used the registry history transport instead of the latest resource revision")
	}
	if occurrenceHit {
		t.Fatal("lookup without a path hint fetched the registry occurrence")
	}
}

// A cross-registry lookup carrying the requirer's pinned commit must ignore
// it and use the target registry's current revision.
func TestLookupRequirementCrossRegistryIgnoresRequirerCommit(t *testing.T) {
	revision := assessedRevisionOccurrence(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "other", Kind: "skill", Name: "helper"},
		ContentDigest: store.RevisionDigest(lookupHelperD),
	}, lookupCommitB, "sha256:"+strings.Repeat("e", 64))
	var pinnedRouteHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/resources/extras/skill/helper":
			_ = json.NewEncoder(w).Encode(revision)
		case "/api/v1/registries/other/occurrences/" + lookupCommitA:
			pinnedRouteHit = true
			_ = json.NewEncoder(w).Encode(lookupOccurrence(lookupCommitA, "sha256:"+strings.Repeat("e", 64)))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := &Resolver{Cache: NewCache(t.TempDir())}
	result, err := r.LookupMarketplaceRequirement(t.Context(), resolver.MarketplaceRequirementQuery{
		Entry:          lookupEntry(server),
		RegistryID:     "other",
		RegistrySlug:   "extras",
		SameRegistry:   false,
		RegistryCommit: lookupCommitA,
		Kind:           "skill",
		Name:           "helper",
	})
	if err != nil {
		t.Fatalf("lookup = %v", err)
	}
	if !result.Found || result.ContentDigest != lookupHelperD {
		t.Fatalf("cross-registry lookup = %+v, want found %s", result, lookupHelperD)
	}
	if pinnedRouteHit {
		t.Fatal("cross-registry lookup consulted the requirer's pinned commit")
	}
}

// A blocked or revoked revision-by-digest fetch on the pinned-commit manifest
// path is an unavailable candidate, not a resolution error.
func TestLookupRequirementUnavailablePinnedRevisionIsNotFound(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusGone} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			manifest, digest := lookupManifest(t,
				marketplaceartifact.RevisionResource{Kind: "skill", Name: "helper", ContentDigest: lookupHelperD},
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/registries/src/occurrences/" + lookupCommitA:
					_ = json.NewEncoder(w).Encode(lookupOccurrence(lookupCommitA, digest))
				case "/api/v1/manifests/" + digest:
					_, _ = w.Write(manifest)
				case "/api/v1/resources/src/skill/helper/revisions/" + lookupHelperD:
					http.Error(w, http.StatusText(status), status)
				default:
					http.Error(w, "not found", http.StatusNotFound)
				}
			}))
			defer server.Close()

			r := &Resolver{Cache: NewCache(t.TempDir())}
			result, err := r.LookupMarketplaceRequirement(t.Context(), resolver.MarketplaceRequirementQuery{
				Entry:          lookupEntry(server),
				RegistryID:     "src",
				SameRegistry:   true,
				RegistryCommit: lookupCommitA,
				Kind:           "skill",
				Name:           "helper",
				Path:           "tools",
			})
			if err != nil {
				t.Fatalf("status %d lookup error = %v, want not-found", status, err)
			}
			if result.Found {
				t.Fatalf("status %d lookup = %+v, want not found", status, result)
			}
		})
	}
}

// spec: resource-prerequisites/missing-candidate-has-not-found-reason
func TestLookupRequirementUnavailableRevisionsAreNotFound(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusGone, http.StatusNotFound} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, http.StatusText(status), status)
			}))
			defer server.Close()

			r := &Resolver{Cache: NewCache(t.TempDir())}
			result, err := r.LookupMarketplaceRequirement(t.Context(), resolver.MarketplaceRequirementQuery{
				Entry:        lookupEntry(server),
				RegistryID:   "src",
				RegistrySlug: "src",
				Kind:         "skill",
				Name:         "helper",
			})
			if err != nil {
				t.Fatalf("status %d lookup error = %v, want not-found", status, err)
			}
			if result.Found {
				t.Fatalf("status %d lookup = %+v, want not found", status, result)
			}
		})
	}
}

// spec: resource-prerequisites/unestablished-source-root-leaves-a-path-hint-unresolved
func TestLookupRequirementPathHintMatchesLatestOccurrenceRoot(t *testing.T) {
	manifestDigest := "sha256:" + strings.Repeat("e", 64)
	revision := assessedRevisionOccurrence(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "helper"},
		ContentDigest: store.RevisionDigest(lookupHelperD),
	}, lookupCommitA, manifestDigest)
	revision.Occurrence.SourcePath = "tools/helper/SKILL.md"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/resources/src/skill/helper":
			_ = json.NewEncoder(w).Encode(revision)
		case "/api/v1/registries/src/occurrences/" + lookupCommitA:
			_ = json.NewEncoder(w).Encode(lookupOccurrence(lookupCommitA, manifestDigest))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := &Resolver{Cache: NewCache(t.TempDir())}
	query := resolver.MarketplaceRequirementQuery{
		Entry:        lookupEntry(server),
		RegistryID:   "src",
		RegistrySlug: "src",
		Kind:         "skill",
		Name:         "helper",
	}
	query.Path = "tools"
	found, err := r.LookupMarketplaceRequirement(t.Context(), query)
	if err != nil || !found.Found {
		t.Fatalf("matching path hint lookup = %+v, %v", found, err)
	}
	query.Path = "other"
	missed, err := r.LookupMarketplaceRequirement(t.Context(), query)
	if err != nil || missed.Found {
		t.Fatalf("mismatched path hint lookup = %+v, %v, want not found", missed, err)
	}
}

// spec: resource-prerequisites/unestablished-source-root-leaves-a-path-hint-unresolved
func TestLookupRequirementOccurrenceErrorsStayFailures(t *testing.T) {
	revision := assessedRevisionOccurrence(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "helper"},
		ContentDigest: store.RevisionDigest(lookupHelperD),
	}, lookupCommitA, "sha256:"+strings.Repeat("e", 64))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/resources/src/skill/helper":
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/registries/src/occurrences/"):
			http.Error(w, "blocked", http.StatusForbidden)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := &Resolver{Cache: NewCache(t.TempDir())}
	result, err := r.LookupMarketplaceRequirement(t.Context(), resolver.MarketplaceRequirementQuery{
		Entry:        lookupEntry(server),
		RegistryID:   "src",
		RegistrySlug: "src",
		Kind:         "skill",
		Name:         "helper",
		Path:         "tools",
	})
	if err == nil {
		t.Fatalf("a blocked occurrence fetch must surface as an error, got %+v", result)
	}
}
