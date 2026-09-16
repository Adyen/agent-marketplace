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
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/publicurl"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/revocation"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// MaxRegistryHistoryEntries is the maximum number of occurrences returned by the
// registry history endpoint. Entries are ordered newest first by the metadata
// store, so truncation is deterministic.
const MaxRegistryHistoryEntries = store.MaxRegistryHistoryEntries
const maxRegistrySummaries = 1000

// Server serves the Marketplace HTTP API and operations endpoints.
type Server struct {
	cfg           config.Config
	meta          store.MetadataStore
	blobs         store.BlobStore
	mpID          store.MarketplaceID
	revocations   *revocation.Evaluator
	revocationErr error
	public        *http.Server
	ops           *http.Server

	metrics *serverMetrics
}

// PublicHandler returns the public API handler, useful for tests.
func (s *Server) PublicHandler() http.Handler { return s.public.Handler }

// OpsHandler returns the operations handler, useful for tests.
func (s *Server) OpsHandler() http.Handler { return s.ops.Handler }

// NewServer creates an API server for the given Marketplace configuration and
// stores. It does not start listeners; call Start.
func NewServer(cfg config.Config, meta store.MetadataStore, blobs store.BlobStore) *Server {
	evaluator, evaluatorErr := revocation.New(cfg)
	s := &Server{
		cfg:           cfg,
		meta:          meta,
		blobs:         blobs,
		mpID:          store.MarketplaceID(cfg.Instance.ID),
		revocations:   evaluator,
		revocationErr: evaluatorErr,
	}

	s.metrics = newServerMetrics()

	publicMux := http.NewServeMux()
	s.metrics.register(publicMux, publicListener, "/.well-known/agent-marketplace.json", s.methodGet(s.handleWellKnown))
	s.metrics.register(publicMux, publicListener, "/api/v1/capabilities", s.methodGet(s.handleCapabilities))
	s.metrics.register(publicMux, publicListener, "/api/v1/instance", s.methodGet(s.handleInstance))
	s.metrics.register(publicMux, publicListener, "/api/v1/search", s.methodGet(s.handleSearch))
	s.metrics.register(publicMux, publicListener, "/api/v1/registries", s.methodGet(s.handleRegistryList))
	s.metrics.register(publicMux, publicListener, "/api/v1/registries/", s.methodGet(s.handleRegistries))
	s.metrics.register(publicMux, publicListener, "/api/v1/resources/", s.methodGet(s.handleResources))
	s.metrics.register(publicMux, publicListener, "/api/v1/manifests/", s.methodGet(s.handleManifests))
	s.metrics.register(publicMux, publicListener, "/api/v1/assessments/", s.methodGet(s.handleAssessmentDigest))
	s.metrics.register(publicMux, publicListener, "/api/v1/inspection/", s.methodGet(s.handleInspectionDigest))
	s.metrics.register(publicMux, publicListener, "/api/v1/content/", s.methodGet(s.handleContentDigest))
	s.metrics.register(publicMux, publicListener, "/api/v1/artifacts/", s.methodGet(s.handleArtifacts))
	s.metrics.register(publicMux, publicListener, "/healthz", s.methodGet(s.handleHealthz))
	// Keep operations endpoints private even when a caller reaches the public
	// listener directly; the operations listener remains the only route.
	s.metrics.register(publicMux, publicListener, "/readyz", http.HandlerFunc(s.handlePublicOperationsNotFound))
	s.metrics.register(publicMux, publicListener, "/metrics", http.HandlerFunc(s.handlePublicOperationsNotFound))
	s.metrics.registerUnmatched(publicMux, publicListener)

	opsMux := http.NewServeMux()
	s.metrics.register(opsMux, operationsListener, "/readyz", s.methodGet(s.handleReadyz))
	s.metrics.register(opsMux, operationsListener, "/metrics", s.methodGet(s.metrics.scrape.ServeHTTP))
	s.metrics.registerUnmatched(opsMux, operationsListener)

	s.public = &http.Server{
		Handler:      publicMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	s.ops = &http.Server{
		Handler:      opsMux,
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

func (s *Server) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	wk := DefaultWellKnown(s.cfg.Instance.ID, s.cfg.Instance.CanonicalURL, s.cfg.Instance.BrowserBase)
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

	registryFilters := q["registry"]
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
		Registries:  registryFilters,
		Kind:        kind,
		Query:       searchQ,
	})
	if err != nil {
		slog.Error("search failed", "err", err)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	registries, err := s.registryIndex(r.Context())
	if err != nil {
		slog.Error("registry index failed", "err", err)
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
	revs = latestEffectiveRevisions(revs, s.revocations)
	total := len(revs)
	sort.Slice(revs, func(i, j int) bool {
		si := relevanceScore(revs[i].Coordinate.Name, searchQ)
		sj := relevanceScore(revs[j].Coordinate.Name, searchQ)
		if si != sj {
			return si > sj
		}
		if revs[i].Coordinate.Registry != revs[j].Coordinate.Registry {
			return revs[i].Coordinate.Registry < revs[j].Coordinate.Registry
		}
		if revs[i].Coordinate.Kind != revs[j].Coordinate.Kind {
			return revs[i].Coordinate.Kind < revs[j].Coordinate.Kind
		}
		return revs[i].Coordinate.Name < revs[j].Coordinate.Name
	})
	revs = slicePage(revs, offset, limit)

	items := make([]ResourceListItem, 0, len(revs))
	for _, rev := range revs {
		items = append(items, s.resourceToListItem(rev, registries, relevanceScore(rev.Coordinate.Name, searchQ)))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	})
}

func (s *Server) handleRegistryList(w http.ResponseWriter, r *http.Request) {
	registries, err := s.meta.ListRegistries(r.Context())
	if err != nil {
		slog.Error("failed to list registries", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list registries")
		return
	}
	sort.Slice(registries, func(i, j int) bool { return registries[i].Slug < registries[j].Slug })
	if len(registries) > maxRegistrySummaries {
		registries = registries[:maxRegistrySummaries]
	}
	type registrySummary struct {
		ID   store.RegistryID `json:"id"`
		Slug string           `json:"slug"`
	}
	items := make([]registrySummary, 0, len(registries))
	for _, registry := range registries {
		items = append(items, registrySummary{ID: registry.ID, Slug: registry.Slug})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleRegistries(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/registries/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeError(w, http.StatusNotFound, "registry not found")
		return
	}

	parts := strings.Split(path, "/")
	switch len(parts) {
	case 1:
		s.handleRegistryDetail(w, r, parts[0])
	case 2:
		if parts[1] != "history" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleRegistryHistory(w, r, parts[0])
	case 3:
		if parts[1] != "occurrences" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleRegistryOccurrence(w, r, parts[0], parts[2])
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleRegistryDetail(w http.ResponseWriter, r *http.Request, slugOrID string) {
	src, err := s.resolveRegistry(r.Context(), slugOrID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "registry not found")
			return
		}
		slog.Error("failed to load registry", "registry", slugOrID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load registry")
		return
	}
	// Convert through the public DTO: the store model contains credentials and
	// refresh diagnostics that must never cross the API boundary.
	public := registryDTO(src)
	if err := validatePublicRegistry(public); err != nil {
		slog.Error("registry contains invalid public metadata", "registry", slugOrID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load registry")
		return
	}
	writeJSON(w, http.StatusOK, public)
}

func (s *Server) handleRegistryOccurrence(w http.ResponseWriter, r *http.Request, slugOrID, commit string) {
	if !IsCanonicalGitCommit(commit) {
		writeError(w, http.StatusBadRequest, "invalid registry occurrence commit")
		return
	}
	src, err := s.resolveRegistry(r.Context(), slugOrID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "registry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load registry")
		return
	}
	occurrence, err := s.meta.GetRegistryOccurrence(r.Context(), src.ID, commit)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "registry occurrence not found")
			return
		}
		slog.Error("registry occurrence failed", "registry", src.ID, "commit", commit, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load registry occurrence")
		return
	}
	if err := validatePublicRegistryOccurrence(occurrence); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load registry occurrence")
		return
	}
	if occurrence.RegistryID != src.ID || occurrence.Commit != commit {
		slog.Error("registry occurrence did not match requested identity", "registry", src.ID, "commit", commit, "occurrence_registry", occurrence.RegistryID, "occurrence_commit", occurrence.Commit)
		writeError(w, http.StatusInternalServerError, "failed to load registry occurrence")
		return
	}
	origin := ""
	if len(src.Origins) > 0 {
		origin = publicAcquisitionOrigin(src.Origins[len(src.Origins)-1])
	}
	if strings.TrimSpace(origin) == "" {
		writeError(w, http.StatusInternalServerError, "registry has no acquisition origin")
		return
	}
	writeJSON(w, http.StatusOK, ExactRegistryOccurrence{
		RegistryID: occurrence.RegistryID, Ref: occurrence.Ref, Roots: occurrence.Roots, Commit: occurrence.Commit,
		RegistryRevisionManifestDigest: occurrence.RegistryRevisionManifestDigest,
		Acquisition:                    occurrence.Acquisition, AcquisitionOrigin: origin,
		OccurredAt: occurrence.OccurredAt,
	})
}

func (s *Server) handleRegistryHistory(w http.ResponseWriter, r *http.Request, slugOrID string) {
	src, err := s.resolveRegistry(r.Context(), slugOrID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "registry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load registry")
		return
	}

	history, err := s.meta.RegistryHistory(r.Context(), src.ID)
	if err != nil {
		slog.Error("registry history failed", "registry", src.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load registry history")
		return
	}
	history = boundRegistryHistory(history)
	for _, occurrence := range history {
		if err := validatePublicRegistryOccurrence(occurrence); err != nil {
			slog.Error("registry history contains invalid occurrence", "registry", src.ID, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to load registry history")
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
	case 6:
		if parts[3] != "revisions" || parts[5] != "inspection" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleResourceInspection(w, r, parts[0], parts[1], parts[2], parts[4])
	case 7:
		if parts[3] != "revisions" || parts[5] != "inspection" || parts[6] != "content" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.handleResourceInspectionContent(w, r, parts[0], parts[1], parts[2], parts[4], "")
	case 8:
		if parts[3] != "revisions" || parts[5] != "inspection" || parts[6] != "content" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if !isCanonicalSHA256Digest(parts[7]) {
			writeError(w, http.StatusBadRequest, "invalid inspection content digest")
			return
		}
		s.handleResourceInspectionContent(w, r, parts[0], parts[1], parts[2], parts[4], parts[7])
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleResourceDetail(w http.ResponseWriter, r *http.Request, registrySlug, kind, name string) {
	if !IsMarketplaceKind(kind) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	src, err := s.resolveRegistry(r.Context(), registrySlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "registry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load registry")
		return
	}

	coord := store.ResourceCoordinate{Marketplace: s.mpID, Registry: src.ID, Kind: kind, Name: name}
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
	writeJSON(w, http.StatusOK, s.applyEffectiveRevocation(rev))
}

func (s *Server) handleResourceRevisions(w http.ResponseWriter, r *http.Request, registrySlug, kind, name string) {
	if !IsMarketplaceKind(kind) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	src, err := s.resolveRegistry(r.Context(), registrySlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "registry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load registry")
		return
	}

	coord := store.ResourceCoordinate{Marketplace: s.mpID, Registry: src.ID, Kind: kind, Name: name}
	revs, err := s.meta.ListResourceRevisions(r.Context(), coord)
	if err != nil {
		slog.Error("list revisions failed", "coord", coord, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to load revisions")
		return
	}
	for i, rev := range revs {
		if err := validatePublicResourceRevisionMatches(rev, coord, ""); err != nil {
			slog.Error("resource revisions contain invalid revision", "coord", coord, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to load revisions")
			return
		}
		revs[i] = s.applyEffectiveRevocation(rev)
	}
	writeJSON(w, http.StatusOK, revs)
}

func (s *Server) handleResourceRevision(w http.ResponseWriter, r *http.Request, registrySlug, kind, name, digest string) {
	if !isCanonicalSHA256Digest(digest) {
		writeError(w, http.StatusBadRequest, "invalid revision digest")
		return
	}
	if !IsMarketplaceKind(kind) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	src, err := s.resolveRegistry(r.Context(), registrySlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "registry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load registry")
		return
	}

	coord := store.ResourceCoordinate{Marketplace: s.mpID, Registry: src.ID, Kind: kind, Name: name}
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
	writeJSON(w, http.StatusOK, s.applyEffectiveRevocation(rev))
}

func (s *Server) handleResourceInspection(w http.ResponseWriter, r *http.Request, registrySlug, kind, name, digest string) {
	_, payload, err := s.getResourceInspection(r.Context(), registrySlug, kind, name, digest)
	if err != nil {
		s.writeInspectionError(w, err)
		return
	}
	writeImmutable(w, "application/json", payload)
}

func (s *Server) handleResourceInspectionContent(w http.ResponseWriter, r *http.Request, registrySlug, kind, name, digest, fileDigest string) {
	manifest, _, err := s.getResourceInspection(r.Context(), registrySlug, kind, name, digest)
	if err != nil {
		s.writeInspectionError(w, err)
		return
	}
	var file *inspection.File
	for i := range manifest.Files {
		if (fileDigest == "" && manifest.Files[i].Path == manifest.PrimaryPath) || (fileDigest != "" && manifest.Files[i].ContentDigest == fileDigest) {
			file = &manifest.Files[i]
			break
		}
	}
	if file == nil || file.InspectionClass == inspection.ClassMetadata || !allowedInspectionMediaType(file.MediaType, file.InspectionClass) {
		writeError(w, http.StatusNotFound, "inspection content not available")
		return
	}
	data, err := s.blobs.Get(r.Context(), store.ArchiveDigest(file.ContentDigest))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "inspection content not found")
			return
		}
		slog.Error("inspection content fetch failed", "digest", file.ContentDigest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch inspection content")
		return
	}
	if s.cfg.Policy.Limits.FileBytes > 0 && int64(len(data)) > s.cfg.Policy.Limits.FileBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "inspection content exceeds size limit")
		return
	}
	if err := inspection.VerifyFileContent(*file, data); err != nil {
		slog.Error("inspection content does not match manifest", "digest", file.ContentDigest, "err", err)
		writeError(w, http.StatusInternalServerError, "inspection content does not match manifest")
		return
	}
	writeImmutable(w, file.MediaType, data)
}

