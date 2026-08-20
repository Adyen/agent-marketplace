package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

const maxArtifactBytes = 64 * 1024 * 1024

const (
	maxAPIErrorBytes       = 4 * 1024
	maxAPIErrorOutputBytes = 4 * 1024
)

const defaultTimeout = 30 * time.Second

// Client is a thin HTTP client for the public Marketplace API.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string
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

// WellKnown fetches the well-known document that maps the Marketplace canonical
// URL to its API base and capabilities.
func (c *Client) WellKnown(ctx context.Context) (mpapi.WellKnown, error) {
	var wk mpapi.WellKnown
	if err := c.getJSON(ctx, c.requestURL([]string{".well-known", "agent-marketplace.json"}, nil), &wk); err != nil {
		return wk, err
	}
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
	return sr, nil
}

func (c *Client) getJSON(ctx context.Context, requestURL string, dst any) error {
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

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("marketplace decode %s: %w", req.URL.Redacted(), err)
	}
	return nil
}

// GetResourceRevision fetches a specific revision of a resource.
func (c *Client) GetResourceRevision(ctx context.Context, source, kind, name, revision string) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	segments := []string{"api", "v1", "resources", source, kind, name, "revisions", revision}
	if err := c.getJSON(ctx, c.requestURL(segments, nil), &rev); err != nil {
		return rev, err
	}
	return rev, nil
}

// GetLatestResourceRevision fetches the latest revision of a resource.
func (c *Client) GetLatestResourceRevision(ctx context.Context, source, kind, name string) (store.ResourceRevision, error) {
	var rev store.ResourceRevision
	segments := []string{"api", "v1", "resources", source, kind, name}
	if err := c.getJSON(ctx, c.requestURL(segments, nil), &rev); err != nil {
		return rev, err
	}
	return rev, nil
}

// GetSource fetches source metadata.
func (c *Client) GetSource(ctx context.Context, source string) (store.Source, error) {
	var src store.Source
	if err := c.getJSON(ctx, c.requestURL([]string{"api", "v1", "sources", source}, nil), &src); err != nil {
		return src, err
	}
	return src, nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("marketplace request %s: %w", requestURL, err)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("marketplace request %s: %w", req.URL.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	limit := int64(maxArtifactBytes)
	if resp.StatusCode != http.StatusOK {
		limit = maxAPIErrorBytes
	}
	body, err := readResponseBody(resp, limit)
	if err != nil {
		return nil, fmt.Errorf("marketplace read %s: %w", req.URL.Redacted(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp.StatusCode, body)
	}
	return body, nil
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

func validateSHA256Digest(digest string) error {
	if len(digest) != len("sha256:")+64 || !strings.HasPrefix(digest, "sha256:") {
		return fmt.Errorf("must be sha256:<64 lower-case hex>")
	}
	for _, c := range digest[len("sha256:"):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("must be sha256:<64 lower-case hex>")
		}
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
	if detail == "" {
		return fmt.Errorf("marketplace error %d", status)
	}
	quoted := QuoteTerminal(detail)
	if len(quoted) > maxAPIErrorOutputBytes {
		quoted = quoted[:maxAPIErrorOutputBytes-3] + "..."
	}
	return fmt.Errorf("marketplace error %d: %s", status, quoted)
}
