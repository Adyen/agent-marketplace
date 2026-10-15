package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/logging/logvalue"
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
const blobProbeTimeout = 2 * time.Second
const defaultServerVersion = "dev"

type ServerOption func(*Server)

func WithVersion(version string) ServerOption {
	return func(server *Server) {
		if version = strings.TrimSpace(version); version != "" {
			server.version = version
		}
	}
}

// Server serves the Marketplace HTTP API and operations endpoints.
type Server struct {
	cfg           config.Config
	version       string
	meta          store.MetadataStore
	blobs         store.BlobStore
	mpID          store.MarketplaceID
	revocations   *revocation.Evaluator
	revocationErr error
	public        *http.Server
	ops           *http.Server

	metrics *serverMetrics

	installations *installationMetricsReader
}

// PublicHandler returns the public API handler, useful for tests.
func (s *Server) PublicHandler() http.Handler { return s.public.Handler }

// OpsHandler returns the operations handler, useful for tests.
func (s *Server) OpsHandler() http.Handler { return s.ops.Handler }

// NewServer creates an API server for the given Marketplace configuration and
// stores. It does not start listeners; call Start.
func NewServer(cfg config.Config, meta store.MetadataStore, blobs store.BlobStore, options ...ServerOption) *Server {
	evaluator, evaluatorErr := revocation.New(cfg)
	s := &Server{
		cfg:           cfg,
		version:       defaultServerVersion,
		meta:          meta,
		blobs:         blobs,
		mpID:          store.MarketplaceID(cfg.Instance.ID),
		revocations:   evaluator,
		revocationErr: evaluatorErr,
	}
	for _, option := range options {
		option(s)
	}

	s.metrics = newServerMetrics(meta)
	if strings.TrimSpace(cfg.InstallationMetrics.URL) != "" {
		s.installations = newInstallationMetricsReader(cfg.InstallationMetrics, s.mpID)
		s.installations.metrics = s.metrics
	}

	publicMux := http.NewServeMux()
	s.registerAdmin(publicMux)
	publicMux.Handle("/.well-known/agent-marketplace.json", s.methodGet(s.handleWellKnown))
	publicMux.Handle("/api/v1/capabilities", s.methodGet(s.handleCapabilities))
	publicMux.Handle("/api/v1/instance", s.methodGet(s.handleInstance))
	publicMux.Handle("/api/v1/search", s.methodGet(s.handleSearch))
	if cfg.CatalogProjection.Enabled {
		publicMux.Handle("/api/v1/catalog", s.methodGet(s.handleCatalogProjection))
	}
	publicMux.Handle("/api/v1/registries", s.methodGet(s.handleRegistryList))
	publicMux.Handle("/api/v1/registries/", s.methodGet(s.handleRegistries))
	publicMux.Handle("/api/v1/resources/", s.methodGet(s.handleResources))
	publicMux.Handle("/api/v1/manifests/", s.methodGet(s.handleManifests))
	publicMux.Handle("/api/v1/assessments/", s.methodGet(s.handleAssessmentDigest))
	publicMux.Handle("/api/v1/inspection/", s.methodGet(s.handleInspectionDigest))
	publicMux.Handle("/api/v1/content/", s.methodGet(s.handleContentDigest))
	publicMux.Handle("/api/v1/artifacts/", s.methodGet(s.handleArtifacts))
	publicMux.Handle("/api/v1/placeholder-archives/", s.methodGet(s.handlePlaceholderArchives))
	publicMux.Handle("/healthz", s.methodGet(s.handleHealthz))
	// Keep operations endpoints private even when a caller reaches the public
	// listener directly; the operations listener remains the only route.
	publicMux.Handle("/readyz", http.HandlerFunc(s.handlePublicOperationsNotFound))
	publicMux.Handle("/metrics", http.HandlerFunc(s.handlePublicOperationsNotFound))

	opsMux := http.NewServeMux()
	opsMux.Handle("/readyz", s.methodGet(s.handleReadyz))
	opsMux.Handle("/metrics", s.methodGet(s.metrics.scrape.ServeHTTP))

	// Each mux is instrumented as a whole, not per route, so responses the
	// mux itself generates are counted too; see serverMetrics.handler.
	s.public = &http.Server{
		Handler:      s.metrics.publicStack(publicMux, s.authorityHandler(s.availabilityHandler(s.metrics.handler(publicListener, publicMux)))),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	s.ops = &http.Server{
		Handler:      s.metrics.handler(operationsListener, opsMux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Start runs the public and operations listeners until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	if err := s.checkAuthority(ctx); err != nil {
		return fmt.Errorf("start API configuration authority: %w", err)
	}
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

	if s.installations != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.installations.run(ctx, s.registryIndex)
		}()
	}

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
	wk.Version = s.version
	wk.Capabilities = s.capabilities()
	writeJSON(w, http.StatusOK, wk)
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.capabilities())
}

func (s *Server) capabilities() Capabilities {
	caps := DefaultCapabilities()
	if !s.cfg.CatalogProjection.Enabled {
		caps.Features = slices.DeleteFunc(caps.Features, func(feature string) bool {
			return feature == CatalogProjectionV1Alpha1Feature
		})
	}
	return caps
}

