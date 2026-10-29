package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func BenchmarkSearchHandlerInstallationsCached(b *testing.B) {
	server := benchSearchServer(b, 100)
	registries, err := server.registryIndex(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	counts := installationCounts{
		Counts:          make(map[installationResourceKey]int64),
		KnownRegistries: map[store.RegistryID]bool{"bench-source": true},
		ValidUntil:      time.Now().Add(time.Hour),
	}
	for i := 0; i < 100; i++ {
		counts.Counts[installationResourceKey{Registry: "bench-source", Kind: "skill", Name: fmt.Sprintf("skill-%04d", i)}] = int64(i%10 + 1)
	}
	server.installations = &installationMetricsReader{
		now:   time.Now,
		hints: make(chan struct{}, 1),
	}
	server.installations.snapshot.Store(&installationSnapshot{
		counts:      counts,
		signature:   registryOriginsSignature(registries),
		evaluatedAt: time.Now(),
		expiresAt:   time.Now().Add(time.Hour),
		staleUntil:  time.Now().Add(time.Hour),
	})
	handler := server.PublicHandler()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?sort=installations", nil)
	control := httptest.NewRecorder()
	handler.ServeHTTP(control, request)
	if control.Code != http.StatusOK || !strings.Contains(control.Body.String(), `"installation_count":`) {
		b.Fatal("cached adoption benchmark did not expose counts")
	}
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
