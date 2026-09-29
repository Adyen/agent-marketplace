package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func BenchmarkListResourceRevisionsByContentDigest(b *testing.B) {
	ctx := context.Background()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		b.Skip("POSTGRES_DSN not set; skipping PostgreSQL benchmark")
	}
	schema := fmt.Sprintf("bench_digest_%d", time.Now().UnixNano())
	s := postgres.NewWithSchema(dsn, schema)
	defer func() {
		_ = s.Close()
		if db, err := sql.Open("pgx", dsn); err == nil {
			_, _ = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			_ = db.Close()
		}
	}()
	if err := s.Open(ctx); err != nil {
		b.Fatalf("Open: %v", err)
	}
	must := func(err error) {
		if err != nil {
			b.Fatal(err)
		}
	}
	digest := func(i int) store.RevisionDigest { return store.RevisionDigest(fmt.Sprintf("sha256:%064d", i)) }
	for reg := 0; reg < 5; reg++ {
		id := store.RegistryID(fmt.Sprintf("reg-%d", reg))
		must(s.PutRegistry(ctx, store.Registry{ID: id, Slug: string(id), Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}))
	}
	policy := 0
	for rev := 0; rev < 30; rev++ {
		for reg := 0; reg < 5; reg++ {
			id := store.RegistryID(fmt.Sprintf("reg-%d", reg))
			commit := fmt.Sprintf("commit-%d-%d", reg, rev)
			manifest := digest(9000 + rev*5 + reg)
			req := store.RegistryRevisionPublication{
				PublicationID: store.PublicationID(fmt.Sprintf("pub-%d-%d", reg, rev)),
				Occurrence: store.RegistryOccurrence{RegistryID: id, Ref: "main", Commit: commit,
					RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror},
			}
			for name := 0; name < 40; name++ {
				d := digest(0)
				if name != 0 || rev != 0 {
					d = digest(1 + name*30 + rev)
				}
				c := store.ResourceCoordinate{Marketplace: "mp", Registry: id, Kind: "skill", Name: fmt.Sprintf("res-%d", name)}
				policy++
				a, err := store.FinalizeResourceAssessment(store.ResourceAssessment{Coordinate: c, ContentDigest: d,
					AnalyzerVersion: "a/v1", PolicyDigest: store.PolicyDigest(digest(policy)), Decision: store.AssessmentDecisionAllow,
					InspectionManifestDigest: d, AssessedAt: time.Now().UTC()})
				must(err)
				req.Resources = append(req.Resources, store.ResourceRevision{Coordinate: c, ContentDigest: d})
				req.ResourceOccurrences = append(req.ResourceOccurrences, store.ResourceOccurrence{Coordinate: c, ContentDigest: d,
					Commit: commit, ManifestDigest: manifest})
				req.Assessments = append(req.Assessments, a)
			}
			must(s.PublishRegistryPublication(ctx, req))
		}
	}
	owners, err := s.ListResourceRevisionsByContentDigest(ctx, "mp", digest(0))
	must(err)
	if len(owners) != 5 {
		b.Fatalf("selected digest owners = %d, want 5", len(owners))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListResourceRevisionsByContentDigest(ctx, "mp", digest(0)); err != nil {
			b.Fatal(err)
		}
	}
}