const (
	defaultCatalogProjectionLimit = 500
	maxCatalogProjectionLimit     = 500
	maxCatalogCursorBytes         = 4096
	maxIfNoneMatchBytes           = 8192
)

// catalogCursorWire is deliberately a small versioned value rather than a
// signed session token: the cursor is only an ordering hint, has no authority,
// and is validated against the configured Marketplace on every request.
type catalogCursorWire struct {
	Version     int    `json:"v"`
	Marketplace string `json:"m"`
	Registry    string `json:"r"`
	Kind        string `json:"k"`
	Name        string `json:"n"`
}

func (s *Server) handleCatalogProjection(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	continuation := false
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		s.writeCatalogError(w, "invalid catalog query", started, continuation)
		return
	}
	for key := range q {
		if key != "after" && key != "limit" {
			s.writeCatalogError(w, "invalid catalog query parameter", started, continuation)
			return
		}
	}
	limit, err := parseCatalogLimit(q["limit"])
	if err != nil {
		s.writeCatalogError(w, err.Error(), started, continuation)
		return
	}
	after, continuation, err := s.parseCatalogCursor(q["after"])
	if err != nil {
		s.writeCatalogError(w, err.Error(), started, continuation)
		return
	}
	if continuation && r.Header.Get("If-None-Match") != "" {
		s.writeCatalogError(w, "If-None-Match is only accepted on the first page", started, continuation)
		return
	}

	validator := r.Header.Get("If-None-Match")
	view := availabilityFromContext(r.Context())
	if s.cfg.Management.Mode == "database" && view == nil {
		writeError(w, http.StatusServiceUnavailable, "availability unavailable")
		return
	}

	request := store.CatalogProjectionPageRequest{
		Marketplace: s.mpID,
		After:       after,
		Limit:       limit,
	}
	unchanged := func(version store.CatalogVersion) bool {
		matches, err := ifNoneMatchMatches(validator, s.availabilityETag(r.Context(), version))
		return err == nil && matches
	}

	readStarted := time.Now()
	var page store.CatalogProjectionPage
	matched := false
	var readErr error
	conditional, conditionalOK := s.meta.(store.ConditionalCatalogProjectionStore)
	if conditionalOK && conditional.OwnsCatalogProjectionStore(s.meta) &&
		!continuation && validator != "" {
		page, matched, readErr = conditional.ReadCatalogProjectionPageConditional(r.Context(), request, unchanged)
	} else {
		page, readErr = s.meta.ReadCatalogProjectionPage(r.Context(), request)
	}
	s.metrics.observePhase(phaseCatalogRead, readStarted)
	if readErr != nil {
		slog.Error("catalog projection read failed", "continuation", continuation, "error", readErr)
		writeError(w, http.StatusInternalServerError, "failed to read catalog projection")
		return
	}

	if !matched {
		availabilityStarted := time.Now()
		for i := range page.Resources {
			var availability Availability
			if s.cfg.Management.Mode == "database" {
				availability = s.evaluateAvailability(view, page.Resources[i].AssessedResourceRevision).Availability
			} else {
				availability = s.fileResourceAvailability(page.Resources[i].AssessedResourceRevision)
			}
			switch availability {
			case AvailabilityRevoked:
				page.Resources[i].Availability = store.CatalogAvailabilityRevoked
			case AvailabilityAvailable:
				page.Resources[i].Availability = store.CatalogAvailabilityAvailable
			case AvailabilityOperatorExposed:
				page.Resources[i].Availability = store.CatalogAvailabilityOperatorExposed
			default:
				page.Resources[i].Availability = store.CatalogAvailabilityBlocked
			}
		}
		s.metrics.observePhase(phaseCatalogAvailability, availabilityStarted)
	}
	responseStarted := time.Now()
	defer s.metrics.observePhase(phaseCatalogResponse, responseStarted)
	etag := s.availabilityETag(r.Context(), page.CatalogVersion)
	setCatalogHeaders(w, etag)
	if !continuation {
		matches, err := ifNoneMatchMatches(validator, etag)
		if err != nil {
			s.writeCatalogError(w, err.Error(), started, continuation)
			return
		}
		if matched || matches {
			slog.Debug("catalog projection not modified", "catalog_version", page.CatalogVersion, "duration", time.Since(started))
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	response, err := s.catalogPageDTO(page)
	if err != nil {
		slog.Error("catalog projection contained invalid public data", "error", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to read catalog projection")
		return
	}
	slog.Debug("catalog projection read", "catalog_version", page.CatalogVersion, "continuation", continuation, "rows", len(response.Resources), "complete", response.Complete, "duration", time.Since(started))
	writeCatalogJSON(w, r.Method, response)
}

func (s *Server) writeCatalogError(w http.ResponseWriter, message string, started time.Time, continuation bool) {
	slog.Debug("catalog projection rejected request", "continuation", continuation, "error", message, "duration", time.Since(started))
	writeError(w, http.StatusBadRequest, message)
}

func parseCatalogLimit(values []string) (int, error) {
	if len(values) == 0 {
		return defaultCatalogProjectionLimit, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, errors.New("invalid catalog limit")
	}
	for _, c := range values[0] {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid catalog limit")
		}
	}
	value, err := strconv.ParseUint(values[0], 10, 16)
	if err != nil || value == 0 || value > maxCatalogProjectionLimit {
		return 0, errors.New("invalid catalog limit")
	}
	return int(value), nil
}

func (s *Server) parseCatalogCursor(values []string) (*store.CatalogProjectionCursor, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}
	if len(values) != 1 || values[0] == "" || len(values[0]) > maxCatalogCursorBytes {
		return nil, true, errors.New("invalid catalog cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(values[0])
	if err != nil || len(payload) == 0 {
		return nil, true, errors.New("invalid catalog cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor catalogCursorWire
	if err := decoder.Decode(&cursor); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, true, errors.New("invalid catalog cursor")
	}
	coordinate := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID(cursor.Marketplace),
		Registry:    store.RegistryID(cursor.Registry),
		Kind:        cursor.Kind,
		Name:        cursor.Name,
	}
	if cursor.Version != 1 || coordinate.Marketplace != s.mpID || coordinate.Registry == "" ||
		coordinate.Name == "" || !IsMarketplaceKind(coordinate.Kind) {
		return nil, true, errors.New("invalid catalog cursor")
	}
	return &store.CatalogProjectionCursor{Coordinate: coordinate}, true, nil
}

func encodeCatalogCursor(cursor *store.CatalogProjectionCursor) (CatalogKeysetCursor, error) {
	if cursor == nil {
		return "", nil
	}
	coordinate := cursor.Coordinate
	if coordinate.Marketplace == "" || coordinate.Registry == "" || coordinate.Name == "" || !IsMarketplaceKind(coordinate.Kind) {
		return "", errors.New("invalid catalog cursor")
	}
	payload, err := json.Marshal(catalogCursorWire{
		Version: 1, Marketplace: coordinate.Marketplace.String(), Registry: coordinate.Registry.String(),
		Kind: coordinate.Kind, Name: coordinate.Name,
	})
	if err != nil {
		return "", fmt.Errorf("encode catalog cursor: %w", err)
	}
	return CatalogKeysetCursor(base64.RawURLEncoding.EncodeToString(payload)), nil
}

func (s *Server) catalogETag(version store.CatalogVersion) CatalogETag {
	var declarations []store.Revocation
	if s.revocations != nil {
		declarations = s.revocations.Declarations()
	}
	return catalogETag(version, declarations)
}

// catalogETag hashes the catalog version together with the live revocation
// declarations. Declarations are sorted by kind and target ID first so the
// hash depends only on the declaration set, never on the order the source
// returned; otherwise replicas of the same deployment would derive different
// ETags for identical state and lose If-None-Match hits.
func catalogETag(version store.CatalogVersion, declarations []store.Revocation) CatalogETag {
	sorted := append([]store.Revocation(nil), declarations...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Kind != sorted[j].Kind {
			return sorted[i].Kind < sorted[j].Kind
		}
		return sorted[i].TargetID < sorted[j].TargetID
	})
	hash := sha256.New()
	for _, declaration := range sorted {
		_, _ = io.WriteString(hash, string(declaration.Kind))
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, declaration.TargetID)
		_, _ = hash.Write([]byte{0})
	}
	return CatalogETag(fmt.Sprintf(`"catalog-%s-%x"`, version, hash.Sum(nil)))
}

