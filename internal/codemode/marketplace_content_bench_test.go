package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkMarketplaceContentLoad(b *testing.B) {
	for _, online := range []bool{false, true} {
		b.Run(fmt.Sprintf("online=%t", online), func(b *testing.B) {
			snapshotOfflineHome(b)
			a, err := hccartifact.FromIR(ir.NewSkill("bench", "Benchmark skill", "# bench\n", nil, nil, ir.SourceRef{}))
			if err != nil {
				b.Fatal(err)
			}
			a.Manifest.RegistryID = "src"
			a.Support = make(map[string]hccartifact.SupportFile)
			for i := 0; i < 16; i++ {
				a.Support[fmt.Sprintf("references/file-%02d.md", i)] = hccartifact.SupportFile{Data: []byte(strings.Repeat("support text\n", 64))}
			}
			raw, archiveDigest, err := hccartifact.Encode(a)
			if err != nil {
				b.Fatal(err)
			}
			coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "bench"}
			revision := a.Manifest.ContentDigest
			assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
				Coordinate: coordinate, ContentDigest: store.RevisionDigest(revision),
				AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
				Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
				AssessedAt: time.Unix(1, 0).UTC(),
			})
			if err != nil {
				b.Fatal(err)
			}
			rev := store.AssessedResourceRevision{ResourceRevision: store.ResourceRevision{
				Coordinate: coordinate, ContentDigest: store.RevisionDigest(revision), ArchiveDigest: store.ArchiveDigest(archiveDigest),
			}, Assessment: assessment, Occurrence: store.ResourceOccurrence{
				Coordinate: coordinate, ContentDigest: store.RevisionDigest(revision),
				Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)),
			}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/resources/src/skill/bench/revisions/"+revision {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(rev); err != nil {
					b.Error(err)
				}
			}))
			defer server.Close()
			cache := marketplace.NewCache(filepath.Join(b.TempDir(), "marketplace"))
			if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
				b.Fatal(err)
			}
			if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
				b.Fatal(err)
			}
			if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", "bench", revision, rev); err != nil {
				b.Fatal(err)
			}
			resource := &Resource{Ref: "marketplace:src/skill/bench", Kind: "skill", Name: "bench", Revision: revision, Assessment: "allow"}
			resource.SetOrigin(&Origin{Marketplace: &MarketplaceOrigin{
				Entry: config.MarketplaceEntry{URL: server.URL, InstanceID: "mp"}, RegistryID: "src", Kind: "skill", Name: "bench", Revision: revision,
			}})
			loader := &FileSkillLoader{Cache: cache, Online: online}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				content, err := loader.Load(context.Background(), resource)
				if err != nil || content.Content != "# bench\n" {
					b.Fatalf("Load content=%q err=%v", content.Content, err)
				}
			}
		})
	}
}
