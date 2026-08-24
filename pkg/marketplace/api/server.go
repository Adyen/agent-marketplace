package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/publicurl"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// MaxSourceHistoryEntries is the maximum number of occurrences returned by the
// source history endpoint. Entries are ordered newest first by the metadata
// store, so truncation is deterministic.
const MaxSourceHistoryEntries = store.MaxSourceHistoryEntries

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
	// Keep operations endpoints private even when a caller reaches the public
	// listener directly; the operations listener remains the only route.
	publicMux.HandleFunc("/readyz", s.handlePublicOperationsNotFound)
	publicMux.HandleFunc("/metrics", s.handlePublicOperationsNotFound)

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
	// Marketplace rows may predate public URL validation. Sanitize at the
	// response boundary so legacy canonical URLs cannot leak credentials.
	mp.CanonicalURL = publicurl.Sanitize(mp.CanonicalURL)
	if err := validatePublicMarketplace(mp); err != nil {
		slog.Error("instance contains invalid public metadata", "err", err)
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

	for _, rev := range revs {
		if err := validatePublicResourceRevision(rev); err != nil {
			slog.Error("search returned invalid resource revision", "err", err)
			writeError(w, http.StatusInternalServerError, "search failed")
			return
		}
		if rev.Coordinate.Marketplace != s.mpID {
			slog.Error("search returned resource from another Marketplace", "resource_marketplace", rev.Coordinate.Marketplace, "marketplace", s.mpID)
			writeError(w, http.StatusInternalServerError, "search failed")
			return
		}
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
	case 3:
		if parts[1] != "occurrences" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleSourceOccurrence(w, r, parts[0], parts[2])
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
	// Convert through the public DTO: the store model contains credentials and
	// refresh diagnostics that must never cross the API boundary.
	public := sourceDTO(src)
	if err := validatePublicSource(public); err != nil {
		slog.Error("source contains invalid public metadata", "source", slugOrID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	writeJSON(w, http.StatusOK, public)
}

func (s *Server) handleSourceOccurrence(w http.ResponseWriter, r *http.Request, slugOrID, commit string) {
	if !IsCanonicalGitCommit(commit) {
		writeError(w, http.StatusBadRequest, "invalid source occurrence commit")
		return
	}
	src, err := s.resolveSource(r.Context(), slugOrID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load source")
		return
	}
	occurrence, err := s.meta.GetSourceOccurrence(r.Context(), src.ID, commit)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source occurrence not found")
			return
		}
		slog.Error("source occurrence failed", "source", src.ID, "commit", commit, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load source occurrence")
		return
	}
	if err := validatePublicSourceOccurrence(occurrence); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load source occurrence")
		return
	}
	if occurrence.SourceID != src.ID || occurrence.Commit != commit {
		slog.Error("source occurrence did not match requested identity", "source", src.ID, "commit", commit, "occurrence_source", occurrence.SourceID, "occurrence_commit", occurrence.Commit)
		writeError(w, http.StatusInternalServerError, "failed to load source occurrence")
		return
	}
	origin := ""
	if len(src.Origins) > 0 {
		origin = publicAcquisitionOrigin(src.Origins[len(src.Origins)-1])
	}
	if strings.TrimSpace(origin) == "" {
		writeError(w, http.StatusInternalServerError, "source has no acquisition origin")
		return
	}
	writeJSON(w, http.StatusOK, ExactSourceOccurrence{
		SourceID: occurrence.SourceID, Ref: occurrence.Ref, Commit: occurrence.Commit,
		SourceRevisionManifestDigest: occurrence.SourceRevisionManifestDigest,
		Acquisition:                  occurrence.Acquisition, AcquisitionOrigin: origin,
		OccurredAt: occurrence.OccurredAt,
	})
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
	history = boundSourceHistory(history)
	for _, occurrence := range history {
		if err := validatePublicSourceOccurrence(occurrence); err != nil {
			slog.Error("source history contains invalid occurrence", "source", src.ID, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to load source history")
			return
		}
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
	if err := validatePublicResourceRevisionMatches(rev, coord, ""); err != nil {
		slog.Error("resource detail contains invalid revision", "coord", coord, "err", err)
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
	for _, rev := range revs {
		if err := validatePublicResourceRevisionMatches(rev, coord, ""); err != nil {
			slog.Error("resource revisions contain invalid revision", "coord", coord, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to load revisions")
			return
		}
	}
	writeJSON(w, http.StatusOK, revs)
}

func (s *Server) handleResourceRevision(w http.ResponseWriter, r *http.Request, sourceSlug, kind, name, digest string) {
	if !isCanonicalSHA256Digest(digest) {
		writeError(w, http.StatusBadRequest, "invalid revision digest")
		return
	}
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
	if err := validatePublicResourceRevisionMatches(rev, coord, digest); err != nil {
		slog.Error("revision detail contains invalid revision", "coord", coord, "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load revision")
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

func (s *Server) handleManifests(w http.ResponseWriter, r *http.Request) {
	digest := strings.TrimPrefix(r.URL.Path, "/api/v1/manifests/")
	if !isCanonicalSHA256Digest(digest) {
		writeError(w, http.StatusBadRequest, "invalid manifest digest")
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

	// Source revision manifests and collection manifests share this immutable
	// route. Validate the latter at the serving boundary so a published
	// collection can never be served with non-canonical or mismatched bytes.
	if collection, payload, decodeErr := hccartifact.DecodeCollectionManifest(bytes.NewReader(data)); decodeErr == nil && collection.Name != "" {
		if err := hccartifact.VerifyCollectionManifest(collection, payload); err != nil || collection.ContentDigest != digest {
			slog.Error("collection manifest blob is invalid", "digest", digest, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to fetch manifest")
			return
		}
		if err := hccartifact.ValidateFlatMirrorManifest(collection); err != nil {
			slog.Error("collection manifest blob is outside supported flat slice", "digest", digest, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to fetch manifest")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	setImmutableCache(w)
	_, _ = w.Write(data)
}

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] != "sha256" || !isLowerHexDigest(parts[1]) {
		writeError(w, http.StatusBadRequest, "invalid artifact digest")
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

func (s *Server) handlePublicOperationsNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
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

func boundSourceHistory(history []store.SourceOccurrence) []store.SourceOccurrence {
	if history == nil {
		return make([]store.SourceOccurrence, 0)
	}
	if len(history) > MaxSourceHistoryEntries {
		slog.Debug("source history response truncated", "entries", len(history), "max_entries", MaxSourceHistoryEntries)
		return history[:MaxSourceHistoryEntries]
	}
	return history
}

func isCanonicalSHA256Digest(value string) bool {
	return IsCanonicalSHA256Digest(value)
}

func validatePublicMarketplace(mp store.Marketplace) error {
	if strings.TrimSpace(string(mp.ID)) == "" || strings.TrimSpace(mp.Name) == "" || strings.TrimSpace(mp.CanonicalURL) == "" {
		return errors.New("marketplace identity is incomplete")
	}
	if err := publicurl.Validate(mp.CanonicalURL); err != nil {
		return fmt.Errorf("canonical URL: %w", err)
	}
	return nil
}

func validatePublicSource(src SourceDTO) error {
	if strings.TrimSpace(string(src.ID)) == "" || strings.TrimSpace(src.Slug) == "" || strings.TrimSpace(src.Ref) == "" {
		return errors.New("source identity is incomplete")
	}
	if len(src.Origins) == 0 || len(src.Roots) == 0 {
		return errors.New("source domain fields are incomplete")
	}
	if err := src.Acquisition.Validate(); err != nil {
		return err
	}
	for i, root := range src.Roots {
		if strings.TrimSpace(root.Path) == "" {
			return fmt.Errorf("source root %d has no path", i)
		}
	}
	return nil
}

func publicAcquisitionOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err == nil && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
		return publicurl.Redact(raw)
	}
	// Git/file origins are valid acquisition locations for private and local
	// deployments. Strip URL passwords while preserving the Git transport.
	parsed, err = url.Parse(raw)
	if err == nil && (strings.EqualFold(parsed.Scheme, "ssh") || strings.EqualFold(parsed.Scheme, "git") || strings.EqualFold(parsed.Scheme, "file")) {
		if parsed.User != nil {
			username := parsed.User.Username()
			parsed.User = url.User(username)
		}
		parsed.Fragment = ""
		return parsed.String()
	}
	if strings.HasPrefix(raw, "git@") && !strings.ContainsAny(raw, "\r\n") {
		return raw
	}
	return ""
}

func validatePublicSourceOccurrence(occ store.SourceOccurrence) error {
	if strings.TrimSpace(string(occ.SourceID)) == "" || strings.TrimSpace(occ.Ref) == "" || strings.TrimSpace(occ.Commit) == "" {
		return errors.New("source occurrence identity is incomplete")
	}
	if !IsCanonicalGitCommit(occ.Commit) {
		return errors.New("source occurrence commit is not canonical")
	}
	if !isCanonicalSHA256Digest(string(occ.SourceRevisionManifestDigest)) {
		return errors.New("source occurrence manifest digest is not canonical")
	}
	if err := occ.Acquisition.Validate(); err != nil {
		return err
	}
	if occ.MirroredArtifactDigest != "" && !isCanonicalSHA256Digest(string(occ.MirroredArtifactDigest)) {
		return errors.New("source occurrence artifact digest is not canonical")
	}
	if occ.OccurredAt.IsZero() {
		return errors.New("source occurrence timestamp is missing")
	}
	return nil
}

func validatePublicResourceRevision(rev store.ResourceRevision) error {
	coord := rev.Coordinate
	if strings.TrimSpace(string(coord.Marketplace)) == "" || strings.TrimSpace(string(coord.Source)) == "" || strings.TrimSpace(coord.Kind) == "" || strings.TrimSpace(coord.Name) == "" {
		return errors.New("resource coordinate is incomplete")
	}
	if !IsMarketplaceKind(coord.Kind) {
		return fmt.Errorf("resource coordinate kind %q is invalid", coord.Kind)
	}
	if !isCanonicalSHA256Digest(string(rev.ContentDigest)) {
		return errors.New("resource content digest is not canonical")
	}
	if coord.Kind == "collection" && rev.ArchiveDigest != "" {
		return errors.New("virtual collection resource revision must not have an archive digest")
	}
	if rev.ArchiveDigest != "" && !isCanonicalSHA256Digest(string(rev.ArchiveDigest)) {
		return errors.New("resource archive digest is not canonical")
	}
	if rev.Provenance != nil {
		if rev.Provenance.Commit != "" && !IsCanonicalGitCommit(rev.Provenance.Commit) {
			return errors.New("resource provenance commit is not canonical")
		}
		if rev.Provenance.ManifestDigest != "" && !isCanonicalSHA256Digest(string(rev.Provenance.ManifestDigest)) {
			return errors.New("resource provenance manifest digest is not canonical")
		}
	}
	return nil
}

func validatePublicResourceRevisionMatches(rev store.ResourceRevision, expected store.ResourceCoordinate, expectedDigest string) error {
	if err := validatePublicResourceRevision(rev); err != nil {
		return err
	}
	if rev.Coordinate != expected {
		return fmt.Errorf("resource coordinate %q does not match requested %q", rev.Coordinate, expected)
	}
	if expectedDigest != "" && string(rev.ContentDigest) != expectedDigest {
		return fmt.Errorf("resource content digest %q does not match requested %q", rev.ContentDigest, expectedDigest)
	}
	return nil
}

func isLowerHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
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