func setCatalogHeaders(w http.ResponseWriter, etag CatalogETag) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", string(etag))
}

func ifNoneMatchMatches(raw string, etag CatalogETag) (bool, error) {
	if raw == "" {
		return false, nil
	}
	if len(raw) > maxIfNoneMatchBytes {
		return false, errors.New("invalid If-None-Match")
	}
	remaining := raw
	matched := false
	for {
		remaining = strings.TrimLeft(remaining, " \t")
		if remaining == "" {
			return false, errors.New("invalid If-None-Match")
		}
		if remaining[0] == '*' {
			if strings.TrimSpace(remaining[1:]) != "" {
				return false, errors.New("invalid If-None-Match")
			}
			return true, nil
		}
		remaining = strings.TrimPrefix(remaining, "W/")
		if len(remaining) < 2 || remaining[0] != '"' {
			return false, errors.New("invalid If-None-Match")
		}
		end := 1
		for end < len(remaining) && remaining[end] != '"' {
			if remaining[end] == '\\' || remaining[end] < 0x21 || remaining[end] == 0x7f {
				return false, errors.New("invalid If-None-Match")
			}
			end++
		}
		if end == len(remaining) {
			return false, errors.New("invalid If-None-Match")
		}
		candidate := remaining[:end+1]
		// If-None-Match uses weak comparison for GET and HEAD. Our generated
		// validator is strong, but a weak form supplied by a compliant client
		// still compares equal.
		if candidate == string(etag) {
			matched = true
		}
		remaining = strings.TrimLeft(remaining[end+1:], " \t")
		if remaining == "" {
			return matched, nil
		}
		if remaining[0] != ',' {
			return false, errors.New("invalid If-None-Match")
		}
		remaining = remaining[1:]
		if strings.TrimSpace(remaining) == "" {
			return false, errors.New("invalid If-None-Match")
		}
	}
}

