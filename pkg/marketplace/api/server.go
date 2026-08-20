package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Server serves the Marketplace HTTP API and operations endpoints.
type Server struct {
	cfg    config.Config
	meta   store.MetadataStore
	blobs  store.BlobStore
	mpID   store.MarketplaceID
	public *http.Server
	ops    *http.Server

	requestsTotal  atomic.Int64
	activeRequests atomic.Int64
}

// PublicHandler returns the public API handler, useful for tests.
func (s *Server) PublicHandler() http.Handler { return s.public.Handler }

// OpsHandler returns the operations handler, useful for tests.
func (s *Server) OpsHandler() http.Handler { return s.ops.Handler }

// NewServer creates an API server for the given Marketplace configuration and
// stores. It does not start listeners; call Start.
func NewServer(cfg config.Config, meta store.MetadataStore, blobs store.BlobStore) *Server {
	s := &Server{
		cfg:   cfg,
		meta:  meta,
		blobs: blobs,
		mpID:  store.MarketplaceID(cfg.Instance.ID),
	}

	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/.well-known/agent-marketplace.json", s.methodGet(s.handleWellKnown))
	publicMux.HandleFunc("/api/v1/capabilities", s.methodGet(s.handleCapabilities))
	publicMux.HandleFunc("/api/v1/instance", s.methodGet(s.handleInstance))
	publicMux.HandleFunc("/api/v1/search", s.methodGet(s.handleSearch))
	publicMux.HandleFunc("/api/v1/sources/", s.methodGet(s.handleSources))
	publicMux.HandleFunc("/api/v1/resources/", s.methodGet(s.handleResources))
	publicMux.HandleFunc("/api/v1/manifests/", s.methodGet(s.handleManifests))
	publicMux.HandleFunc("/api/v1/artifacts/", s.methodGet(s.handleArtifacts))
	publicMux.HandleFunc("/s/", s.methodGet(s.handleBrowser))
	publicMux.HandleFunc("/healthz", s.methodGet(s.handleHealthz))

	opsMux := http.NewServeMux()
	opsMux.HandleFunc("/readyz", s.methodGet(s.handleReadyz))
	opsMux.HandleFunc("/metrics", s.methodGet(s.handleMetrics))

	s.public = &http.Server{
		Handler:      s.withMetrics(publicMux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	s.ops = &http.Server{
		Handler:      s.withMetrics(opsMux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Start runs the public and operations listeners until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	pubLn, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen public %q: %w", s.cfg.Listen, err)
	}
	opLn, err := net.Listen("tcp", s.cfg.OperationsListen)
	if err != nil {
		_ = pubLn.Close()
		return fmt.Errorf("listen operations %q: %w", s.cfg.OperationsListen, err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("public api listening", "addr", pubLn.Addr())
		if err := s.public.Serve(pubLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("operations api listening", "addr", opLn.Addr())
		if err := s.ops.Serve(opLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	<-ctx.Done()

	slog.Info("api shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.public.Shutdown(shutdownCtx)
	_ = s.ops.Shutdown(shutdownCtx)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return ctx.Err()
	}
}

func (s *Server) methodGet(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h(w, r)
	}
}

func (s *Server) withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.activeRequests.Add(1)
		defer s.activeRequests.Add(-1)
		s.requestsTotal.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	wk := DefaultWellKnown(s.cfg.Instance.ID, s.cfg.Instance.CanonicalURL)
	writeJSON(w, http.StatusOK, wk)
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, DefaultCapabilities())
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	mp, err := s.meta.GetMarketplace(r.Context(), s.mpID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "marketplace not found")
			return
		}
		slog.Error("failed to load marketplace", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load marketplace")
		return
	}
	writeJSON(w, http.StatusOK, mp)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "" && !IsMarketplaceKind(kind) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}

	source := q.Get("source")
	searchQ := q.Get("q")
	if len(searchQ) > 256 {
		writeError(w, http.StatusBadRequest, "query too long")
		return
	}

	limit, err := parseLimit(q.Get("limit"), 100, 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := parseOffset(q.Get("offset"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	revs, err := s.meta.SearchResourceRevisions(r.Context(), store.ResourceSearchFilter{
		Marketplace: s.mpID,
		Source:      source,
		Kind:        kind,
		Query:       searchQ,
	})
	if err != nil {
		slog.Error("search failed", "err", err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	slugs, err := s.sourceSlugs(r.Context())
	if err != nil {
		slog.Error("source slugs failed", "err", err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	total := len(revs)
	sort.Slice(revs, func(i, j int) bool {
		si := relevanceScore(revs[i].Coordinate.Name, searchQ)
		sj := relevanceScore(revs[j].Coordinate.Name, searchQ)
		if si != sj {
			return si > sj
		}
		if revs[i].Coordinate.Source != revs[j].Coordinate.Source {
			return revs[i].Coordinate.Source < revs[j].Coordinate.Source
		}
		if revs[i].Coordinate.Kind != revs[j].Coordinate.Kind {
			return revs[i].Coordinate.Kind < revs[j].Coordinate.Kind
		}
		return revs[i].Coordinate.Name < revs[j].Coordinate.Name
	})
	revs = slicePage(revs, offset, limit)

	items := make([]ResourceListItem, 0, len(revs))
	for _, rev := range revs {
		items = append(items, s.resourceToListItem(rev, slugs, relevanceScore(rev.Coordinate.Name, searchQ)))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	})
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sources/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}

	parts := strings.Split(path, "/")
	switch len(parts) {
	case 1:
		s.handleSourceDetail(w, r, parts[0])
	case 2:
		if parts[1] != "history" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleSourceHistory(w, r, parts[0])
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleSourceDetail(w http.ResponseWriter, r *http.Request, slugOrID string) {
	src, err := s.resolveSource(r.Context(), slugOrID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		slog.Error("failed to load source", "source", slugOrID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	src.CredentialsRef = ""
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) handleSourceHistory(w http.ResponseWriter, r *http.Request, slugOrID string) {
	src, err := s.resolveSource(r.Context(), slugOrID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load source")
		return
	}

	history, err := s.meta.SourceHistory(r.Context(), src.ID)
	if err != nil {
		slog.Error("source history failed", "source", src.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load source history")
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/resources/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")

	switch len(parts) {
	case 3:
		s.handleResourceDetail(w, r, parts[0], parts[1], parts[2])
	case 4:
		if parts[3] != "revisions" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleResourceRevisions(w, r, parts[0], parts[1], parts[2])
	case 5:
		if parts[3] != "revisions" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleResourceRevision(w, r, parts[0], parts[1], parts[2], parts[4])
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleResourceDetail(w http.ResponseWriter, r *http.Request, sourceSlug, kind, name string) {
	if !IsMarketplaceKind(kind) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	src, err := s.resolveSource(r.Context(), sourceSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load source")
		return
	}

	coord := store.ResourceCoordinate{Marketplace: s.mpID, Source: src.ID, Kind: kind, Name: name}
	rev, err := s.meta.GetLatestResourceRevision(r.Context(), coord)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "resource not found")
			return
		}
		slog.Error("resource detail failed", "coord", coord, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load resource")
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

func (s *Server) handleResourceRevisions(w http.ResponseWriter, r *http.Request, sourceSlug, kind, name string) {
	if !IsMarketplaceKind(kind) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	src, err := s.resolveSource(r.Context(), sourceSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load source")
		return
	}

	coord := store.ResourceCoordinate{Marketplace: s.mpID, Source: src.ID, Kind: kind, Name: name}
	revs, err := s.meta.ListResourceRevisions(r.Context(), coord)
	if err != nil {
		slog.Error("list revisions failed", "coord", coord, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load revisions")
		return
	}
	writeJSON(w, http.StatusOK, revs)
}

func (s *Server) handleResourceRevision(w http.ResponseWriter, r *http.Request, sourceSlug, kind, name, digest string) {
	if !IsMarketplaceKind(kind) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	src, err := s.resolveSource(r.Context(), sourceSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load source")
		return
	}

	coord := store.ResourceCoordinate{Marketplace: s.mpID, Source: src.ID, Kind: kind, Name: name}
	rev, err := s.meta.GetResourceRevision(r.Context(), coord, store.RevisionDigest(digest))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "revision not found")
			return
		}
		slog.Error("revision detail failed", "coord", coord, "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load revision")
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

func (s *Server) handleManifests(w http.ResponseWriter, r *http.Request) {
	digest := strings.TrimPrefix(r.URL.Path, "/api/v1/manifests/")
	digest = strings.Trim(digest, "/")
	if digest == "" {
		writeError(w, http.StatusNotFound, "manifest not found")
		return
	}

	data, err := s.blobs.Get(r.Context(), store.ArchiveDigest(digest))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "manifest not found")
			return
		}
		slog.Error("manifest fetch failed", "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch manifest")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	setImmutableCache(w)
	_, _ = w.Write(data)
}

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] != "sha256" {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}

	digest := store.ArchiveDigest("sha256:" + parts[1])
	data, err := s.blobs.Get(r.Context(), digest)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "artifact not found")
			return
		}
		slog.Error("artifact fetch failed", "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch artifact")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
	setImmutableCache(w)
	_, _ = w.Write(data)
}

func (s *Server) handleBrowser(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/s/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !IsMarketplaceKind(parts[1]) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	s.handleResourceDetail(w, r, parts[0], parts[1], parts[2])
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, err := s.meta.GetMarketplace(ctx, s.mpID); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	if err := s.meta.ReadyCheck(ctx, s.blobs); err != nil {
		slog.Error("readyz failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# HELP agent_marketplace_requests_total Total HTTP requests\n")
	_, _ = fmt.Fprintf(w, "# TYPE agent_marketplace_requests_total counter\n")
	_, _ = fmt.Fprintf(w, "agent_marketplace_requests_total %d\n", s.requestsTotal.Load())
	_, _ = fmt.Fprintf(w, "# HELP agent_marketplace_active_requests Active HTTP requests\n")
	_, _ = fmt.Fprintf(w, "# TYPE agent_marketplace_active_requests gauge\n")
	_, _ = fmt.Fprintf(w, "agent_marketplace_active_requests %d\n", s.activeRequests.Load())
}

func (s *Server) resolveSource(ctx context.Context, slugOrID string) (store.Source, error) {
	src, err := s.meta.GetSourceBySlug(ctx, slugOrID)
	if err == nil {
		return src, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Source{}, err
	}
	return s.meta.GetSource(ctx, store.SourceID(slugOrID))
}

func (s *Server) sourceSlugs(ctx context.Context) (map[store.SourceID]string, error) {
	sources, err := s.meta.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[store.SourceID]string, len(sources))
	for _, src := range sources {
		m[src.ID] = src.Slug
	}
	return m, nil
}

func (s *Server) resourceToListItem(rev store.ResourceRevision, slugs map[store.SourceID]string, score float64) ResourceListItem {
	item := ResourceListItem{
		SourceID:      string(rev.Coordinate.Source),
		SourceSlug:    slugs[rev.Coordinate.Source],
		Kind:          rev.Coordinate.Kind,
		Name:          rev.Coordinate.Name,
		Revision:      string(rev.ContentDigest),
		ContentDigest: string(rev.ContentDigest),
		Score:         score,
	}
	if rev.License != "" {
		if item.Metadata == nil {
			item.Metadata = make(map[string]string)
		}
		item.Metadata["license"] = rev.License
	}
	return item
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode json response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func setImmutableCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
}

func parseLimit(raw string, defaultVal, maxVal int) (int, error) {
	if raw == "" {
		return defaultVal, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid limit")
	}
	if v > maxVal {
		v = maxVal
	}
	return v, nil
}

func parseOffset(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid offset")
	}
	return v, nil
}

func slicePage[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func relevanceScore(name, query string) float64 {
	if query == "" {
		return 1.0
	}
	if strings.EqualFold(name, query) {
		return 1.0
	}
	if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
		return 0.5
	}
	return 0.0
}
