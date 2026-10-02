package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func BenchmarkCatalogPublicHandler(b *testing.B) {
	for _, backend := range []string{"sqlite", "postgres"} {
		b.Run(backend, func(b *testing.B) {
			ctx := context.Background()
			var meta store.MetadataStore
			if backend == "postgres" {
				dsn := os.Getenv("POSTGRES_DSN")
				if dsn == "" {
					b.Skip("POSTGRES_DSN is required for PostgreSQL measurements")
				}
				meta = postgres.NewWithSchema(dsn, fmt.Sprintf("catalog_bench_%d", time.Now().UnixNano()))
			} else {
				meta = sqlite.New(filepath.Join(b.TempDir(), "catalog.db"))
			}
			if err := meta.Open(ctx); err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = meta.Close() })
			cfg := seedCatalogBenchmark(b, ctx, meta)
			if _, err := (admin.Service{Metadata: meta, Deployment: admin.Deployment{AllowedOrigins: []string{"https://git.example"}}}).Adopt(ctx, cfg, admin.AdoptionRequest{
				Actor: store.ManagedActor{Issuer: "benchmark", Subject: "catalog"}, Reason: "isolated benchmark",
				RequestID: "catalog-benchmark", IdempotencyKey: "catalog-benchmark",
			}); err != nil {
				b.Fatal(err)
			}
			cfg.Management.Mode = "database"
			handler := NewServer(cfg, meta, nil).PublicHandler()
			probe := func(path, etag string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodGet, path, nil)
				if etag != "" {
					r.Header.Set("If-None-Match", etag)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			first := probe("/api/v1/catalog?limit=500", "")
			var page CatalogPage
			if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &page) != nil || len(page.Resources) != 500 || page.Complete || page.NextCursor == "" {
				b.Fatalf("first500 fixture: %d %s", first.Code, first.Body.String())
			}
			for _, tc := range []struct {
				name, path, etag string
				status, rows     int
			}{
				{"first1", "/api/v1/catalog?limit=1", "", http.StatusOK, 1},
				{"first100", "/api/v1/catalog?limit=100", "", http.StatusOK, 100},
				{"first500", "/api/v1/catalog?limit=500", "", http.StatusOK, 500},
				{"continuation", "/api/v1/catalog?limit=500&after=" + string(page.NextCursor), "", http.StatusOK, 76},
				{"matching304", "/api/v1/catalog?limit=500", first.Header().Get("ETag"), http.StatusNotModified, 0},
			} {
				b.Run(tc.name, func(b *testing.B) {
					control := probe(tc.path, tc.etag)
					if control.Code != tc.status {
						b.Fatalf("control status %d: %s", control.Code, control.Body.String())
					}
					if tc.status == http.StatusOK {
						var controlPage CatalogPage
						if err := json.Unmarshal(control.Body.Bytes(), &controlPage); err != nil || len(controlPage.Resources) != tc.rows {
							b.Fatalf("control rows: %d %v", len(controlPage.Resources), err)
						}
					} else if control.Body.Len() != 0 || tc.etag == "" {
						b.Fatal("conditional control must have an ETag and empty body")
					}
					r := httptest.NewRequest(http.MethodGet, tc.path, nil)
					if tc.etag != "" {
						r.Header.Set("If-None-Match", tc.etag)
					}
					pool, hasPool := meta.(interface{ PoolStats() sql.DBStats })
					var beforePool sql.DBStats
					if hasPool {
						beforePool = pool.PoolStats()
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						w := httptest.NewRecorder()
						handler.ServeHTTP(w, r)
						if w.Code != tc.status {
							b.Fatalf("request status %d: %s", w.Code, w.Body.String())
						}
					}
					b.StopTimer()
					if hasPool {
						afterPool := pool.PoolStats()
						b.ReportMetric(float64(afterPool.WaitCount-beforePool.WaitCount)/float64(b.N), "pool-waits/op")
						b.ReportMetric(float64(afterPool.WaitDuration-beforePool.WaitDuration)/float64(b.N), "pool-wait-ns/op")
					}
				})
			}
		})
	}
}

