package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func TestArtifactV2FailuresNeverDowngrade(t *testing.T) {
	for _, failure := range []string{"corrupt-v2", "missing-v2", "missing-v1"} {
		t.Run(failure, func(t *testing.T) {
			raw, revision, encoded := artifactPair(t)
			v1, v2 := 0, 0
			mux := http.NewServeMux()
			serveWellKnown(mux, "mp")
			mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
			mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
				v1++
				if failure == "missing-v1" {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write(raw)
			})
			mux.HandleFunc("/api/v1/blobs/", func(w http.ResponseWriter, r *http.Request) {
				v2++
				if failure == "missing-v2" {
					http.NotFound(w, r)
					return
				}
				if failure == "corrupt-v2" {
					_, _ = w.Write([]byte("corrupt"))
					return
				}
				_, _ = w.Write(encoded.Bytes)
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: string(revision.ContentDigest), Digest: string(revision.ContentDigest)}
			if failure == "missing-v1" {
				pin.LockArchiveDigest = string(revision.ArchiveDigest)
			} else {
				pin.LockArtifactSchema = "v2"
				pin.LockArtifactDigest = string(encoded.Reference.ArtifactDigest)
				pin.LockBlobDigest = string(encoded.Reference.BlobDigest)
			}
			cache := NewCache(t.TempDir())
			r := &Resolver{Cache: cache}
			resolved, errs := r.Resolve(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}, nil, resolver.MarketplaceResolveModeRefresh)
			expected := ErrExactArtifactUnavailable
			if failure == "corrupt-v2" {
				expected = artifact.ErrBlobDigestMismatch
			}
			if len(resolved) != 0 || len(errs) != 1 || !errors.Is(errs[0], expected) {
				t.Fatalf("failure: %v %v", resolved, errs)
			}
			if failure == "missing-v1" && v2 != 0 || failure != "missing-v1" && v1 != 0 || v1+v2 != 1 {
				t.Fatalf("schema substitution: v1=%d v2=%d", v1, v2)
			}
			marker := filepath.Join(filepath.Dir(cache.ArtifactPath(string(encoded.Reference.BlobDigest))), "verified.marker")
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("failure marked cache: %v", err)
			}
		})
	}
}
