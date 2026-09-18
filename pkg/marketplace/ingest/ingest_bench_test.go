package ingest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func BenchmarkBuildRegistryRevision(b *testing.B) {
	ctx := context.Background()
	repoDir := createIngestRepo(b)
	commit, err := repoCommit(repoDir)
	if err != nil {
		b.Fatalf("repo commit: %v", err)
	}
	cfg := testConfig(b, repoDir)
	source := store.Registry{
		ID:          store.RegistryID("src-bench"),
		Slug:        "bench-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildRegistryRevision(ctx, cfg, store.MarketplaceID("mp-bench"), source, repoDir, commit)
		if err != nil {
			b.Fatalf("BuildRegistryRevision: %v", err)
		}
	}
}

// BenchmarkAuditCacheLookup measures a realistic SQLite cache hit for a
// complete resource-audit logical key. All setup happens outside the timed loop.
func BenchmarkAuditCacheLookup(b *testing.B) {
	ctx := context.Background()
	s := sqlite.New(filepath.Join(b.TempDir(), "marketplace.db"))
	if err := s.Open(ctx); err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = s.Close() })

	coord := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID("mp-bench"),
		Registry:    store.RegistryID("reg-bench"),
		Kind:        "skill",
		Name:        "ready",
	}
	key := store.ResourceAuditKey{
		Coordinate:      coord,
		SubjectDigest:   store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
		AnalyzerID:      "marketplace.hidden-content",
		AnalyzerVersion: "1.2.0",
		ConfigDigest:    store.PolicyDigest("sha256:" + strings.Repeat("b", 64)),
	}
	audit := store.ResourceAudit{
		ResourceAuditKey: key,
		Role:             analyze.RoleGating,
		Verdict:          analyze.VerdictPass,
		Summary:          "no issues",
		Findings:         nil,
		Duration:         100 * time.Microsecond,
		Error:            "",
		AssessedAt:       time.Now().UTC(),
	}
	finalized, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		b.Fatalf("FinalizeResourceAudit: %v", err)
	}
	if err := s.PutResourceAudit(ctx, finalized); err != nil {
		b.Fatalf("PutResourceAudit: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.GetCachedResourceAudit(ctx, key)
		if err != nil {
			b.Fatalf("GetCachedResourceAudit: %v", err)
		}
	}
}
