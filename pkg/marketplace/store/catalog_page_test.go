package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func openCatalogTestStore(t *testing.T, backend string) store.MetadataStore {
	t.Helper()
	ctx := context.Background()
	if backend == "postgres" {
		dsn := os.Getenv("POSTGRES_DSN")
		if dsn == "" {
			t.Skip("POSTGRES_DSN is required for the PostgreSQL backend")
		}
		schema := fmt.Sprintf("am_t_%d", time.Now().UnixNano())
		meta := postgres.NewWithSchema(dsn, schema)
		if err := meta.Open(ctx); err != nil {
			t.Fatalf("open postgres: %v", err)
		}
		t.Cleanup(func() { _ = meta.Close() })
		t.Cleanup(func() {
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				return
			}
			defer func() { _ = db.Close() }()
			dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = db.ExecContext(dropCtx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		})
		return meta
	}
	meta := sqlite.New(filepath.Join(t.TempDir(), "catalog.db"))
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	return meta
}

func putCatalogCurrent(t *testing.T, ctx context.Context, meta store.MetadataStore, coord store.ResourceCoordinate, digestChar, commit string) store.ResourceRevision {
	t.Helper()
	rev := store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat(digestChar, 64)),
		License:       "MIT",
		Description:   coord.Name,
	}
	if err := meta.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("put revision %s: %v", coord.Name, err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: commit,
		ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("f", 64)),
	}); err != nil {
		t.Fatalf("put occurrence %s: %v", coord.Name, err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "catalog/v1",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)), AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("put assessment %s: %v", coord.Name, err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("set current assessment %s: %v", coord.Name, err)
	}
	return rev
}

func catalogPageCoordinates(page store.CatalogProjectionPage) []string {
	out := make([]string, 0, len(page.Resources))
	for _, resource := range page.Resources {
		out = append(out, resource.Coordinate.String()+"@"+string(resource.ContentDigest))
	}
	return out
}

// spec: marketplace-catalog-performance/duplicate-and-legacy-candidates-preserve-winning-pages
func TestCatalogPageEligibilityAndWinners(t *testing.T) {
	ctx := context.Background()
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			meta := openCatalogTestStore(t, backend)
			const (
				marketplace = store.MarketplaceID("mp")
				listed      = store.RegistryID("reg")
				unlisted    = store.RegistryID("hidden")
				head        = "head-commit"
				otherCommit = "other-commit"
			)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(meta.PutMarketplace(ctx, store.Marketplace{ID: marketplace, Name: "Catalog", CanonicalURL: "https://marketplace.test"}))
			must(meta.PutRegistry(ctx, store.Registry{ID: listed, Slug: "listed", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}))
			must(meta.PutRegistry(ctx, store.Registry{ID: unlisted, Slug: "hidden", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: false}))
			manifest := store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
			for _, registry := range []store.RegistryID{listed, unlisted} {
				must(meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: registry, Ref: "main", Commit: head, RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror}))
			}
			must(meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: listed, Ref: "main", Commit: otherCommit, RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror}))

			coord := func(kind, name string) store.ResourceCoordinate {
				return store.ResourceCoordinate{Marketplace: marketplace, Registry: listed, Kind: kind, Name: name}
			}
			putCatalogCurrent(t, ctx, meta, coord("skill", "winner"), "b", head)
			winner := putCatalogCurrent(t, ctx, meta, coord("skill", "winner"), "d", head)
			putCatalogCurrent(t, ctx, meta, coord("command", "cmd"), "a", head)
			collection := putCatalogCurrent(t, ctx, meta, coord("collection", "bundle"), "e", head)
			must(meta.PutCollectionRevision(ctx, store.CollectionRevision{
				ID: collection.ContentDigest.String(), RegistryID: listed, ManifestDigest: collection.ContentDigest,
				Members: []store.ResourceCoordinate{coord("skill", "winner")},
			}))
			putCatalogCurrent(t, ctx, meta, coord("skill", "obsolete"), "c", otherCommit)
			noOccurrence := coord("skill", "no-occurrence")
			must(meta.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: noOccurrence, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("1", 64))}))
			assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
				Coordinate: noOccurrence, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("1", 64)),
				AnalyzerVersion: "catalog/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
				Decision:                 store.AssessmentDecisionAllow,
				InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)), AssessedAt: time.Unix(1, 0).UTC(),
			})
			must(err)
			must(meta.PutResourceAssessment(ctx, assessment))
			must(meta.SetCurrentResourceAssessment(ctx, assessment))
			noAssessment := coord("skill", "no-assessment")
			must(meta.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: noAssessment, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("2", 64))}))
			must(meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: noAssessment, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("2", 64)), Commit: head, ManifestDigest: manifest}))
			unsupported := coord("plugin", "unsupported")
			must(meta.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: unsupported, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("3", 64))}))
			must(meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: unsupported, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("3", 64)), Commit: head, ManifestDigest: manifest}))
			putCatalogCurrent(t, ctx, meta, store.ResourceCoordinate{Marketplace: marketplace, Registry: unlisted, Kind: "skill", Name: "hidden"}, "9", head)

			must(meta.SetRegistryHead(ctx, listed, head, ""))
			must(meta.SetRegistryHead(ctx, unlisted, head, ""))

			wantOrder := []string{
				"mp/reg/collection/bundle@" + collection.ContentDigest.String(),
				"mp/reg/command/cmd@sha256:" + strings.Repeat("a", 64),
				"mp/reg/skill/winner@" + winner.ContentDigest.String(),
			}
			var seen []string
			after := (*store.CatalogProjectionCursor)(nil)
			for page := 0; ; page++ {
				result, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: marketplace, After: after, Limit: 1})
				if err != nil {
					t.Fatalf("page %d: %v", page, err)
				}
				if len(result.Resources) != 1 {
					t.Fatalf("page %d = %d resources, want 1 (lookahead)", page, len(result.Resources))
				}
				seen = append(seen, catalogPageCoordinates(result)...)
				if result.Next == nil {
					break
				}
				if page > len(wantOrder) {
					t.Fatal("lookahead cursor never terminated")
				}
				after = result.Next
			}
			if fmt.Sprint(seen) != fmt.Sprint(wantOrder) {
				t.Fatalf("page order = %v, want %v", seen, wantOrder)
			}
			full, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: marketplace, Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			if full.Next != nil || fmt.Sprint(catalogPageCoordinates(full)) != fmt.Sprint(wantOrder) {
				t.Fatalf("final page = %+v", catalogPageCoordinates(full))
			}
			if full.Resources[0].Collection == nil || len(full.Resources[0].Collection.Resources) != 1 ||
				full.Resources[0].Collection.Resources[0] != coord("skill", "winner") {
				t.Fatalf("collection members = %+v, want winner", full.Resources[0].Collection)
			}
			if full.Resources[2].ContentDigest != winner.ContentDigest {
				t.Fatalf("winning digest = %s, want %s", full.Resources[2].ContentDigest, winner.ContentDigest)
			}
		})
	}
}
