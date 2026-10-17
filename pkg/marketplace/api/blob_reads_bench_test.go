package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

// BenchmarkContentDigestRead includes local filesystem reads and HTTP handler
// dispatch, but no network transport, acquisition or ownership lookup.
func BenchmarkContentDigestRead(b *testing.B) {
	data := []byte(strings.Repeat("ordinary inert content\n", 320))
	digest := inspection.Digest(data)
	blobs := fsblob.New(b.TempDir())
	if err := blobs.Put(context.Background(), store.ArchiveDigest(digest), data); err != nil {
		b.Fatal(err)
	}
	server := payloadReadServer(b, blobs)
	handler := server.PublicHandler()
	path := "/api/v1/content/" + strings.Replace(digest, ":", "/", 1)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.Len() != len(data) {
			b.Fatalf("healthy content response = %d/%d", response.Code, response.Body.Len())
		}
	}
}
