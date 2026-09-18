package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func BenchmarkAuthorityGate(b *testing.B) {
	meta := sqlite.New(":memory:")
	if err := meta.Open(context.Background()); err != nil {
		b.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	server := NewServer(config.Config{Instance: config.Instance{ID: "market"}}, meta, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, tc := range []struct {
		name    string
		handler http.Handler
	}{
		{"baseline", next}, {"guarded", server.authorityHandler(next)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				response := httptest.NewRecorder()
				tc.handler.ServeHTTP(response, request)
				if response.Code != http.StatusNoContent {
					b.Fatal(response.Code)
				}
			}
		})
	}
}