func (s *Server) getResourceInspection(ctx context.Context, registrySlug, kind, name, digest string) (inspection.Manifest, []byte, error) {
	if !isCanonicalSHA256Digest(digest) {
		return inspection.Manifest{}, nil, errInvalidInspectionRevision
	}
	if !IsMarketplaceKind(kind) {
		return inspection.Manifest{}, nil, errInspectionUnavailable
	}
	src, err := s.resolveRegistry(ctx, registrySlug)
	if err != nil {
		return inspection.Manifest{}, nil, err
	}
	coord := store.ResourceCoordinate{Marketplace: s.mpID, Registry: src.ID, Kind: kind, Name: name}
	rev, err := s.meta.GetResourceRevision(ctx, coord, store.RevisionDigest(digest))
	if err != nil {
		return inspection.Manifest{}, nil, err
	}
	if !isCanonicalSHA256Digest(string(rev.Assessment.InspectionManifestDigest)) {
		return inspection.Manifest{}, nil, errInspectionUnavailable
	}
	manifestDigest := string(rev.Assessment.InspectionManifestDigest)
	payload, err := s.blobs.Get(ctx, store.ArchiveDigest(manifestDigest))
	if err != nil {
		return inspection.Manifest{}, nil, err
	}
	if !inspectionWithinLimit(len(payload), inspection.MaxManifestBytes) {
		return inspection.Manifest{}, nil, fmt.Errorf("inspection manifest exceeds serialized limit %d", inspection.MaxManifestBytes)
	}
	var manifest inspection.Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return inspection.Manifest{}, nil, fmt.Errorf("decode inspection manifest: %w", err)
	}
	if err := inspection.Verify(manifest, payload, manifestDigest); err != nil {
		return inspection.Manifest{}, nil, err
	}
	if manifest.Coordinate != coord || manifest.RevisionDigest != string(rev.ContentDigest) {
		return inspection.Manifest{}, nil, errors.New("inspection manifest identity mismatch")
	}
	return manifest, payload, nil
}

