package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

// benchSearchServer builds a server backed by an in-memory SQLite store holding
// num listed skill revisions. It is shared by the search benchmarks so they
// measure the same corpus.
func benchSearchServer(b *testing.B, num int) *Server {
	b.Helper()
	ctx := context.Background()

	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	b.Cleanup(func() { _ = meta.Close() })

	cfg := config.Config{
		Instance:         config.Instance{ID: "bench-mp", Name: "Bench", CanonicalURL: "https://bench"},
		Listen:           "127.0.0.1:0",
		OperationsListen: "127.0.0.1:0",
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: store.MarketplaceID(cfg.Instance.ID), Name: cfg.Instance.Name, CanonicalURL: cfg.Instance.CanonicalURL}); err != nil {
		b.Fatalf("put marketplace: %v", err)
	}

	src := store.Registry{
		ID:          store.RegistryID("bench-source"),
		Slug:        "bench",
		Ref:         "main",
		Origins:     []string{"https://bench.git"},
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := meta.PutRegistry(ctx, src); err != nil {
		b.Fatalf("put source: %v", err)
	}

	manifest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	commit := strings.Repeat("d", 40)
	if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: src.ID, Ref: src.Ref, Commit: commit, RegistryRevisionManifestDigest: manifest,
		Acquisition: src.Acquisition, OccurredAt: time.Now().UTC(),
	}); err != nil {
		b.Fatalf("put registry occurrence: %v", err)
	}
	for i := 0; i < num; i++ {
		revision := benchRevision(src.ID, store.MarketplaceID(cfg.Instance.ID), i)
		if err := meta.PutResourceRevision(ctx, revision); err != nil {
			b.Fatalf("put resource revision: %v", err)
		}
		if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{
			Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, Commit: commit, ManifestDigest: manifest,
		}); err != nil {
			b.Fatalf("put resource occurrence: %v", err)
		}
		assessment := store.ResourceAssessment{
			Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest, AnalyzerVersion: "1.0.0",
			PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("b", 64)),
			Decision:                 store.AssessmentDecisionAllow,
			InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("c", 64)),
			AssessedAt:               time.Now().UTC(),
		}
		if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
			b.Fatalf("put resource assessment: %v", err)
		}
		if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
			b.Fatalf("set current resource assessment: %v", err)
		}
	}

	return NewServer(cfg, meta, fsblob.New(b.TempDir()))
}

// benchDescription is a realistic one-line summary, sized like the
// descriptions the production instance actually serves.
const benchDescription = "Analyze user retention and engagement patterns by cohort. " +
	"Upload your data or describe what you need, and get retention curves, " +
	"feature adoption trends, and actionable insights."

func benchRevision(source store.RegistryID, marketplace store.MarketplaceID, i int) store.ResourceRevision {
	return store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: marketplace,
			Registry:    source,
			Kind:        KindSkill,
			Name:        fmt.Sprintf("skill-%04d", i),
		},
		ContentDigest: store.RevisionDigest(fmt.Sprintf("sha256:%064d", i)),
		ArchiveDigest: store.ArchiveDigest(fmt.Sprintf("sha256:%064d", i)),
		License:       "MIT",
		Description:   benchDescription,
	}
}

func validateSearchBenchmark(b *testing.B, handler http.Handler, request *http.Request, want int) {
	b.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != want || result.Total != want {
		b.Fatalf("search benchmark did not return its expected %d resources", want)
	}
}

// BenchmarkSearchHandler measures the search hot path without a loopback TCP
// connection, so the number reflects store access, revocation filtering,
// sorting, and DTO construction rather than the host's socket latency.
func BenchmarkSearchHandler(b *testing.B) {
	handler := benchSearchServer(b, 100).PublicHandler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=skill-0050", nil)
	validateSearchBenchmark(b, handler, request, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			b.Fatalf("status %d", recorder.Code)
		}
	}
}

// BenchmarkSearchHandlerAll measures the unfiltered listing, which is what the
// index page requests and therefore the widest DTO conversion fan-out.
func BenchmarkSearchHandlerAll(b *testing.B) {
	handler := benchSearchServer(b, 100).PublicHandler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	validateSearchBenchmark(b, handler, request, 100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			b.Fatalf("status %d", recorder.Code)
		}
	}
}
