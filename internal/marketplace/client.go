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

	identityMu sync.RWMutex
	instanceID store.MarketplaceID
	sourceIDs  map[string]store.SourceID
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
		baseURL:    u,
		httpClient: &http.Client{Timeout: defaultTimeout},
		userAgent:  "agent-marketplace",
		sourceIDs:  make(map[string]store.SourceID),
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
	if err := validateAPISearchResponse(sr); err != nil {
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
func (c *Client) GetResourceRevision(ctx context.Context, source, kind, name, revision string) (store.ResourceRevision, error) {
	return c.getResourceRevision(ctx, source, source, kind, name, revision)
}

// GetResourceRevisionForSource keeps a slug in the route while validating the
// immutable Source ID returned by the Marketplace.
func (c *Client) GetResourceRevisionForSource(ctx context.Context, routeSource, sourceID, kind, name, revision string) (store.ResourceRevision, error) {
	return c.getResourceRevision(ctx, routeSource, sourceID, kind, name, revision)
}

func (c *Client) getResourceRevision(ctx context.Context, routeSource, expectedSource, kind, name, revision string) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	if err := validateSHA256Digest(revision); err != nil {
		return rev, fmt.Errorf("revision digest %q: %w", revision, err)
	}
	segments := []string{"api", "v1", "resources", routeSource, kind, name, "revisions", revision}
	if err := c.getJSON(ctx, c.requestURL(segments, nil), &rev); err != nil {
		return rev, err
	}
	if err := validateAPIResourceRevisionMatches(rev, c.expectedResourceCoordinate(expectedSource, kind, name), store.RevisionDigest(revision)); err != nil {
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

func (c *Client) GetResourceInspection(ctx context.Context, source, kind, name, revision string) (inspection.Manifest, error) {
	manifest, _, err := c.getResourceInspection(ctx, source, kind, name, revision)
	return manifest, err
}

func (c *Client) GetResourceInspectionManifest(ctx context.Context, source, kind, name, revision string) (inspection.Manifest, error) {
	return c.GetResourceInspection(ctx, source, kind, name, revision)
}

func (c *Client) GetResourceInspectionPrimary(ctx context.Context, source, kind, name, revision string) (InspectionContent, error) {
	return c.getResourceInspectionContent(ctx, source, kind, name, revision, "")
}

func (c *Client) GetResourceInspectionFile(ctx context.Context, source, kind, name, revision, fileDigest string) (InspectionContent, error) {
	if err := validateSHA256Digest(fileDigest); err != nil {
		return InspectionContent{}, fmt.Errorf("inspection file digest %q: %w", fileDigest, err)
	}
	return c.getResourceInspectionContent(ctx, source, kind, name, revision, fileDigest)
}

func (c *Client) getResourceInspection(ctx context.Context, source, kind, name, revision string) (inspection.Manifest, store.ResourceRevision, error) {
	var manifest inspection.Manifest
	if !mpapi.IsMarketplaceKind(kind) {
		return manifest, store.ResourceRevision{}, fmt.Errorf("inspection kind %q is invalid", kind)
	}
	if strings.TrimSpace(source) == "" || strings.TrimSpace(name) == "" {
		return manifest, store.ResourceRevision{}, fmt.Errorf("inspection coordinate is incomplete")
	}
	rev, err := c.getResourceRevision(ctx, source, "", kind, name, revision)
	if err != nil {
		return manifest, rev, err
	}
	if rev.Provenance == nil || rev.Provenance.InspectionManifestDigest == "" {
		return manifest, rev, fmt.Errorf("resource revision has no inspection manifest provenance")
	}
	manifestDigest := string(rev.Provenance.InspectionManifestDigest)
	if err := validateSHA256Digest(manifestDigest); err != nil {
		return manifest, rev, fmt.Errorf("inspection manifest provenance: %w", err)
	}
	raw, err := c.getRawLimited(ctx, c.requestURL([]string{"api", "v1", "resources", source, kind, name, "revisions", revision, "inspection"}, nil), inspection.MaxManifestBytes)
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

func (c *Client) getResourceInspectionContent(ctx context.Context, source, kind, name, revision, fileDigest string) (InspectionContent, error) {
	manifest, _, err := c.getResourceInspection(ctx, source, kind, name, revision)
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
	segments := []string{"api", "v1", "resources", source, kind, name, "revisions", revision, "inspection", "content"}
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
func (c *Client) GetLatestResourceRevision(ctx context.Context, source, kind, name string) (store.ResourceRevision, error) {
	return c.getLatestResourceRevision(ctx, source, source, kind, name)
}

// GetLatestResourceRevisionForSource keeps a slug in the route while validating
// the immutable Source ID returned by the Marketplace.
func (c *Client) GetLatestResourceRevisionForSource(ctx context.Context, routeSource, sourceID, kind, name string) (store.ResourceRevision, error) {
	return c.getLatestResourceRevision(ctx, routeSource, sourceID, kind, name)
}

func (c *Client) getLatestResourceRevision(ctx context.Context, routeSource, expectedSource, kind, name string) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	segments := []string{"api", "v1", "resources", routeSource, kind, name}
	if err := c.getJSON(ctx, c.requestURL(segments, nil), &rev); err != nil {
		return rev, err
	}
	if err := validateAPIResourceRevisionMatches(rev, c.expectedResourceCoordinate(expectedSource, kind, name), ""); err != nil {
		return rev, fmt.Errorf("invalid Marketplace resource revision: %w", err)
	}
	return rev, nil
}

// GetSource fetches source metadata addressed by its immutable Source ID. The
// response ID is checked before any metadata is remembered or returned so a
// route/proxy mix-up cannot retarget a pin.
func (c *Client) GetSource(ctx context.Context, sourceID string) (store.Source, error) {
	if strings.TrimSpace(sourceID) == "" {
		return store.Source{}, fmt.Errorf("source ID is required")
	}
	return c.getSource(ctx, sourceID, sourceID)
}

// GetSourceBySlug fetches source metadata through a user-facing slug or alias.
// A slug is not an immutable identity, so callers must use the returned ID for
// subsequent exact-coordinate requests.
func (c *Client) GetSourceBySlug(ctx context.Context, slug string) (store.Source, error) {
	return c.getSource(ctx, slug, "")
}

func (c *Client) getSource(ctx context.Context, routeSource, expectedSourceID string) (store.Source, error) {
	var public mpapi.PublicSource
	if err := c.getJSONStrict(ctx, c.requestURL([]string{"api", "v1", "sources", routeSource}, nil), &public); err != nil {
		return store.Source{}, err
	}
	if err := validateAPISource(public); err != nil {
		return store.Source{}, fmt.Errorf("invalid Marketplace source: %w", err)
	}
	if expectedSourceID != "" && public.ID != store.SourceID(expectedSourceID) {
		return store.Source{}, fmt.Errorf("source ID %q does not match requested %q", public.ID, expectedSourceID)
	}
	c.rememberSource(routeSource, public)

	src := store.Source{
		ID: public.ID, Slug: public.Slug, Aliases: public.Aliases,
		Origins: public.Origins, Ref: public.Ref, Roots: public.Roots,
		Acquisition: public.Acquisition, License: public.License, Listed: public.Listed,
	}
	if public.Publisher != nil {
		src.Publisher = store.Publisher{Name: public.Publisher.Name, URL: public.Publisher.URL}
	}
	return src, nil
}

// GetSourceOccurrence fetches the immutable occurrence for one exact commit and
// the source's current operator-approved acquisition origin.
func (c *Client) GetSourceOccurrence(ctx context.Context, source, commit string) (mpapi.ExactSourceOccurrence, error) {
	var occurrence mpapi.ExactSourceOccurrence
	if strings.TrimSpace(source) == "" {
		return occurrence, fmt.Errorf("source occurrence source ID is required")
	}
	if !mpapi.IsCanonicalGitCommit(commit) {
		return occurrence, fmt.Errorf("source occurrence commit %q is not canonical", commit)
	}
	segments := []string{"api", "v1", "sources", source, "occurrences", commit}
	if err := c.getJSONStrict(ctx, c.requestURL(segments, nil), &occurrence); err != nil {
		return occurrence, err
	}
	// The route accepts either the current source slug or immutable Source ID,
	// but a response for a different source must never be usable for this pin.
	if err := validateAPIExactSourceOccurrence(occurrence, store.SourceID(source), commit); err != nil {
		return occurrence, fmt.Errorf("invalid Marketplace source occurrence: %w", err)
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
	if err := validateSHA256Digest(archiveDigest); err != nil {
		return nil, fmt.Errorf("archive digest %q: %w", archiveDigest, err)
	}
	hexDigest := strings.TrimPrefix(archiveDigest, "sha256:")
	path := c.requestURL([]string{"api", "v1", "artifacts", "sha256", hexDigest}, nil)
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
		if wk.BrowserLatestRoute != wk.BrowserBase+"/s/{source}/{kind}/{name}" || wk.BrowserExactRoute != wk.BrowserBase+"/s/{source}/{kind}/{name}/revisions/{revision}" {
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

func validateAPISearchResponse(response SearchResponse) error {
	if response.Total < 0 || response.Offset < 0 || response.Limit <= 0 {
		return fmt.Errorf("search pagination is invalid")
	}
	for _, item := range response.Items {
		if err := validateAPIResourceListItem(item); err != nil {
			return err
		}
	}
	return nil
}

func validateAPIResourceListItem(item mpapi.ResourceListItem) error {
	if strings.TrimSpace(item.SourceID) == "" || strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.Name) == "" {
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

func validateAPIExactSourceOccurrence(occ mpapi.ExactSourceOccurrence, source store.SourceID, commit string) error {
	if occ.SourceID == "" {
		return fmt.Errorf("source ID is missing")
	}
	if source != "" && occ.SourceID != source {
		return fmt.Errorf("source ID %q does not match requested %q", occ.SourceID, source)
	}
	if occ.Commit != commit || !mpapi.IsCanonicalGitCommit(occ.Commit) {
		return fmt.Errorf("occurrence commit is not the requested full commit")
	}
	if strings.TrimSpace(occ.Ref) == "" || strings.TrimSpace(occ.AcquisitionOrigin) == "" || occ.OccurredAt.IsZero() {
		return fmt.Errorf("source occurrence fields are incomplete")
	}
	if err := validateSHA256Digest(string(occ.SourceRevisionManifestDigest)); err != nil {
		return fmt.Errorf("source manifest digest: %w", err)
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

func validateAPISource(src mpapi.PublicSource) error {
	if strings.TrimSpace(string(src.ID)) == "" || strings.TrimSpace(src.Slug) == "" || strings.TrimSpace(src.Ref) == "" {
		return fmt.Errorf("source identity is incomplete")
	}
	if len(src.Origins) == 0 || len(src.Roots) == 0 {
		return fmt.Errorf("source domain fields are incomplete")
	}
	for i, root := range src.Roots {
		if strings.TrimSpace(root.Path) == "" {
			return fmt.Errorf("source root %d has no path", i)
		}
	}
	if err := src.Acquisition.Validate(); err != nil {
		return err
	}
	return nil
}

func validateAPIResourceRevision(rev store.ResourceRevision) error {
	coord := rev.Coordinate
	if strings.TrimSpace(string(coord.Marketplace)) == "" || strings.TrimSpace(string(coord.Source)) == "" || strings.TrimSpace(coord.Kind) == "" || strings.TrimSpace(coord.Name) == "" {
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
	if rev.Provenance != nil {
		if rev.Provenance.Commit != "" && !mpapi.IsCanonicalGitCommit(rev.Provenance.Commit) {
			return fmt.Errorf("provenance commit is not canonical")
		}
		if rev.Provenance.ManifestDigest != "" {
			if err := validateSHA256Digest(string(rev.Provenance.ManifestDigest)); err != nil {
				return fmt.Errorf("provenance manifest digest: %w", err)
			}
		}
	}
	return nil
}

func validateAPIResourceRevisionMatches(rev store.ResourceRevision, expected store.ResourceCoordinate, expectedDigest store.RevisionDigest) error {
	if err := validateAPIResourceRevision(rev); err != nil {
		return err
	}
	if expected.Marketplace != "" && rev.Coordinate.Marketplace != expected.Marketplace {
		return fmt.Errorf("coordinate.marketplace %q does not match requested %q", rev.Coordinate.Marketplace, expected.Marketplace)
	}
	if expected.Source != "" && rev.Coordinate.Source != expected.Source {
		return fmt.Errorf("coordinate.source %q does not match requested %q", rev.Coordinate.Source, expected.Source)
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

func (c *Client) expectedResourceCoordinate(source, kind, name string) store.ResourceCoordinate {
	c.identityMu.RLock()
	defer c.identityMu.RUnlock()
	return store.ResourceCoordinate{
		Marketplace: c.instanceID,
		Source:      c.sourceIDs[source],
		Kind:        kind,
		Name:        name,
	}
}

func (c *Client) rememberSource(requested string, source mpapi.PublicSource) {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	c.sourceIDs[requested] = source.ID
	c.sourceIDs[source.Slug] = source.ID
	for _, alias := range source.Aliases {
		c.sourceIDs[alias] = source.ID
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
