package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/publicurl"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

const maxArtifactBytes = 64 * 1024 * 1024

const (
	maxAPIErrorBytes       = 4 * 1024
	maxAPIErrorOutputBytes = 4 * 1024
)

const defaultTimeout = 30 * time.Second

type APIError struct {
	StatusCode int
	Detail     string
}

func (e *APIError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("marketplace error %d", e.StatusCode)
	}
	return fmt.Sprintf("marketplace error %d: %s", e.StatusCode, e.Detail)
}

// Client is a thin HTTP client for the public Marketplace API.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string

	identityMu  sync.RWMutex
	instanceID  store.MarketplaceID
	registryIDs map[string]store.RegistryID
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithUserAgent sets the User-Agent request header.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithTimeout sets the timeout on the default HTTP client. Ignored if WithHTTPClient is used.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if c.httpClient == nil {
			c.httpClient = &http.Client{Timeout: d}
		} else {
			c.httpClient.Timeout = d
		}
	}
}

// NewClient creates a Marketplace API client for the given base URL. The base
// URL should be the scheme/host of the Marketplace instance; API paths are
// resolved relative to it.
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("marketplace base URL is required")
	}

	normalized, err := urlutil.NormalizeEndpointURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid marketplace base URL %q: %w", baseURL, err)
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("invalid marketplace base URL %q: %w", baseURL, err)
	}

	c := &Client{
		baseURL:     u,
		httpClient:  &http.Client{Timeout: defaultTimeout},
		userAgent:   "agent-marketplace",
		registryIDs: make(map[string]store.RegistryID),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return c, nil
}

// SearchRequest is the query for the Marketplace search endpoint.
type SearchRequest struct {
	Query  string
	Kind   string
	Limit  int
	Offset int
}

// SearchResponse is the envelope returned by /api/v1/search.
type SearchResponse struct {
	Items  []mpapi.ResourceListItem `json:"items"`
	Total  int                      `json:"total"`
	Offset int                      `json:"offset"`
	Limit  int                      `json:"limit"`
}