func (s *Server) catalogPageDTO(page store.CatalogProjectionPage) (CatalogPage, error) {
	resources := make([]CatalogResource, 0, len(page.Resources))
	for _, resource := range page.Resources {
		dto, err := s.catalogResourceDTO(resource)
		if err != nil {
			return CatalogPage{}, err
		}
		resources = append(resources, dto)
	}
	next, err := encodeCatalogCursor(page.Next)
	if err != nil {
		return CatalogPage{}, err
	}
	return CatalogPage{
		APIVersion: CatalogAPIVersionV1Alpha1, MarketplaceID: s.mpID.String(),
		CatalogVersion: page.CatalogVersion.String(), Complete: page.Next == nil,
		NextCursor: next, Resources: resources,
	}, nil
}

func (s *Server) catalogResourceDTO(resource store.CatalogProjectionResource) (CatalogResource, error) {
	rev := resource.AssessedResourceRevision
	if err := validatePublicResourceRevision(rev); err != nil {
		return CatalogResource{}, err
	}
	if rev.Coordinate.Marketplace != s.mpID || resource.RegistrySlug == "" {
		return CatalogResource{}, errors.New("catalog resource identity is invalid")
	}
	availability, err := catalogAvailabilityDTO(resource.Availability)
	if err != nil {
		return CatalogResource{}, err
	}
	rev = s.applyEffectiveRevocation(rev)
	if rev.Revocation != nil {
		availability = AvailabilityRevoked
	}
	out := CatalogResource{
		RegistryID: resource.Coordinate.Registry.String(), RegistrySlug: resource.RegistrySlug,
		Kind: resource.Coordinate.Kind, Name: resource.Coordinate.Name,
		Description: store.BoundDescription(resource.Description), Revision: resource.ContentDigest.String(),
		License: resource.License, Availability: availability,
	}
	if resource.Source != nil {
		source, ok := publicCatalogSource(*resource.Source)
		if ok {
			out.Source = &source
		}
	}
	if resource.Coordinate.Kind == KindCollection {
		collection, err := catalogCollectionDTO(resource.Collection, s.mpID)
		if err != nil {
			return CatalogResource{}, err
		}
		out.Collection = &collection
	}
	return out, nil
}

func catalogAvailabilityDTO(value store.CatalogAvailability) (Availability, error) {
	switch value {
	case store.CatalogAvailabilityAvailable:
		return AvailabilityAvailable, nil
	case store.CatalogAvailabilityBlocked:
		return AvailabilityBlocked, nil
	case store.CatalogAvailabilityOperatorExposed:
		return AvailabilityOperatorExposed, nil
	case store.CatalogAvailabilityRevoked:
		return AvailabilityRevoked, nil
	default:
		return "", errors.New("catalog resource availability is invalid")
	}
}

func publicCatalogSource(source store.CatalogProjectionSource) (CatalogSource, bool) {
	repositoryURL := publicurl.Sanitize(source.RepositoryURL)
	sourcePath, err := store.NormalizeSourcePath(source.Path)
	if publicurl.Validate(repositoryURL) != nil || err != nil || !IsCanonicalGitCommit(source.Commit) {
		return CatalogSource{}, false
	}
	return CatalogSource{RepositoryURL: repositoryURL, Path: sourcePath, Commit: source.Commit}, true
}

// resourceSourceResolver returns a request-scoped resolver for optional source
// provenance, memoizing the registry origin lookup per occurrence commit.
func (s *Server) resourceSourceResolver(ctx context.Context) func(store.AssessedResourceRevision) *CatalogSource {
	type occurrenceKey struct {
		registry store.RegistryID
		commit   string
	}
	origins := make(map[occurrenceKey]string)
	return func(rev store.AssessedResourceRevision) *CatalogSource {
		if rev.Occurrence.SourcePath == "" {
			return nil
		}
		key := occurrenceKey{registry: rev.Coordinate.Registry, commit: rev.Occurrence.Commit}
		origin, cached := origins[key]
		if !cached {
			occurrence, err := s.meta.GetRegistryOccurrence(ctx, key.registry, key.commit)
			switch {
			case errors.Is(err, store.ErrNotFound):
			case err != nil:
				slog.Warn("resource source origin unavailable", "registry", logvalue.Safe(key.registry), "commit", logvalue.Safe(key.commit), "err", logvalue.Safe(err))
			default:
				origin = occurrence.SourceOrigin
			}
			origins[key] = origin
		}
		if origin == "" {
			return nil
		}
		source, ok := publicCatalogSource(store.CatalogProjectionSource{RepositoryURL: origin, Path: rev.Occurrence.SourcePath, Commit: rev.Occurrence.Commit})
		if !ok {
			return nil
		}
		return &source
	}
}

