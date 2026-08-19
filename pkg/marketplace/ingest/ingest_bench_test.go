package ingest

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkBuildSourceRevision(b *testing.B) {
	ctx := context.Background()
	repoDir := createIngestRepo(b)
	commit, err := repoCommit(repoDir)
	if err != nil {
		b.Fatalf("repo commit: %v", err)
	}
	cfg := testConfig(b, repoDir)
	source := store.Source{
		ID:          store.SourceID("src-bench"),
		Slug:        "bench-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-bench"), source, repoDir, commit)
		if err != nil {
			b.Fatalf("BuildSourceRevision: %v", err)
		}
	}
}