// UnmarshalJSON requires the fields that make a search response an envelope.
// Using pointers here distinguishes omitted and null fields from valid zero
// values; ordinary unmarshalling would silently turn both into zero values.
func (r *SearchResponse) UnmarshalJSON(data []byte) error {
	var wire struct {
		Items  *[]mpapi.ResourceListItem `json:"items"`
		Total  *int                      `json:"total"`
		Offset *int                      `json:"offset"`
		Limit  *int                      `json:"limit"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Items == nil || wire.Total == nil || wire.Offset == nil || wire.Limit == nil {
		return fmt.Errorf("search response envelope is incomplete")
	}
	*r = SearchResponse{Items: *wire.Items, Total: *wire.Total, Offset: *wire.Offset, Limit: *wire.Limit}
	return nil
}

// WellKnown fetches the well-known document that maps the Marketplace canonical
// URL to its API base and capabilities.
func (c *Client) WellKnown(ctx context.Context) (mpapi.WellKnown, error) {
	var wk mpapi.WellKnown
	if err := c.getJSON(ctx, c.requestURL([]string{".well-known", "agent-marketplace.json"}, nil), &wk); err != nil {
		return wk, err
	}
	if err := validateAPIWellKnown(wk); err != nil {
		return wk, fmt.Errorf("invalid Marketplace well-known document: %w", err)
	}
	c.identityMu.Lock()
	c.instanceID = store.MarketplaceID(wk.InstanceID)
	c.identityMu.Unlock()
	return wk, nil
}

// Search queries the Marketplace search endpoint.
func (c *Client) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	var sr SearchResponse

	q := url.Values{}
	if req.Query != "" {
		q.Set("q", req.Query)
	}
	if req.Kind != "" {
		q.Set("kind", req.Kind)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset > 0 {
		q.Set("offset", strconv.Itoa(req.Offset))
	}

	if err := c.getJSON(ctx, c.requestURL([]string{"api", "v1", "search"}, q), &sr); err != nil {
		return sr, err
	}
	if err := validateAPISearchResponse(sr, req); err != nil {
		return sr, fmt.Errorf("invalid Marketplace search response: %w", err)
	}
	return sr, nil
}

func (c *Client) getJSON(ctx context.Context, requestURL string, dst any) error {
	return c.getJSONMode(ctx, requestURL, dst, false)
}

func (c *Client) getJSONStrict(ctx context.Context, requestURL string, dst any) error {
	return c.getJSONMode(ctx, requestURL, dst, true)
}

func (c *Client) getJSONMode(ctx context.Context, requestURL string, dst any, strict bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("marketplace request %s: %w", requestURL, err)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("marketplace request %s: %w", req.URL.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	limit := int64(maxArtifactBytes)
	if resp.StatusCode != http.StatusOK {
		limit = maxAPIErrorBytes
	}
	body, err := readResponseBody(resp, limit)
	if err != nil {
		return fmt.Errorf("marketplace read %s: %w", req.URL.Redacted(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return apiError(resp.StatusCode, body)
	}

	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		return fmt.Errorf("marketplace decode %s: null response", req.URL.Redacted())
	}
	if strict {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(dst); err != nil {
			return fmt.Errorf("marketplace decode %s: %w", req.URL.Redacted(), err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			if err == nil {
				return fmt.Errorf("marketplace decode %s: trailing JSON value", req.URL.Redacted())
			}
			return fmt.Errorf("marketplace decode %s: %w", req.URL.Redacted(), err)
		}
	} else if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("marketplace decode %s: %w", req.URL.Redacted(), err)
	}
	return nil
}

// GetResourceRevision fetches a specific revision of a resource.
func (c *Client) GetResourceRevision(ctx context.Context, registry, kind, name, revision string) (store.AssessedResourceRevision, error) {
	return c.getResourceRevision(ctx, registry, registry, kind, name, revision)
}

// GetResourceRevisionForRegistry keeps a slug in the route while validating the
// immutable Registry ID returned by the Marketplace.
func (c *Client) GetResourceRevisionForRegistry(ctx context.Context, routeRegistry, registryID, kind, name, revision string) (store.AssessedResourceRevision, error) {
	return c.getResourceRevision(ctx, routeRegistry, registryID, kind, name, revision)
}

func (c *Client) getResourceRevision(ctx context.Context, routeRegistry, expectedRegistry, kind, name, revision string) (store.AssessedResourceRevision, error) {
	var response struct {
		store.AssessedResourceRevision
		CurrentAvailability *struct {
			Availability mpapi.Availability `json:"availability"`
		} `json:"current_availability,omitempty"`
	}
	var rev store.AssessedResourceRevision
	if err := validateSHA256Digest(revision); err != nil {
		return rev, fmt.Errorf("revision digest %q: %w", revision, err)
	}
	segments := []string{"api", "v1", "resources", routeRegistry, kind, name, "revisions", revision}
	if err := c.getJSON(ctx, c.requestURL(segments, nil), &response); err != nil {
		return rev, err
	}
	rev = response.AssessedResourceRevision
	if response.CurrentAvailability != nil {
		rev.EffectiveAvailability = string(response.CurrentAvailability.Availability)
	}
	if err := validateAPIResourceRevisionMatches(rev, c.expectedResourceCoordinate(expectedRegistry, kind, name), store.RevisionDigest(revision)); err != nil {
		return rev, fmt.Errorf("invalid Marketplace resource revision: %w", err)
	}
	return rev, nil
}

type InspectionContent struct {
	Manifest    inspection.Manifest
	File        inspection.File
	ContentType string
	Data        []byte
}

func (c *Client) GetResourceInspection(ctx context.Context, registry, kind, name, revision string) (inspection.Manifest, error) {
	manifest, _, err := c.getResourceInspection(ctx, registry, kind, name, revision)
	return manifest, err
}

func (c *Client) GetResourceInspectionManifest(ctx context.Context, registry, kind, name, revision string) (inspection.Manifest, error) {
	return c.GetResourceInspection(ctx, registry, kind, name, revision)
}

func (c *Client) GetResourceInspectionPrimary(ctx context.Context, registry, kind, name, revision string) (InspectionContent, error) {
	return c.getResourceInspectionContent(ctx, registry, kind, name, revision, "")
}

func (c *Client) GetResourceInspectionFile(ctx context.Context, registry, kind, name, revision, fileDigest string) (InspectionContent, error) {
	if err := validateSHA256Digest(fileDigest); err != nil {
		return InspectionContent{}, fmt.Errorf("inspection file digest %q: %w", fileDigest, err)
	}
	return c.getResourceInspectionContent(ctx, registry, kind, name, revision, fileDigest)
}

func (c *Client) getResourceInspection(ctx context.Context, registry, kind, name, revision string) (inspection.Manifest, store.AssessedResourceRevision, error) {
	var manifest inspection.Manifest
	if !mpapi.IsMarketplaceKind(kind) {
		return manifest, store.AssessedResourceRevision{}, fmt.Errorf("inspection kind %q is invalid", kind)
	}
	if strings.TrimSpace(registry) == "" || strings.TrimSpace(name) == "" {
		return manifest, store.AssessedResourceRevision{}, fmt.Errorf("inspection coordinate is incomplete")
	}
	rev, err := c.getResourceRevision(ctx, registry, "", kind, name, revision)
	if err != nil {
		return manifest, rev, err
	}
	if rev.Assessment.InspectionManifestDigest == "" {
		return manifest, rev, fmt.Errorf("resource revision has no inspection manifest assessment")
	}
	manifestDigest := string(rev.Assessment.InspectionManifestDigest)
	if err := validateSHA256Digest(manifestDigest); err != nil {
		return manifest, rev, fmt.Errorf("inspection manifest provenance: %w", err)
	}
	raw, err := c.getRawLimited(ctx, c.requestURL([]string{"api", "v1", "resources", registry, kind, name, "revisions", revision, "inspection"}, nil), inspection.MaxManifestBytes)
	if err != nil {
		return manifest, rev, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, rev, fmt.Errorf("decode inspection manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return manifest, rev, fmt.Errorf("inspection manifest has trailing JSON")
		}
		return manifest, rev, fmt.Errorf("decode inspection manifest: %w", err)
	}
	if err := inspection.Verify(manifest, raw, manifestDigest); err != nil {
		return manifest, rev, fmt.Errorf("verify inspection manifest: %w", err)
	}
	manifest.ManifestDigest = manifestDigest
	if manifest.Coordinate != rev.Coordinate || manifest.RevisionDigest != string(rev.ContentDigest) {
		return manifest, rev, fmt.Errorf("inspection manifest identity does not match resource revision")
	}
	return manifest, rev, nil
}

func (c *Client) getResourceInspectionContent(ctx context.Context, registry, kind, name, revision, fileDigest string) (InspectionContent, error) {
	manifest, _, err := c.getResourceInspection(ctx, registry, kind, name, revision)
	if err != nil {
		return InspectionContent{}, err
	}
	var file *inspection.File
	for i := range manifest.Files {
		if (fileDigest == "" && manifest.Files[i].Path == manifest.PrimaryPath) || manifest.Files[i].ContentDigest == fileDigest {
			file = &manifest.Files[i]
			break
		}
	}
	if file == nil || file.InspectionClass == inspection.ClassMetadata {
		return InspectionContent{}, fmt.Errorf("inspection content %q is unavailable", fileDigest)
	}
	if file.Size < 0 || file.Size > maxArtifactBytes {
		return InspectionContent{}, fmt.Errorf("inspection content size %d exceeds client limit", file.Size)
	}
	segments := []string{"api", "v1", "resources", registry, kind, name, "revisions", revision, "inspection", "content"}
	if fileDigest != "" {
		segments = append(segments, fileDigest)
	}
	data, headers, err := c.getRawResponseLimit(ctx, c.requestURL(segments, nil), file.Size)
	if err != nil {
		return InspectionContent{}, err
	}
	contentType := strings.TrimSpace(strings.Split(headers.Get("Content-Type"), ";")[0])
	if contentType != file.MediaType {
		return InspectionContent{}, fmt.Errorf("inspection content type %q does not match manifest %q", contentType, file.MediaType)
	}
	if err := inspection.VerifyFileContent(*file, data); err != nil {
		return InspectionContent{}, fmt.Errorf("verify inspection content: %w", err)
	}
	return InspectionContent{Manifest: manifest, File: *file, ContentType: contentType, Data: data}, nil
}

// GetLatestResourceRevision fetches the latest revision of a resource.
func (c *Client) GetLatestResourceRevision(ctx context.Context, registry, kind, name string) (store.AssessedResourceRevision, error) {
	return c.getLatestResourceRevision(ctx, registry, registry, kind, name)
}

// GetLatestResourceRevisionForRegistry keeps a slug in the route while validating
// the immutable Registry ID returned by the Marketplace.
func (c *Client) GetLatestResourceRevisionForRegistry(ctx context.Context, routeRegistry, registryID, kind, name string) (store.AssessedResourceRevision, error) {
	return c.getLatestResourceRevision(ctx, routeRegistry, registryID, kind, name)
}

func (c *Client) getLatestResourceRevision(ctx context.Context, routeRegistry, expectedRegistry, kind, name string) (store.AssessedResourceRevision, error) {
	var response struct {
		store.AssessedResourceRevision
		CurrentAvailability *struct {
			Availability mpapi.Availability `json:"availability"`
		} `json:"current_availability,omitempty"`
	}
	var rev store.AssessedResourceRevision
	segments := []string{"api", "v1", "resources", routeRegistry, kind, name}
	if err := c.getJSON(ctx, c.requestURL(segments, nil), &response); err != nil {
		return rev, err
	}
	rev = response.AssessedResourceRevision
	if response.CurrentAvailability != nil {
		rev.EffectiveAvailability = string(response.CurrentAvailability.Availability)
	}
	if err := validateAPIResourceRevisionMatches(rev, c.expectedResourceCoordinate(expectedRegistry, kind, name), ""); err != nil {
		return rev, fmt.Errorf("invalid Marketplace resource revision: %w", err)
	}
	return rev, nil
}

// GetRegistry fetches registry metadata addressed by its immutable Registry ID. The
// response ID is checked before any metadata is remembered or returned so a
// route/proxy mix-up cannot retarget a pin.
func (c *Client) GetRegistry(ctx context.Context, registryID string) (store.Registry, error) {
	if strings.TrimSpace(registryID) == "" {
		return store.Registry{}, fmt.Errorf("registry ID is required")
	}
	return c.getRegistry(ctx, registryID, registryID)
}

// GetRegistryBySlug fetches registry metadata through a user-facing slug or alias.
// A slug is not an immutable identity, so callers must use the returned ID for
// subsequent exact-coordinate requests.
func (c *Client) GetRegistryBySlug(ctx context.Context, slug string) (store.Registry, error) {
	return c.getRegistry(ctx, slug, "")
}

func (c *Client) getRegistry(ctx context.Context, routeRegistry, expectedRegistryID string) (store.Registry, error) {
	var public mpapi.PublicRegistry
	if err := c.getJSONStrict(ctx, c.requestURL([]string{"api", "v1", "registries", routeRegistry}, nil), &public); err != nil {
		return store.Registry{}, err
	}
	if err := validateAPIRegistry(public); err != nil {
		return store.Registry{}, fmt.Errorf("invalid Marketplace registry: %w", err)
	}
	if expectedRegistryID != "" && public.ID != store.RegistryID(expectedRegistryID) {
		return store.Registry{}, fmt.Errorf("registry ID %q does not match requested %q", public.ID, expectedRegistryID)
	}
	c.rememberRegistry(routeRegistry, public)

	registry := store.Registry{
		ID: public.ID, Slug: public.Slug, Aliases: public.Aliases,
		Origins: public.Origins, Ref: public.Ref, Roots: public.Roots,
		Acquisition: public.Acquisition, License: public.License, Listed: public.Listed,
	}
	if public.Publisher != nil {
		registry.Publisher = store.Publisher{Name: public.Publisher.Name, URL: public.Publisher.URL}
	}
	return registry, nil
}

// GetRegistryOccurrence fetches the immutable occurrence for one exact commit and
// the registry's current operator-approved acquisition origin.
func (c *Client) GetRegistryOccurrence(ctx context.Context, registryID, commit string) (mpapi.ExactRegistryOccurrence, error) {
	var occurrence mpapi.ExactRegistryOccurrence
	if strings.TrimSpace(registryID) == "" {
		return occurrence, fmt.Errorf("registry occurrence registry ID is required")
	}
	if !mpapi.IsCanonicalGitCommit(commit) {
		return occurrence, fmt.Errorf("registry occurrence commit %q is not canonical", commit)
	}
	segments := []string{"api", "v1", "registries", registryID, "occurrences", commit}
	if err := c.getJSONStrict(ctx, c.requestURL(segments, nil), &occurrence); err != nil {
		return occurrence, err
	}
	// The route accepts either the current registry slug or immutable Registry ID,
	// but a response for a different registry must never be usable for this pin.
	if err := validateAPIExactRegistryOccurrence(occurrence, store.RegistryID(registryID), commit); err != nil {
		return occurrence, fmt.Errorf("invalid Marketplace registry occurrence: %w", err)
	}
	return occurrence, nil
}

// GetCollectionManifest downloads and verifies a standalone canonical
// collection manifest by its content digest. The manifest is not a tar
// artifact: collections remain virtual and only their leaf artifacts are
// materialized.
func (c *Client) GetCollectionManifest(ctx context.Context, manifestDigest string) (hccartifact.CollectionManifest, []byte, error) {
	var manifest hccartifact.CollectionManifest
	if err := validateSHA256Digest(manifestDigest); err != nil {
		return manifest, nil, fmt.Errorf("manifest digest %q: %w", manifestDigest, err)
	}
	raw, err := c.getRaw(ctx, c.requestURL([]string{"api", "v1", "manifests", manifestDigest}, nil))
	if err != nil {
		return manifest, nil, err
	}
	decoded, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(raw))
	if err != nil {
		return manifest, nil, fmt.Errorf("decode collection manifest: %w", err)
	}
	if err := hccartifact.VerifyCollectionManifest(decoded, payload); err != nil {
		return manifest, nil, fmt.Errorf("verify collection manifest: %w", err)
	}
	if err := hccartifact.ValidateCollectionManifest(decoded); err != nil {
		return manifest, nil, fmt.Errorf("validate collection manifest: %w", err)
	}
	if decoded.ContentDigest != manifestDigest {
		return manifest, nil, fmt.Errorf("collection manifest digest mismatch: got %s, want %s", decoded.ContentDigest, manifestDigest)
	}
	return decoded, payload, nil
}

// GetArtifact downloads an artifact archive by its archive digest.
func (c *Client) GetArtifact(ctx context.Context, archiveDigest string) ([]byte, error) {
	return c.getArtifactBytes(ctx, "artifacts", archiveDigest)
}

// GetArtifactProof downloads the exact placeholder-form archive whose bytes
// hash to the existing v1 archive digest.
func (c *Client) GetArtifactProof(ctx context.Context, archiveDigest string) ([]byte, error) {
	return c.getArtifactBytes(ctx, "artifact-proofs", archiveDigest)
}

func (c *Client) getArtifactBytes(ctx context.Context, route, archiveDigest string) ([]byte, error) {
	if err := validateSHA256Digest(archiveDigest); err != nil {
		return nil, fmt.Errorf("archive digest %q: %w", archiveDigest, err)
	}
	hexDigest := strings.TrimPrefix(archiveDigest, "sha256:")
	path := c.requestURL([]string{"api", "v1", route, "sha256", hexDigest}, nil)
	return c.getRaw(ctx, path)
}

// getRaw performs a GET and returns the response body bytes for non-JSON endpoints.
func (c *Client) getRaw(ctx context.Context, requestURL string) ([]byte, error) {
	body, _, err := c.getRawResponse(ctx, requestURL)
	return body, err
}

func (c *Client) getRawResponse(ctx context.Context, requestURL string) ([]byte, http.Header, error) {
	return c.getRawResponseLimit(ctx, requestURL, maxArtifactBytes)
}

func (c *Client) getRawLimited(ctx context.Context, requestURL string, limit int64) ([]byte, error) {
	body, _, err := c.getRawResponseLimit(ctx, requestURL, limit)
	return body, err
}

func (c *Client) getRawResponseLimit(ctx context.Context, requestURL string, limit int64) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("marketplace request %s: %w", requestURL, err)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("marketplace request %s: %w", req.URL.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	responseLimit := limit
	if resp.StatusCode != http.StatusOK {
		responseLimit = maxAPIErrorBytes
	}
	body, err := readResponseBody(resp, responseLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("marketplace read %s: %w", req.URL.Redacted(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, apiError(resp.StatusCode, body)
	}
	return body, resp.Header.Clone(), nil
}

func (c *Client) requestURL(segments []string, query url.Values) string {
	u := *c.baseURL
	escapedPath := strings.TrimSuffix(c.baseURL.EscapedPath(), "/")
	for _, segment := range segments {
		escapedPath += "/" + url.PathEscape(segment)
	}
	path, _ := url.PathUnescape(escapedPath)
	u.Path = path
	u.RawPath = escapedPath
	u.RawQuery = query.Encode()
	u.Fragment = ""
	return u.String()
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	hc := *c.httpClient
	originalURL := *req.URL
	userCheckRedirect := hc.CheckRedirect
	hc.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if err := validateRedirectTarget(&originalURL, next); err != nil {
			return err
		}
		if userCheckRedirect != nil {
			if err := userCheckRedirect(next, via); err != nil {
				return err
			}
		}
		return validateRedirectTarget(&originalURL, next)
	}
	return hc.Do(req)
}

func validateRedirectTarget(original *url.URL, next *http.Request) error {
	if next == nil || next.URL == nil {
		return fmt.Errorf("redirect target is missing")
	}
	if next.URL.User != nil || !sameOrigin(original, next.URL) {
		return fmt.Errorf("redirect origin is not allowed: %s", next.URL.Redacted())
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func readResponseBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return body, nil
}

func validateAPIWellKnown(wk mpapi.WellKnown) error {
	if strings.TrimSpace(wk.InstanceID) == "" || strings.TrimSpace(wk.CanonicalURL) == "" || strings.TrimSpace(wk.APIBase) == "" {
		return fmt.Errorf("well-known identity fields are incomplete")
	}
	if err := publicurl.Validate(wk.CanonicalURL); err != nil {
		return fmt.Errorf("canonical URL: %w", err)
	}
	browserFields := []string{wk.BrowserBase, wk.BrowserLatestRoute, wk.BrowserExactRoute}
	if slices.ContainsFunc(browserFields, func(value string) bool { return strings.TrimSpace(value) != "" }) {
		if slices.ContainsFunc(browserFields, func(value string) bool { return strings.TrimSpace(value) == "" }) {
			return fmt.Errorf("well-known browser fields are incomplete")
		}
		if err := publicurl.Validate(wk.BrowserBase); err != nil {
			return fmt.Errorf("browser base: %w", err)
		}
		if wk.BrowserLatestRoute != wk.BrowserBase+"/r/{registry}/{kind}/{name}" || wk.BrowserExactRoute != wk.BrowserBase+"/r/{registry}/{kind}/{name}/revisions/{revision}" {
			return fmt.Errorf("well-known browser route templates are invalid")
		}
	}
	caps := wk.Capabilities
	if caps.APIVersions == nil || caps.ArtifactSchemaVersions == nil || caps.ResourceKinds == nil ||
		caps.AcquisitionModes == nil || caps.Features == nil {
		return fmt.Errorf("capabilities are incomplete")
	}
	if len(caps.APIVersions) == 0 || len(caps.ArtifactSchemaVersions) == 0 || len(caps.ResourceKinds) == 0 ||
		len(caps.AcquisitionModes) == 0 {
		return fmt.Errorf("capabilities are empty")
	}
	for _, version := range caps.APIVersions {
		if strings.TrimSpace(version) == "" {
			return fmt.Errorf("capabilities contain an empty API version")
		}
	}
	for _, version := range caps.ArtifactSchemaVersions {
		if strings.TrimSpace(version) == "" {
			return fmt.Errorf("capabilities contain an empty artifact schema version")
		}
	}
	for _, kind := range caps.ResourceKinds {
		if !mpapi.IsMarketplaceKind(kind) {
			return fmt.Errorf("capabilities contain invalid resource kind %q", kind)
		}
	}
	for _, mode := range caps.AcquisitionModes {
		if mode != mpapi.AcquisitionIndexOnly && mode != mpapi.AcquisitionMirror {
			return fmt.Errorf("capabilities contain invalid acquisition mode %q", mode)
		}
	}
	return nil
}

func validateAPISearchResponse(response SearchResponse, request SearchRequest) error {
	if response.Total < 0 || response.Offset < 0 || response.Limit <= 0 || response.Offset != request.Offset {
		return fmt.Errorf("search pagination is invalid")
	}
	pageLimit := response.Limit
	if request.Limit > 0 && request.Limit < pageLimit {
		pageLimit = request.Limit
	}
	if len(response.Items) > pageLimit || (len(response.Items) > 0 && response.Offset+len(response.Items) > response.Total) {
		return fmt.Errorf("search page exceeds pagination bounds")
	}
	for _, item := range response.Items {
		if err := validateAPIResourceListItem(item); err != nil {
			return err
		}
	}
	return nil
}

func validateAPIResourceListItem(item mpapi.ResourceListItem) error {
	if strings.TrimSpace(item.RegistryID) == "" || strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.Name) == "" {
		return fmt.Errorf("resource list item coordinate is incomplete")
	}
	if !mpapi.IsMarketplaceKind(item.Kind) {
		return fmt.Errorf("resource list item kind %q is invalid", item.Kind)
	}
	if err := validateSHA256Digest(item.Revision); err != nil {
		return fmt.Errorf("revision: %w", err)
	}
	if err := validateSHA256Digest(item.ContentDigest); err != nil {
		return fmt.Errorf("content digest: %w", err)
	}
	if item.Revision != item.ContentDigest {
		return fmt.Errorf("revision and content digest differ")
	}
	return nil
}

func validateAPIExactRegistryOccurrence(occ mpapi.ExactRegistryOccurrence, registryID store.RegistryID, commit string) error {
	if occ.RegistryID == "" {
		return fmt.Errorf("registry ID is missing")
	}
	if registryID != "" && occ.RegistryID != registryID {
		return fmt.Errorf("registry ID %q does not match requested %q", occ.RegistryID, registryID)
	}
	if occ.Commit != commit || !mpapi.IsCanonicalGitCommit(occ.Commit) {
		return fmt.Errorf("occurrence commit is not the requested full commit")
	}
	if strings.TrimSpace(occ.Ref) == "" || strings.TrimSpace(occ.AcquisitionOrigin) == "" || len(occ.Roots) == 0 || occ.OccurredAt.IsZero() {
		return fmt.Errorf("registry occurrence fields are incomplete")
	}
	for i, root := range occ.Roots {
		if strings.TrimSpace(root.Path) == "" {
			return fmt.Errorf("registry occurrence root %d is incomplete", i)
		}
	}
	if err := validateSHA256Digest(string(occ.RegistryRevisionManifestDigest)); err != nil {
		return fmt.Errorf("registry manifest digest: %w", err)
	}
	if err := occ.Acquisition.Validate(); err != nil {
		return err
	}
	if err := validateAcquisitionOrigin(occ.AcquisitionOrigin); err != nil {
		return fmt.Errorf("acquisition origin: %w", err)
	}
	return nil
}

// validateAcquisitionOrigin is stricter than the general registry URL check:
// an index-only occurrence must name a Git remote, not a plain local directory,
// and must not carry credentials or URL query state. SCP syntax remains
// supported because it is the documented SSH form for local registry entries.
func validateAcquisitionOrigin(raw string) error {
	origin := strings.TrimSpace(raw)
	if err := cache.ValidateGitURL(origin); err != nil {
		return err
	}
	if filepath.IsAbs(origin) || origin == "." || origin == ".." || strings.HasPrefix(origin, "./") || strings.HasPrefix(origin, "../") {
		return fmt.Errorf("bare relative or absolute paths are not allowed")
	}
	if strings.ContainsAny(origin, "?#") {
		return fmt.Errorf("query and fragment are not allowed")
	}

	parsed, err := url.Parse(origin)
	if err == nil && parsed.Scheme != "" {
		if parsed.User != nil {
			return fmt.Errorf("URL userinfo is not allowed")
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "ssh":
			if parsed.Host == "" {
				return fmt.Errorf("URL host is required")
			}
		case "file":
			if hasPathTraversal(parsed.Path) {
				return fmt.Errorf("file URL contains path traversal")
			}
			if parsed.Host != "" || !filepath.IsAbs(parsed.Path) {
				return fmt.Errorf("file URL must use an absolute local path")
			}
			return nil
		default:
			return fmt.Errorf("unsupported Git URL scheme %q", parsed.Scheme)
		}
		if hasPathTraversal(parsed.Path) {
			return fmt.Errorf("URL contains path traversal")
		}
		return nil
	}

	if !isSCPOrigin(origin) {
		return fmt.Errorf("origin must be an HTTP(S), SSH, file URL, or SCP-style Git origin")
	}
	return nil
}

func hasPathTraversal(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func isSCPOrigin(origin string) bool {
	colon := strings.IndexByte(origin, ':')
	if colon <= 0 || strings.ContainsAny(origin[:colon], "/\\") || origin[colon+1:] == "" {
		return false
	}
	host := origin[:colon]
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		if at == 0 || strings.Contains(host[:at], ":") {
			return false
		}
		host = host[at+1:]
	}
	if host == "" || strings.HasPrefix(host, "-") || strings.HasPrefix(origin[colon+1:], "-") {
		return false
	}
	for _, r := range host {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

func validateAPIRegistry(registry mpapi.PublicRegistry) error {
	if strings.TrimSpace(string(registry.ID)) == "" || strings.TrimSpace(registry.Slug) == "" || strings.TrimSpace(registry.Ref) == "" {
		return fmt.Errorf("registry identity is incomplete")
	}
	if len(registry.Origins) == 0 || len(registry.Roots) == 0 {
		return fmt.Errorf("registry domain fields are incomplete")
	}
	for i, root := range registry.Roots {
		if strings.TrimSpace(root.Path) == "" {
			return fmt.Errorf("registry root %d has no path", i)
		}
	}
	if err := registry.Acquisition.Validate(); err != nil {
		return err
	}
	return nil
}

func validateAPIResourceRevision(rev store.AssessedResourceRevision) error {
	coord := rev.Coordinate
	if strings.TrimSpace(string(coord.Marketplace)) == "" || strings.TrimSpace(string(coord.Registry)) == "" || strings.TrimSpace(coord.Kind) == "" || strings.TrimSpace(coord.Name) == "" {
		return fmt.Errorf("resource coordinate is incomplete")
	}
	if !mpapi.IsMarketplaceKind(coord.Kind) {
		return fmt.Errorf("resource kind %q is invalid", coord.Kind)
	}
	if err := validateSHA256Digest(string(rev.ContentDigest)); err != nil {
		return fmt.Errorf("content digest: %w", err)
	}
	if coord.Kind == "collection" && rev.ArchiveDigest != "" {
		return fmt.Errorf("virtual collection resource revision must not have an archive digest")
	}
	if rev.ArchiveDigest != "" {
		if err := validateSHA256Digest(string(rev.ArchiveDigest)); err != nil {
			return fmt.Errorf("archive digest: %w", err)
		}
	}
	if err := rev.Assessment.Validate(); err != nil {
		return fmt.Errorf("assessment: %w", err)
	}
	if rev.Assessment.Coordinate != rev.Coordinate || rev.Assessment.ContentDigest != rev.ContentDigest {
		return fmt.Errorf("assessment identity does not match resource revision")
	}
	if rev.Occurrence.Coordinate != rev.Coordinate || rev.Occurrence.ContentDigest != rev.ContentDigest {
		return fmt.Errorf("occurrence identity does not match resource revision")
	}
	if !mpapi.IsCanonicalGitCommit(rev.Occurrence.Commit) {
		return fmt.Errorf("occurrence commit is not canonical")
	}
	if err := validateSHA256Digest(string(rev.Occurrence.ManifestDigest)); err != nil {
		return fmt.Errorf("occurrence manifest digest: %w", err)
	}
	return nil
}

func validateAPIResourceRevisionMatches(rev store.AssessedResourceRevision, expected store.ResourceCoordinate, expectedDigest store.RevisionDigest) error {
	if err := validateAPIResourceRevision(rev); err != nil {
		return err
	}
	if expected.Marketplace != "" && rev.Coordinate.Marketplace != expected.Marketplace {
		return fmt.Errorf("coordinate.marketplace %q does not match requested %q", rev.Coordinate.Marketplace, expected.Marketplace)
	}
	if expected.Registry != "" && rev.Coordinate.Registry != expected.Registry {
		return fmt.Errorf("coordinate.registry %q does not match requested %q", rev.Coordinate.Registry, expected.Registry)
	}
	if expected.Kind != "" && rev.Coordinate.Kind != expected.Kind {
		return fmt.Errorf("coordinate.kind %q does not match requested %q", rev.Coordinate.Kind, expected.Kind)
	}
	if expected.Name != "" && rev.Coordinate.Name != expected.Name {
		return fmt.Errorf("coordinate.name %q does not match requested %q", rev.Coordinate.Name, expected.Name)
	}
	if expectedDigest != "" && rev.ContentDigest != expectedDigest {
		return fmt.Errorf("exact revision content digest mismatch: response %q, requested %q", rev.ContentDigest, expectedDigest)
	}
	return nil
}

func (c *Client) expectedResourceCoordinate(registry, kind, name string) store.ResourceCoordinate {
	c.identityMu.RLock()
	defer c.identityMu.RUnlock()
	return store.ResourceCoordinate{
		Marketplace: c.instanceID,
		Registry:    c.registryIDs[registry],
		Kind:        kind,
		Name:        name,
	}
}

func (c *Client) rememberRegistry(requested string, registry mpapi.PublicRegistry) {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	c.registryIDs[requested] = registry.ID
	c.registryIDs[registry.Slug] = registry.ID
	for _, alias := range registry.Aliases {
		c.registryIDs[alias] = registry.ID
	}
}

func validateSHA256Digest(digest string) error {
	if !mpapi.IsCanonicalSHA256Digest(digest) {
		return fmt.Errorf("must be sha256:<64 lower-case hex>")
	}
	return nil
}

func apiError(status int, body []byte) error {
	if len(body) > maxAPIErrorBytes {
		body = body[:maxAPIErrorBytes]
	}
	var e struct {
		Error string `json:"error"`
	}
	detail := ""
	if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
		detail = e.Error
	} else if len(body) > 0 {
		detail = string(body)
	}
	quoted := QuoteTerminal(detail)
	if len(quoted) > maxAPIErrorOutputBytes {
		quoted = quoted[:maxAPIErrorOutputBytes-3] + "..."
	}
	return &APIError{StatusCode: status, Detail: quoted}
}
