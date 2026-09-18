package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s *Server) checkAuthority(ctx context.Context) error {
	return store.CheckConfigurationAuthority(ctx, s.meta, s.mpID, s.cfg.Management.Mode)
}

func (s *Server) authorityHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			if err := s.checkAuthority(r.Context()); err != nil {
				slog.WarnContext(r.Context(), "public request rejected by configuration authority", "marketplace", s.mpID)
				w.Header().Set("Cache-Control", "private, no-store")
				writeError(w, http.StatusServiceUnavailable, "configuration authority unavailable")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