func catalogCollectionDTO(collection *store.CatalogCollectionProjection, marketplace store.MarketplaceID) (CatalogCollection, error) {
	out := CatalogCollection{
		Resources:            make([]CatalogCollectionResourceReference, 0),
		Includes:             make([]CatalogCollectionIncludeReference, 0),
		UnresolvedReferences: make([]CatalogUnresolvedReference, 0),
	}
	if collection == nil {
		return out, nil
	}
	for _, coordinate := range collection.Resources {
		if err := validateCatalogReference(coordinate, marketplace, false); err != nil {
			return CatalogCollection{}, err
		}
		out.Resources = append(out.Resources, CatalogCollectionResourceReference{
			RegistryID: coordinate.Registry.String(), Kind: coordinate.Kind, Name: coordinate.Name,
		})
	}
	for _, coordinate := range collection.Includes {
		if err := validateCatalogReference(coordinate, marketplace, true); err != nil {
			return CatalogCollection{}, err
		}
		out.Includes = append(out.Includes, CatalogCollectionIncludeReference{
			RegistryID: coordinate.Registry.String(), Kind: coordinate.Kind, Name: coordinate.Name,
		})
	}
	for _, unresolved := range collection.UnresolvedReferences {
		referenceType := unresolved.ReferenceType
		switch referenceType {
		case "resource":
		case "include":
		default:
			return CatalogCollection{}, errors.New("catalog unresolved reference type is invalid")
		}
		if err := validateCatalogReference(unresolved.Coordinate, marketplace, referenceType == "include"); err != nil ||
			strings.TrimSpace(unresolved.Reason) == "" {
			return CatalogCollection{}, errors.New("catalog unresolved reference is invalid")
		}
		out.UnresolvedReferences = append(out.UnresolvedReferences, CatalogUnresolvedReference{
			ReferenceType: referenceType, RegistryID: unresolved.Coordinate.Registry.String(),
			Kind: unresolved.Coordinate.Kind, Name: unresolved.Coordinate.Name, Reason: unresolved.Reason,
		})
	}
	return out, nil
}

func validateCatalogReference(coordinate store.ResourceCoordinate, marketplace store.MarketplaceID, mustBeCollection bool) error {
	if coordinate.Marketplace != marketplace || coordinate.Registry == "" || coordinate.Name == "" || !IsMarketplaceKind(coordinate.Kind) {
		return errors.New("catalog collection reference is invalid")
	}
	if mustBeCollection && coordinate.Kind != KindCollection {
		return errors.New("catalog collection include is invalid")
	}
	return nil
}