func (s *Server) writeInspectionError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, errInspectionUnavailable) {
		writeError(w, http.StatusNotFound, "inspection not found")
		return
	}
	if errors.Is(err, errInvalidInspectionRevision) {
		writeError(w, http.StatusBadRequest, "invalid inspection revision")
		return
	}
	slog.Error("inspection request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "failed to load inspection")
}

var (
	errInvalidInspectionRevision = errors.New("invalid inspection revision")
	errInspectionUnavailable     = errors.New("inspection unavailable")
)

func (s *Server) handleAssessmentDigest(w http.ResponseWriter, r *http.Request) {
	digest, ok := digestPath(r.URL.Path, "/api/v1/assessments/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid assessment digest")
		return
	}
	assessment, err := s.meta.GetResourceAssessment(r.Context(), store.AssessmentDigest(digest))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "assessment not found")
			return
		}
		slog.Error("assessment fetch failed", "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch assessment")
		return
	}
	if assessment.AssessmentDigest != store.AssessmentDigest(digest) || assessment.Coordinate.Marketplace != s.mpID || validatePublicResourceAssessment(assessment) != nil {
		slog.Error("assessment contains invalid public metadata", "digest", digest)
		writeError(w, http.StatusInternalServerError, "failed to fetch assessment")
		return
	}
	payload, err := json.Marshal(assessment)
	if err != nil {
		slog.Error("assessment encoding failed", "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch assessment")
		return
	}
	writeImmutable(w, "application/json", payload)
}

