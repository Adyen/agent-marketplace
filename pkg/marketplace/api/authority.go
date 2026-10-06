package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s *Server) checkAuthority(ctx context.Context) error {
	return store.CheckConfigurationAuthority(ctx, s.meta, s.mpID, s.cfg.Management.Mode)
}

func (s *Server) authorityHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			started := time.Now()
			change, err := store.ReadConfigurationAuthority(r.Context(), s.meta, s.mpID, s.cfg.Management.Mode)
			s.metrics.observePhase(phaseAuthority, started)
			if err != nil {
				slog.WarnContext(r.Context(), "public request rejected by configuration authority", "marketplace", s.mpID)
				w.Header().Set("Cache-Control", "private, no-store")
				writeError(w, http.StatusServiceUnavailable, "configuration authority unavailable")
				return
			}
			if s.cfg.Management.Mode == "database" {
				r = r.WithContext(context.WithValue(r.Context(), authorityChangeContextKey{}, change))
			}
		}
		next.ServeHTTP(w, r)
	})
}