func seedCatalogBenchmark(b *testing.B, ctx context.Context, meta store.MetadataStore) config.Config {
	b.Helper()
	const marketplace = store.MarketplaceID("catalog-benchmark")
	cfg := config.Config{
		Instance:          config.Instance{ID: string(marketplace), Name: "Catalog benchmark", CanonicalURL: "https://marketplace.test"},
		CatalogProjection: config.CatalogProjection{Enabled: true},
		Policy:            config.Policy{LicenseAllowlist: []string{"MIT", "Apache-2.0"}, LicenseUndeclared: "reject"},
	}
	must := func(err error) {
		if err != nil {
			b.Fatal(err)
		}
	}
	digest := func(i int) store.RevisionDigest { return store.RevisionDigest(fmt.Sprintf("sha256:%064x", i)) }
	must(meta.PutMarketplace(ctx, store.Marketplace{ID: marketplace, Name: cfg.Instance.Name, CanonicalURL: cfg.Instance.CanonicalURL}))
	kinds := []string{KindSkill, KindCommand, KindSubagent, KindHook, KindMcpServer, KindCollection}
	for registryIndex := 0; registryIndex < 6; registryIndex++ {
		registry := store.RegistryID(fmt.Sprintf("registry-%02d", registryIndex))
		origin := fmt.Sprintf("https://git.example/internal/registry-%02d.git", registryIndex)
		cfg.Registries = append(cfg.Registries, config.Registry{ID: string(registry), Slug: string(registry), Origin: origin, Ref: "main", Acquisition: "mirror", License: "MIT", Roots: []config.RegistryRoot{{Path: "."}}})
		must(meta.PutRegistry(ctx, store.Registry{ID: registry, Slug: string(registry), Origins: []string{origin}, Ref: "main", Listed: true, Acquisition: store.AcquisitionMirror, License: "MIT", Roots: []store.RegistryRoot{{Path: "."}}}))
		for history := 0; history < 3; history++ {
			commit := fmt.Sprintf("%040x", 1+registryIndex*3+history)
			previous := ""
			if history > 0 {
				previous = fmt.Sprintf("%040x", registryIndex*3+history)
			}
			manifest := digest(10000 + registryIndex*3 + history)
			must(meta.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
				if err := tx.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: registry, Ref: "main", Commit: commit, RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror, SourceOrigin: origin, OccurredAt: time.Unix(int64(history+1), 0).UTC()}); err != nil {
					return err
				}
				for index := 0; index < 96; index++ {
					kind := kinds[index%len(kinds)]
					coordinate := store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: kind, Name: fmt.Sprintf("resource-%03d", index)}
					rev := store.ResourceRevision{Coordinate: coordinate, ContentDigest: digest(1 + registryIndex*288 + history*96 + index), License: "MIT", Description: strings.Repeat("catalog description ", 16)}
					if kind != KindCollection {
						rev.ArchiveDigest = store.ArchiveDigest(digest(20000 + registryIndex*288 + history*96 + index))
					}
					if err := tx.PutResourceRevision(ctx, rev); err != nil {
						return err
					}
					if err := tx.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: rev.ContentDigest, Commit: commit, ManifestDigest: manifest, SourcePath: fmt.Sprintf("resources/%s/%s.yaml", kind, coordinate.Name)}); err != nil {
						return err
					}
					assessment := store.ResourceAssessment{Coordinate: coordinate, ContentDigest: rev.ContentDigest, AnalyzerVersion: "catalog-benchmark/v1", PolicyDigest: store.PolicyDigest(digest(30000)), Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: digest(40000 + registryIndex*288 + history*96 + index), AssessedAt: time.Unix(1, 0).UTC()}
					for finding := 0; finding < 4; finding++ {
						assessment.Findings = append(assessment.Findings, store.Finding{Code: fmt.Sprintf("marketplace.find.benchmark-%d", finding), Severity: "info", Policy: "allow", Message: strings.Repeat("bounded finding ", 8), Location: &store.FindingLocation{Path: "resource/skill.json", Line: finding + 1}})
					}
					assessment, err := store.FinalizeResourceAssessment(assessment)
					if err != nil {
						return err
					}
					if err := tx.PutResourceAssessment(ctx, assessment); err != nil {
						return err
					}
					if err := tx.SetCurrentResourceAssessment(ctx, assessment); err != nil {
						return err
					}
					if kind == KindCollection {
						member := store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: KindSkill, Name: fmt.Sprintf("resource-%03d", index-5)}
						missing := store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: KindSkill, Name: "missing"}
						include := store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: KindCollection, Name: fmt.Sprintf("resource-%03d", (index+6)%96)}
						if err := tx.PutCollectionRevision(ctx, store.CollectionRevision{ID: string(rev.ContentDigest), RegistryID: registry, ManifestDigest: rev.ContentDigest, Members: []store.ResourceCoordinate{member, missing}, Includes: []store.ResourceCoordinate{include}}); err != nil {
							return err
						}
					}
				}
				return tx.SetRegistryHead(ctx, registry, commit, previous)
			}))
		}
	}
	return cfg
}