func (s *Server) handleInspectionDigest(w http.ResponseWriter, r *http.Request) {
	digest, ok := digestPath(r.URL.Path, "/api/v1/inspection/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid inspection digest")
		return
	}
	data, err := s.blobs.Get(r.Context(), store.ArchiveDigest(digest))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "inspection not found")
			return
		}
		slog.Error("inspection manifest fetch failed", "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch inspection")
		return
	}
	if !inspectionWithinLimit(len(data), inspection.MaxManifestBytes) {
		writeError(w, http.StatusRequestEntityTooLarge, "inspection manifest exceeds size limit")
		return
	}
	var manifest inspection.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil || inspection.Verify(manifest, data, digest) != nil {
		writeError(w, http.StatusInternalServerError, "invalid inspection manifest")
		return
	}
	writeImmutable(w, "application/json", data)
}

func (s *Server) handleContentDigest(w http.ResponseWriter, r *http.Request) {
	digest, ok := digestPath(r.URL.Path, "/api/v1/content/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid content digest")
		return
	}
	data, err := s.blobs.Get(r.Context(), store.ArchiveDigest(digest))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "content not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch content")
		return
	}
	if s.cfg.Policy.Limits.FileBytes > 0 && int64(len(data)) > s.cfg.Policy.Limits.FileBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "content exceeds size limit")
		return
	}
	if inspectionDigestOf(data) != digest {
		writeError(w, http.StatusInternalServerError, "content digest mismatch")
		return
	}
	media, allowed := fixedContentMediaType(data)
	if !allowed {
		writeError(w, http.StatusUnsupportedMediaType, "content media type is not allowed")
		return
	}
	writeImmutable(w, media, data)
}