func writeCatalogJSON(w http.ResponseWriter, method string, page CatalogPage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if method == http.MethodHead {
		return
	}
	if err := json.NewEncoder(w).Encode(page); err != nil {
		slog.Error("failed to encode catalog projection response", "err", logvalue.Safe(err))
	}
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	mp, err := s.meta.GetMarketplace(r.Context(), s.mpID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "marketplace not found")
			return
		}
		slog.Error("failed to load marketplace", "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load marketplace")
		return
	}
	// Marketplace rows may predate public URL validation. Sanitize at the
	// response boundary so legacy canonical URLs cannot leak credentials.
	mp.CanonicalURL = publicurl.Sanitize(mp.CanonicalURL)
	if err := validatePublicMarketplace(mp); err != nil {
		slog.Error("instance contains invalid public metadata", "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load marketplace")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		store.Marketplace
		Version string `json:"version"`
	}{Marketplace: mp, Version: s.version})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "" && !IsMarketplaceKind(kind) {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}
	if sortParam := q.Get("sort"); sortParam != "" && sortParam != "installations" {
		writeError(w, http.StatusBadRequest, "invalid sort")
		return
	}
	sortByInstallations := q.Get("sort") == "installations"

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
		slog.Error("search failed", "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	registries, err := s.registryIndex(r.Context())
	if err != nil {
		slog.Error("registry index failed", "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	for _, rev := range revs {
		if err := validatePublicResourceRevision(rev); err != nil {
			slog.Error("search returned invalid resource revision", "err", logvalue.Safe(err))
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

	var adoption *installationCounts
	if sortByInstallations && s.installations != nil {
		if counts, ok := s.installations.lookup(registries); ok {
			adoption = counts
		} else {
			s.installations.hint()
		}
	}

	sort.Slice(revs, func(i, j int) bool {
		si := relevanceScore(revs[i].Coordinate.Name, searchQ)
		sj := relevanceScore(revs[j].Coordinate.Name, searchQ)
		if si != sj {
			return si > sj
		}
		if adoption != nil {
			ci := adoption.Counts[installationResourceKey{Registry: revs[i].Coordinate.Registry, Kind: revs[i].Coordinate.Kind, Name: revs[i].Coordinate.Name}]
			cj := adoption.Counts[installationResourceKey{Registry: revs[j].Coordinate.Registry, Kind: revs[j].Coordinate.Kind, Name: revs[j].Coordinate.Name}]
			if ci != cj {
				return ci > cj
			}
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
		item := s.resourceToListItem(rev, registries, relevanceScore(rev.Coordinate.Name, searchQ))
		if adoption != nil && adoption.KnownRegistries[rev.Coordinate.Registry] {
			count := adoption.Counts[installationResourceKey{Registry: rev.Coordinate.Registry, Kind: rev.Coordinate.Kind, Name: rev.Coordinate.Name}]
			item.InstallationCount = &count
		}
		if s.cfg.Management.Mode == "database" {
			decision := s.evaluateAvailability(availabilityFromContext(r.Context()), rev)
			if decision.Availability != AvailabilityAvailable {
				item.Availability = decision.Availability
			}
		}
		items = append(items, item)
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
		slog.Error("failed to list registries", "err", logvalue.Safe(err))
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
		slog.Error("failed to load registry", "registry", logvalue.Safe(slugOrID), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load registry")
		return
	}
	// Convert through the public DTO: the store model contains credentials and
	// refresh diagnostics that must never cross the API boundary.
	public := registryDTO(src)
	if err := validatePublicRegistry(public); err != nil {
		slog.Error("registry contains invalid public metadata", "registry", logvalue.Safe(slugOrID), "err", logvalue.Safe(err))
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
		slog.Error("registry occurrence failed", "registry", logvalue.Safe(src.ID), "commit", logvalue.Safe(commit), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load registry occurrence")
		return
	}
	if err := validatePublicRegistryOccurrence(occurrence); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load registry occurrence")
		return
	}
	if occurrence.RegistryID != src.ID || occurrence.Commit != commit {
		slog.Error("registry occurrence did not match requested identity", "registry", logvalue.Safe(src.ID), "commit", logvalue.Safe(commit), "occurrence_registry", logvalue.Safe(occurrence.RegistryID), "occurrence_commit", logvalue.Safe(occurrence.Commit))
		writeError(w, http.StatusInternalServerError, "failed to load registry occurrence")
		return
	}
	if s.cfg.Management.Mode == "database" {
		restriction, err := s.managedRegistryRestriction(r.Context(), src.ID)
		if !writeAvailabilityRestriction(w, restriction, err) {
			return
		}
		if !s.managedDigestAllowed(w, r.Context(), "manifest", store.ArchiveDigest(occurrence.RegistryRevisionManifestDigest)) {
			return
		}
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
		slog.Error("registry history failed", "registry", logvalue.Safe(src.ID), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load registry history")
		return
	}
	history = boundRegistryHistory(history)
	for _, occurrence := range history {
		if err := validatePublicRegistryOccurrence(occurrence); err != nil {
			slog.Error("registry history contains invalid occurrence", "registry", logvalue.Safe(src.ID), "err", logvalue.Safe(err))
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
		slog.Error("resource detail failed", "coord", logvalue.Safe(coord), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load resource")
		return
	}
	if err := validatePublicResourceRevisionMatches(rev, coord, ""); err != nil {
		slog.Error("resource detail contains invalid revision", "coord", logvalue.Safe(coord), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load resource")
		return
	}
	if !s.managedResourceAllowed(w, r.Context(), rev) {
		return
	}
	writeJSON(w, http.StatusOK, s.availabilityResourceDTO(r.Context(), rev, s.resourceSourceResolver(r.Context())))
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
		slog.Error("list revisions failed", "coord", logvalue.Safe(coord), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load revisions")
		return
	}
	for i, rev := range revs {
		if err := validatePublicResourceRevisionMatches(rev, coord, ""); err != nil {
			slog.Error("resource revisions contain invalid revision", "coord", logvalue.Safe(coord), "err", logvalue.Safe(err))
			writeError(w, http.StatusInternalServerError, "failed to load revisions")
			return
		}
		if !s.managedResourceAllowed(w, r.Context(), rev) {
			return
		}
		revs[i] = s.applyEffectiveRevocation(rev)
	}
	items := make([]any, len(revs))
	resolveSource := s.resourceSourceResolver(r.Context())
	for i, rev := range revs {
		items[i] = s.availabilityResourceDTO(r.Context(), rev, resolveSource)
	}
	writeJSON(w, http.StatusOK, items)
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
		slog.Error("revision detail failed", "coord", logvalue.Safe(coord), "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load revision")
		return
	}
	if err := validatePublicResourceRevisionMatches(rev, coord, digest); err != nil {
		slog.Error("revision detail contains invalid revision", "coord", logvalue.Safe(coord), "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to load revision")
		return
	}
	if !s.managedResourceAllowed(w, r.Context(), rev) {
		return
	}
	writeJSON(w, http.StatusOK, s.availabilityResourceDTO(r.Context(), rev, s.resourceSourceResolver(r.Context())))
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
		slog.Error("inspection content fetch failed", "digest", file.ContentDigest, "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch inspection content")
		return
	}
	if s.cfg.Policy.Limits.FileBytes > 0 && int64(len(data)) > s.cfg.Policy.Limits.FileBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "inspection content exceeds size limit")
		return
	}
	if err := inspection.VerifyFileContent(*file, data); err != nil {
		slog.Error("inspection content does not match manifest", "digest", file.ContentDigest, "err", logvalue.Safe(err))
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
	if s.cfg.Management.Mode == "database" {
		restriction, err := s.managedOwnerRestriction(ctx, []store.AssessedResourceRevision{rev})
		if err != nil {
			return inspection.Manifest{}, nil, err
		}
		if restriction == AvailabilityRevoked {
			return inspection.Manifest{}, nil, errContentRevoked
		}
		if restriction != AvailabilityAvailable {
			return inspection.Manifest{}, nil, errContentBlocked
		}
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
	if errors.Is(err, errContentRevoked) {
		writeError(w, http.StatusGone, "content unavailable")
		return
	}
	if errors.Is(err, errContentBlocked) {
		writeError(w, http.StatusForbidden, "content unavailable")
		return
	}
	if errors.Is(err, errAvailabilityUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "availability unavailable")
		return
	}
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, errInspectionUnavailable) {
		writeError(w, http.StatusNotFound, "inspection not found")
		return
	}
	if errors.Is(err, errInvalidInspectionRevision) {
		writeError(w, http.StatusBadRequest, "invalid inspection revision")
		return
	}
	slog.Error("inspection request failed", "err", logvalue.Safe(err))
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
		slog.Error("assessment fetch failed", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch assessment")
		return
	}
	if assessment.AssessmentDigest != store.AssessmentDigest(digest) || assessment.Coordinate.Marketplace != s.mpID || validatePublicResourceAssessment(assessment) != nil {
		slog.Error("assessment contains invalid public metadata", "digest", logvalue.Safe(digest))
		writeError(w, http.StatusInternalServerError, "failed to fetch assessment")
		return
	}
	payload, err := json.Marshal(assessment)
	if err != nil {
		slog.Error("assessment encoding failed", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
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
	if !s.managedDigestAllowed(w, r.Context(), "inspection", store.ArchiveDigest(digest)) {
		return
	}
	data, err := s.blobs.Get(r.Context(), store.ArchiveDigest(digest))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "inspection not found")
			return
		}
		slog.Error("inspection manifest fetch failed", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
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
	if !s.managedDigestAllowed(w, r.Context(), "content", store.ArchiveDigest(digest)) {
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
	if !s.managedDigestAllowed(w, r.Context(), "manifest", store.ArchiveDigest(digest)) {
		return
	}

	data, err := s.blobs.Get(r.Context(), store.ArchiveDigest(digest))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "manifest not found")
			return
		}
		slog.Error("manifest fetch failed", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
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
		slog.Error("manifest blob is invalid", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch manifest")
		return
	}
	restriction, err := s.manifestRestriction(r.Context(), store.RevisionDigest(digest), data)
	if err != nil {
		slog.Error("manifest ownership lookup failed", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
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

const artifactMediaType = "application/vnd.agent-marketplace.artifact.v1.tar+gzip"

// archiveDigestFromPath parses the sha256 archive digest that ends a route path.
func archiveDigestFromPath(path, prefix string) (store.ArchiveDigest, bool) {
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 2 || parts[0] != "sha256" || !isLowerHexDigest(parts[1]) {
		return "", false
	}
	return store.ArchiveDigest("sha256:" + parts[1]), true
}

// writeArchiveAvailability refuses archives no owned revision serves. It reports
// whether the archive may be served; unowned digests are refused before blob
// storage is touched, so an orphaned blob at the address is never returned.
func (s *Server) writeArchiveAvailability(w http.ResponseWriter, r *http.Request, digest store.ArchiveDigest) bool {
	revisions, err := s.meta.ListResourceRevisionsByArchiveDigest(r.Context(), s.mpID, digest)
	if err != nil {
		slog.Error("artifact ownership lookup failed", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch artifact")
		return false
	}
	if len(revisions) == 0 {
		writeError(w, http.StatusNotFound, "artifact not found")
		return false
	}
	restriction, err := s.managedOwnerRestriction(r.Context(), revisions)
	if err != nil {
		writeAvailabilityRestriction(w, restriction, err)
		return false
	}
	switch restriction {
	case AvailabilityRevoked:
		writeError(w, http.StatusGone, "artifact is revoked")
		return false
	case AvailabilityBlocked:
		writeError(w, http.StatusForbidden, "artifact is blocked by assessment")
		return false
	}
	return true
}

// archiveWithinLimit reports whether data fits the configured artifact size limit,
// writing the refusal when it does not.
func (s *Server) archiveWithinLimit(w http.ResponseWriter, data []byte) bool {
	artifactLimit := s.cfg.Policy.Limits.ArtifactBytes
	if artifactLimit <= 0 {
		artifactLimit = config.DefaultArtifactBytes
	}
	if int64(len(data)) > artifactLimit {
		writeError(w, http.StatusRequestEntityTooLarge, "artifact exceeds size limit")
		return false
	}
	return true
}

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	digest, ok := archiveDigestFromPath(r.URL.Path, "/api/v1/artifacts/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid artifact digest")
		return
	}
	if !s.writeArchiveAvailability(w, r, digest) {
		return
	}
	data, err := s.blobs.Get(r.Context(), digest)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "artifact not found")
			return
		}
		slog.Error("artifact fetch failed", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch artifact")
		return
	}
	if !s.archiveWithinLimit(w, data) {
		return
	}
	if err := s.verifyFinalArchive(r.Context(), digest, data); err != nil {
		slog.Error("artifact blob digest mismatch", "digest", logvalue.Safe(digest), "error", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "artifact digest mismatch")
		return
	}
	writeImmutable(w, artifactMediaType, data)
}

// verifyFinalArchive checks a stored final archive against its digest. When the
// placeholder-form archive is stored, the final bytes are checked by content
// against it and never re-compressed; otherwise the archive is re-encoded.
func (s *Server) verifyFinalArchive(ctx context.Context, digest store.ArchiveDigest, data []byte) error {
	placeholder, err := s.blobs.Get(ctx, store.PlaceholderArchiveKey(digest))
	if err == nil {
		verified, err := hccartifact.VerifyPlaceholderArchive(placeholder, string(digest))
		if err != nil {
			return err
		}
		_, err = hccartifact.VerifyFinalAgainstPlaceholder(data, verified)
		return err
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	artifact, rawArchive, err := hccartifact.Decode(bytes.NewReader(data))
	if err == nil && artifact.Manifest.ArchiveDigest != string(digest) {
		err = fmt.Errorf("manifest archive digest %q does not match requested %q", artifact.Manifest.ArchiveDigest, digest)
	}
	if err == nil {
		err = artifact.VerifyArchiveDigest(rawArchive)
	}
	return err
}

func (s *Server) handlePlaceholderArchives(w http.ResponseWriter, r *http.Request) {
	digest, ok := archiveDigestFromPath(r.URL.Path, "/api/v1/placeholder-archives/")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid placeholder archive digest")
		return
	}
	if !s.writeArchiveAvailability(w, r, digest) {
		return
	}
	data, err := s.placeholderArchive(r.Context(), digest)
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, hccartifact.ErrPlaceholderNotReproducible):
		writeError(w, http.StatusNotFound, "placeholder archive not available")
		return
	case err != nil:
		slog.Error("placeholder archive fetch failed", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "failed to fetch placeholder archive")
		return
	}
	if _, err := hccartifact.VerifyPlaceholderArchive(data, string(digest)); err != nil {
		slog.Error("placeholder archive digest mismatch", "digest", logvalue.Safe(digest), "error", logvalue.Safe(err))
		writeError(w, http.StatusInternalServerError, "placeholder archive digest mismatch")
		return
	}
	if !s.archiveWithinLimit(w, data) {
		return
	}
	writeImmutable(w, artifactMediaType, data)
}

// placeholderArchive returns the stored placeholder-form archive for digest. A
// missing one is derived from the final archive and stored only when the derived
// bytes hash to digest; otherwise it returns ErrPlaceholderNotReproducible.
func (s *Server) placeholderArchive(ctx context.Context, digest store.ArchiveDigest) ([]byte, error) {
	data, err := s.blobs.Get(ctx, store.PlaceholderArchiveKey(digest))
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	final, err := s.blobs.Get(ctx, digest)
	if err != nil {
		return nil, err
	}
	derived, err := hccartifact.DerivePlaceholderArchive(final, string(digest))
	if err != nil {
		return nil, err
	}
	// Storing the derived copy is best effort: the caller verifies the bytes by hash
	// before serving them, so a read-only or failing blob store must not fail the request.
	if err := store.PutImmutableBlob(ctx, s.blobs, store.PlaceholderArchiveKey(digest), derived); err != nil {
		slog.Warn("derived placeholder archive could not be stored; serving it unstored", "digest", logvalue.Safe(digest), "err", logvalue.Safe(err))
	}
	return derived, nil
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
	if err := s.checkAuthority(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "configuration authority unavailable")
		return
	}
	ctx := r.Context()
	if err := s.revocationsReady(ctx); err != nil {
		slog.Error("readyz failed: live revocations are not reconciled", "err", logvalue.Safe(err))
		writeError(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	if _, err := s.meta.GetMarketplace(ctx, s.mpID); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, blobProbeTimeout)
	defer cancel()
	if err := s.blobs.Probe(probeCtx); err != nil {
		slog.Error("readyz failed: blob store unavailable", "err", logvalue.Safe(err))
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
	if len(revisions) == 0 {
		return AvailabilityAvailable
	}
	allowed := false
	for _, revision := range revisions {
		availability := s.fileResourceAvailability(revision)
		if availability == AvailabilityRevoked {
			return AvailabilityRevoked
		}
		if availability == AvailabilityAvailable || availability == AvailabilityOperatorExposed {
			allowed = true
		}
	}
	if allowed {
		return AvailabilityAvailable
	}
	return AvailabilityBlocked
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
	if s.cfg.Management.Mode == "database" && len(owners) > 0 {
		restriction, err := s.managedOwnerRestriction(ctx, owners)
		if err != nil || restriction != AvailabilityAvailable {
			return restriction, err
		}
	} else if restriction := s.resourceOwnerRestriction(owners); restriction != AvailabilityAvailable {
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
func resourceAvailability(rev store.AssessedResourceRevision, registry store.Registry, rejectedResources string) Availability {
	if rev.Revocation != nil {
		return AvailabilityRevoked
	}
	if registry.Acquisition == store.AcquisitionMirror && rev.Coordinate.Kind != KindCollection && rev.ArchiveDigest == "" {
		slog.Debug("listing resource without a mirrored artifact as blocked",
			"coordinate", rev.Coordinate,
			"content_digest", rev.ContentDigest,
		)
		return AvailabilityNoArtifact
	}
	if rev.Assessment.Decision == store.AssessmentDecisionReject {
		if rejectedResources == config.RejectedResourcesExpose {
			return AvailabilityOperatorExposed
		}
		return AvailabilityPolicyBlocked
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
		Availability: resourceAvailability(rev, registry, rejectedResourcesForOverrides(s.cfg.Policy, registry.PolicyOverrides)),
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
		slog.Error("failed to encode json response", "err", logvalue.Safe(err))
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