func writeImmutable(w http.ResponseWriter, mediaType string, data []byte) {
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	setImmutableCache(w)
	_, _ = w.Write(data)
}

func allowedInspectionMediaType(mediaType, class string) bool {
	return inspection.AllowedMediaType(mediaType, class) && class != inspection.ClassMetadata
}

func fixedContentMediaType(data []byte) (string, bool) {
	if media := http.DetectContentType(data); media == "image/png" || media == "image/jpeg" || media == "image/gif" || media == "image/webp" {
		return media, true
	}
	if !inspection.IsSafeText(data) {
		return "", false
	}
	if json.Valid(data) {
		return "application/json", true
	}
	return "text/plain", true
}

func inspectionDigestOf(data []byte) string { return inspection.Digest(data) }

func inspectionWithinLimit(size int, limit int64) bool {
	return limit <= 0 || int64(size) <= limit
}

// digestPath accepts only the URL spelling used by immutable digest routes:
// /sha256/<64 lowercase hexadecimal characters>. The storage spelling remains
// sha256:<hex>.
func digestPath(requestPath, prefix string) (string, bool) {
	if !strings.HasPrefix(requestPath, prefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(requestPath, prefix), "/")
	if len(parts) != 2 || parts[0] != "sha256" || !isLowerHexDigest(parts[1]) {
		return "", false
	}
	return "sha256:" + parts[1], true
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

	// Registry revision manifests and collection manifests share this immutable
	// route. The blob store is also used for archives, so the route must prove
	// the payload's manifest type, canonical representation, and identity before
	// returning it. An arbitrary JSON or archive blob must never become a public
	// manifest merely because it was stored under a digest-shaped key.
	if err := validateManifestPayload(data, digest, manifestPayloadLimit(s.cfg)); err != nil {
		if errors.Is(err, errManifestUnsupported) {
			writeError(w, http.StatusUnsupportedMediaType, "manifest media type is not supported")
			return
		}
		slog.Error("manifest blob is invalid", "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch manifest")
		return
	}
	restriction, err := s.manifestRestriction(r.Context(), store.RevisionDigest(digest), data)
	if err != nil {
		slog.Error("manifest ownership lookup failed", "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch manifest")
		return
	}
	switch restriction {
	case AvailabilityRevoked:
		writeError(w, http.StatusGone, "manifest is revoked")
		return
	case AvailabilityBlocked:
		writeError(w, http.StatusForbidden, "manifest is blocked by assessment")
		return
	}

	writeImmutable(w, "application/json", data)
}

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] != "sha256" || !isLowerHexDigest(parts[1]) {
		writeError(w, http.StatusBadRequest, "invalid artifact digest")
		return
	}

	digest := store.ArchiveDigest("sha256:" + parts[1])
	revisions, err := s.meta.ListResourceRevisionsByArchiveDigest(r.Context(), s.mpID, digest)
	if err != nil {
		slog.Error("artifact ownership lookup failed", "digest", digest, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch artifact")
		return
	}
	// The digest address is not itself authorization to read a blob. Refuse an
	// unowned digest before touching blob storage, including when an orphaned
	// blob happens to exist at the requested address.
	if len(revisions) == 0 {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	switch s.resourceOwnerRestriction(revisions) {
	case AvailabilityRevoked:
		writeError(w, http.StatusGone, "artifact is revoked")
		return
	case AvailabilityBlocked:
		writeError(w, http.StatusForbidden, "artifact is blocked by assessment")
		return
	}
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
	artifactLimit := s.cfg.Policy.Limits.ArtifactBytes
	if artifactLimit <= 0 {
		artifactLimit = config.DefaultArtifactBytes
	}
	if int64(len(data)) > artifactLimit {
		writeError(w, http.StatusRequestEntityTooLarge, "artifact exceeds size limit")
		return
	}
	artifact, rawArchive, err := hccartifact.Decode(bytes.NewReader(data))
	if err == nil && artifact.Manifest.ArchiveDigest != string(digest) {
		err = fmt.Errorf("manifest archive digest %q does not match requested %q", artifact.Manifest.ArchiveDigest, digest)
	}
	if err == nil {
		err = artifact.VerifyArchiveDigest(rawArchive)
	}
	if err != nil {
		slog.Error("artifact blob digest mismatch", "digest", digest, "error", err)
		writeError(w, http.StatusInternalServerError, "artifact digest mismatch")
		return
	}

	writeImmutable(w, "application/vnd.agent-marketplace.artifact.v1.tar+gzip", data)
}

var errManifestUnsupported = errors.New("unsupported manifest payload")

func manifestPayloadLimit(cfg config.Config) int64 {
	if cfg.Policy.Limits.ArtifactBytes > 0 {
		return cfg.Policy.Limits.ArtifactBytes
	}
	return config.DefaultArtifactBytes
}

// validateManifestPayload accepts the two manifest formats published by the
// Marketplace. Registry revision manifests are content-addressed by the
// canonical payload with revision_digest omitted; the encoded compatibility
// form with revision_digest is also accepted. Collection manifests carry their
// content_digest in the payload. Both forms must be canonical and match the
// digest in the URL.
func validateManifestPayload(data []byte, digest string, limit int64) error {
	if limit > 0 && int64(len(data)) > limit {
		return fmt.Errorf("manifest payload size %d exceeds limit %d", len(data), limit)
	}

	var shape map[string]json.RawMessage
	if err := json.Unmarshal(data, &shape); err != nil || shape == nil {
		return errManifestUnsupported
	}

	_, registryShape := shape["generator"]
	if !registryShape {
		_, registryShape = shape["resources"]
	}
	if !registryShape {
		_, registryShape = shape["revision_digest"]
	}
	if registryShape {
		manifest, payload, err := marketplaceartifact.DecodeRegistryRevisionManifest(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("decode registry revision manifest: %w", err)
		}
		if manifest.RevisionDigest != "" {
			if manifest.RevisionDigest != digest {
				return fmt.Errorf("registry revision manifest digest %q does not match requested %q", manifest.RevisionDigest, digest)
			}
			if err := marketplaceartifact.VerifyRegistryRevisionManifest(manifest, payload); err != nil {
				return fmt.Errorf("verify registry revision manifest: %w", err)
			}
			return nil
		}
		canonical, computed, err := marketplaceartifact.CanonicalRegistryRevisionPayload(manifest)
		if err != nil {
			return fmt.Errorf("validate registry revision manifest: %w", err)
		}
		if !bytes.Equal(canonical, payload) {
			return errors.New("registry revision manifest is not canonical JSON")
		}
		if computed != digest {
			return fmt.Errorf("registry revision manifest digest mismatch: got %s, want %s", computed, digest)
		}
		return nil
	}

	_, collectionShape := shape["name"]
	if !collectionShape {
		_, collectionShape = shape["members"]
	}
	if !collectionShape {
		_, collectionShape = shape["content_digest"]
	}
	if !collectionShape {
		_, collectionShape = shape["supported_schema_versions"]
	}
	if !collectionShape {
		return errManifestUnsupported
	}
	collection, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode collection manifest: %w", err)
	}
	if err := hccartifact.VerifyCollectionManifest(collection, payload); err != nil {
		return fmt.Errorf("verify collection manifest: %w", err)
	}
	if collection.ContentDigest != digest {
		return fmt.Errorf("collection manifest digest %q does not match requested %q", collection.ContentDigest, digest)
	}
	return nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePublicOperationsNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.revocationsReady(ctx); err != nil {
		slog.Error("readyz failed: live revocations are not reconciled", "err", err)
		writeError(w, http.StatusServiceUnavailable, "not ready")
		return
	}
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

func (s *Server) resolveRegistry(ctx context.Context, slugOrID string) (store.Registry, error) {
	src, err := s.meta.GetRegistryBySlug(ctx, slugOrID)
	if err == nil {
		return src, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Registry{}, err
	}
	return s.meta.GetRegistry(ctx, store.RegistryID(slugOrID))
}

func (s *Server) revocationsReady(ctx context.Context) error {
	if s.revocationErr != nil {
		return s.revocationErr
	}
	if s.revocations == nil {
		return errors.New("effective revocation evaluator is unavailable")
	}
	desired := s.revocations.Declarations()
	actual := make(map[string]struct{}, len(desired))
	for _, kind := range []store.RevocationKind{store.RevocationRegistry, store.RevocationCoordinate, store.RevocationRevision} {
		rows, err := s.meta.ListRevocations(ctx, kind)
		if err != nil {
			return fmt.Errorf("list active %s revocations: %w", kind, err)
		}
		for _, row := range rows {
			actual[revocationKey(row.Kind, row.TargetID)] = struct{}{}
		}
	}
	if len(actual) != len(desired) {
		return fmt.Errorf("active revocation set is not reconciled: have %d declarations, want %d", len(actual), len(desired))
	}
	for _, wanted := range desired {
		if _, ok := actual[revocationKey(wanted.Kind, wanted.TargetID)]; !ok {
			return fmt.Errorf("%s revocation %q is not reconciled", wanted.Kind, wanted.TargetID)
		}
	}
	return nil
}

func revocationKey(kind store.RevocationKind, targetID string) string {
	return string(kind) + "\x00" + targetID
}

func (s *Server) applyEffectiveRevocation(rev store.AssessedResourceRevision) store.AssessedResourceRevision {
	if s.revocations == nil {
		return rev
	}
	return s.revocations.Apply(rev)
}

// latestEffectiveRevisions expects revisions grouped by coordinate with the
// latest revision first. It preserves the store's latest non-revoked selection
// while evaluating revocations from the live operator configuration.
func latestEffectiveRevisions(revs []store.AssessedResourceRevision, evaluator *revocation.Evaluator) []store.AssessedResourceRevision {
	selected := make([]store.AssessedResourceRevision, 0, len(revs))
	seen := make(map[store.ResourceCoordinate]struct{}, len(revs))
	for _, revision := range revs {
		if _, ok := seen[revision.Coordinate]; ok {
			continue
		}
		if evaluator != nil {
			revision = evaluator.Apply(revision)
		}
		if revision.Revocation != nil {
			continue
		}
		seen[revision.Coordinate] = struct{}{}
		selected = append(selected, revision)
	}
	return selected
}

// resourceOwnerRestriction withholds digest-addressed bytes when any owning
// current assessment rejects them or any owning revision is effectively
// revoked. A digest route has no coordinate and therefore cannot safely make a
// narrower authorization decision.
func (s *Server) resourceOwnerRestriction(revisions []store.AssessedResourceRevision) Availability {
	restriction := AvailabilityAvailable
	for _, revision := range revisions {
		if s.applyEffectiveRevocation(revision).Revocation != nil {
			return AvailabilityRevoked
		}
		if revision.Assessment.Decision == store.AssessmentDecisionReject {
			restriction = AvailabilityBlocked
		}
	}
	return restriction
}

// manifestRestriction resolves all current assessments that own a registry or
// collection manifest. Registry manifest entries provide the content digests
// needed to find historical owners without relying on the current registry head.
func (s *Server) manifestRestriction(ctx context.Context, digest store.RevisionDigest, payload []byte) (Availability, error) {
	if s.revocations == nil {
		return AvailabilityBlocked, s.revocationErr
	}
	owners := make([]store.AssessedResourceRevision, 0)
	collections, err := s.meta.ListResourceRevisionsByContentDigest(ctx, s.mpID, digest)
	if err != nil {
		return AvailabilityBlocked, err
	}
	for _, revision := range collections {
		if revision.Coordinate.Kind == KindCollection {
			owners = append(owners, revision)
		}
	}
	var registryManifest struct {
		Resources []struct {
			ContentDigest store.RevisionDigest `json:"content_digest"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(payload, &registryManifest); err != nil {
		return AvailabilityBlocked, err
	}
	for _, resource := range registryManifest.Resources {
		revisions, err := s.meta.ListResourceRevisionsByContentDigest(ctx, s.mpID, resource.ContentDigest)
		if err != nil {
			return AvailabilityBlocked, err
		}
		owners = append(owners, revisions...)
	}
	if restriction := s.resourceOwnerRestriction(owners); restriction != AvailabilityAvailable {
		return restriction, nil
	}
	// An exact collection declaration remains authoritative even when metadata
	// has not yet been imported for the immutable manifest.
	for _, target := range s.revocations.Targets() {
		if target.Coordinate.Marketplace == s.mpID && target.Coordinate.Kind == KindCollection && target.ContentDigest == digest {
			return AvailabilityRevoked, nil
		}
	}
	return AvailabilityAvailable, nil
}

// registryIndex loads every registry once so a listing can resolve slugs and
// acquisition modes for its whole page without a per-resource lookup.
func (s *Server) registryIndex(ctx context.Context) (map[store.RegistryID]store.Registry, error) {
	registries, err := s.meta.ListRegistries(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[store.RegistryID]store.Registry, len(registries))
	for _, registry := range registries {
		m[registry.ID] = registry
	}
	return m, nil
}

// resourceAvailability reports whether a revision can be installed from this
// instance, using only what the listing already holds.
//
// A rejected revision is stored and indexed on purpose, for transparency, but
// it is never installable. The explicit current assessment decision is the
// authority; findings are explanatory and must not be reinterpreted as policy.
// The archive check is qualified by acquisition mode because an index-only
// registry legitimately has no archive, and a collection is addressed by its
// manifest digest rather than an archive.
func resourceAvailability(rev store.AssessedResourceRevision, registry store.Registry) Availability {
	if rev.Revocation != nil {
		return AvailabilityRevoked
	}
	if rev.Assessment.Decision == store.AssessmentDecisionReject {
		return AvailabilityPolicyBlocked
	}
	if registry.Acquisition == store.AcquisitionMirror && rev.Coordinate.Kind != KindCollection && rev.ArchiveDigest == "" {
		slog.Debug("listing resource without a mirrored artifact as blocked",
			"coordinate", rev.Coordinate,
			"content_digest", rev.ContentDigest,
		)
		return AvailabilityNoArtifact
	}
	return AvailabilityAvailable
}

func (s *Server) resourceToListItem(rev store.AssessedResourceRevision, registries map[store.RegistryID]store.Registry, score float64) ResourceListItem {
	registry := registries[rev.Coordinate.Registry]
	item := ResourceListItem{
		RegistryID:    string(rev.Coordinate.Registry),
		RegistrySlug:  registry.Slug,
		Kind:          rev.Coordinate.Kind,
		Name:          rev.Coordinate.Name,
		Revision:      string(rev.ContentDigest),
		ContentDigest: string(rev.ContentDigest),
		// Bound again on the way out. The ingest path already bounds what it
		// writes, but this response must stay safe for rows written by any
		// other producer against the same database.
		Description:  store.BoundDescription(rev.Description),
		Availability: resourceAvailability(rev, registry),
		Score:        score,
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

func boundRegistryHistory(history []store.RegistryOccurrence) []store.RegistryOccurrence {
	if history == nil {
		return make([]store.RegistryOccurrence, 0)
	}
	if len(history) > MaxRegistryHistoryEntries {
		slog.Debug("registry history response truncated", "entries", len(history), "max_entries", MaxRegistryHistoryEntries)
		return history[:MaxRegistryHistoryEntries]
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

func validatePublicRegistry(src RegistryDTO) error {
	if strings.TrimSpace(string(src.ID)) == "" || strings.TrimSpace(src.Slug) == "" || strings.TrimSpace(src.Ref) == "" {
		return errors.New("registry identity is incomplete")
	}
	if len(src.Origins) == 0 || len(src.Roots) == 0 {
		return errors.New("registry domain fields are incomplete")
	}
	if err := src.Acquisition.Validate(); err != nil {
		return err
	}
	for i, root := range src.Roots {
		if strings.TrimSpace(root.Path) == "" {
			return fmt.Errorf("registry root %d has no path", i)
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

func validatePublicRegistryOccurrence(occ store.RegistryOccurrence) error {
	if strings.TrimSpace(string(occ.RegistryID)) == "" || strings.TrimSpace(occ.Ref) == "" || strings.TrimSpace(occ.Commit) == "" {
		return errors.New("registry occurrence identity is incomplete")
	}
	if !IsCanonicalGitCommit(occ.Commit) {
		return errors.New("registry occurrence commit is not canonical")
	}
	if len(occ.Roots) == 0 {
		return errors.New("registry occurrence roots are missing")
	}
	for i, root := range occ.Roots {
		if strings.TrimSpace(root.Path) == "" {
			return fmt.Errorf("registry occurrence root %d has no path", i)
		}
	}
	if !isCanonicalSHA256Digest(string(occ.RegistryRevisionManifestDigest)) {
		return errors.New("registry occurrence manifest digest is not canonical")
	}
	if err := occ.Acquisition.Validate(); err != nil {
		return err
	}
	if occ.MirroredArtifactDigest != "" && !isCanonicalSHA256Digest(string(occ.MirroredArtifactDigest)) {
		return errors.New("registry occurrence artifact digest is not canonical")
	}
	if occ.OccurredAt.IsZero() {
		return errors.New("registry occurrence timestamp is missing")
	}
	return nil
}

func validatePublicResourceRevision(rev store.AssessedResourceRevision) error {
	coord := rev.Coordinate
	if strings.TrimSpace(string(coord.Marketplace)) == "" || strings.TrimSpace(string(coord.Registry)) == "" || strings.TrimSpace(coord.Kind) == "" || strings.TrimSpace(coord.Name) == "" {
		return errors.New("resource coordinate is incomplete")
	}
	if !IsMarketplaceKind(coord.Kind) {
		return fmt.Errorf("resource coordinate kind %q is invalid", coord.Kind)
	}
	if !isCanonicalSHA256Digest(string(rev.ContentDigest)) {
		return errors.New("resource content digest is not canonical")
	}
	if coord.Kind == KindCollection && rev.ArchiveDigest != "" {
		return errors.New("virtual collection resource revision must not have an archive digest")
	}
	if rev.ArchiveDigest != "" && !isCanonicalSHA256Digest(string(rev.ArchiveDigest)) {
		return errors.New("resource archive digest is not canonical")
	}
	if err := validatePublicResourceAssessment(rev.Assessment); err != nil {
		return err
	}
	if rev.Assessment.Coordinate != coord || rev.Assessment.ContentDigest != rev.ContentDigest {
		return errors.New("resource assessment identity does not match revision")
	}
	if rev.Occurrence.Coordinate != coord || rev.Occurrence.ContentDigest != rev.ContentDigest {
		return errors.New("resource occurrence identity does not match revision")
	}
	if !IsCanonicalGitCommit(rev.Occurrence.Commit) {
		return errors.New("resource occurrence commit is not canonical")
	}
	if !isCanonicalSHA256Digest(string(rev.Occurrence.ManifestDigest)) {
		return errors.New("resource occurrence manifest digest is not canonical")
	}
	return nil
}

func validatePublicResourceAssessment(assessment store.ResourceAssessment) error {
	if err := assessment.Validate(); err != nil {
		return err
	}
	if !IsMarketplaceKind(assessment.Coordinate.Kind) {
		return errors.New("resource assessment coordinate kind is invalid")
	}
	return nil
}

func validatePublicResourceRevisionMatches(rev store.AssessedResourceRevision, expected store.ResourceCoordinate, expectedDigest string) error {
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
