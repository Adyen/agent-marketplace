package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

const (
	// catalogScanPageLimit is the requested page size for catalog scans.
	catalogScanPageLimit = 500
	// maxCatalogScanPages bounds a single scan so a hostile or buggy endpoint
	// cannot keep a client paging forever.
	maxCatalogScanPages = 64
	// maxCatalogScanResources bounds the aggregate resources a scan accepts.
	maxCatalogScanResources = catalogScanPageLimit * maxCatalogScanPages
	// maxCatalogPageBytes bounds one catalog page response body.
	maxCatalogPageBytes = 16 << 20
	// maxCatalogScanBytes bounds the aggregate response bytes a scan accepts
	// across all pages.
	maxCatalogScanBytes = 32 << 20
	// catalogScanTimeout bounds one endpoint scan.
	catalogScanTimeout = 60 * time.Second

	catalogCacheSchema    = 1
	maxCatalogCacheSize   = 32 << 20
	maxCatalogCacheRecord = maxCatalogScanResources
)

// ErrCatalogNotCached marks an endpoint outcome where no verified complete
// catalog record exists yet; it is an expected cold-start state, not a failure.
var ErrCatalogNotCached = errors.New("no verified complete catalog is cached")

// CatalogOutcome is the per-endpoint result of a catalog scan or cache read.
// Complete is true only for a verified, identity-consistent full scan (or its
// persisted record); Err reports why an endpoint has no complete catalog.
type CatalogOutcome struct {
	Endpoint       config.MarketplaceEntry
	Resources      []mpapi.CatalogResource
	CatalogVersion string
	ETag           string
	FetchedAt      time.Time
	Complete       bool
	FromCache      bool
	Err            error
}

// CatalogScanOptions configures ScanCatalog.
type CatalogScanOptions struct {
	UserAgent string
	Timeout   time.Duration
	Cache     *CatalogCache
}

// catalogPageResult is one fetched page plus its validators.
type catalogPageResult struct {
	page        mpapi.CatalogPage
	etag        string
	notModified bool
	bytes       int
}

// CatalogPage fetches a single catalog page. ifNoneMatch is only sent when
// after is empty; the server rejects conditional continuation requests.
func (c *Client) CatalogPage(ctx context.Context, after mpapi.CatalogKeysetCursor, ifNoneMatch string) (catalogPageResult, error) {
	var result catalogPageResult
	q := url.Values{}
	q.Set("limit", fmt.Sprint(catalogScanPageLimit))
	if after != "" {
		q.Set("after", string(after))
		ifNoneMatch = ""
	}
	requestURL := c.requestURL([]string{"api", "v1", "catalog"}, q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return result, fmt.Errorf("marketplace request %s: %w", requestURL, err)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	resp, err := c.do(req)
	if err != nil {
		return result, fmt.Errorf("marketplace request %s: %w", req.URL.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified && after == "" {
		result.notModified = true
		return result, nil
	}

	limit := int64(maxCatalogPageBytes)
	if resp.StatusCode != http.StatusOK {
		limit = maxAPIErrorBytes
	}
	body, err := readResponseBody(resp, limit)
	if err != nil {
		return result, fmt.Errorf("marketplace read %s: %w", req.URL.Redacted(), err)
	}
	result.bytes = len(body)
	if resp.StatusCode != http.StatusOK {
		return result, apiError(resp.StatusCode, body)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&result.page); err != nil {
		return result, fmt.Errorf("marketplace decode %s: %w", req.URL.Redacted(), err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return result, fmt.Errorf("marketplace decode %s: trailing JSON value", req.URL.Redacted())
		}
		return result, fmt.Errorf("marketplace decode %s: %w", req.URL.Redacted(), err)
	}
	result.etag = resp.Header.Get("ETag")
	return result, nil
}

// validateCatalogResource enforces the coordinate integrity a scan relies on.
func validateCatalogResource(resource mpapi.CatalogResource) error {
	if strings.TrimSpace(resource.RegistryID) == "" || strings.TrimSpace(resource.Name) == "" {
		return fmt.Errorf("catalog resource coordinate is incomplete")
	}
	if !mpapi.IsMarketplaceKind(resource.Kind) {
		return fmt.Errorf("catalog resource kind %q is invalid", resource.Kind)
	}
	if err := validateSHA256Digest(resource.Revision); err != nil {
		return fmt.Errorf("catalog resource revision: %w", err)
	}
	return nil
}

// ScanCatalog performs a bounded, identity-verified complete scan of one
// Marketplace endpoint's /api/v1/catalog. Only a fully consistent result is
// persisted and returned as Complete; every failure keeps any previously
// cached complete record untouched.
func ScanCatalog(ctx context.Context, ep config.MarketplaceEntry, opts CatalogScanOptions) CatalogOutcome {
	out := CatalogOutcome{Endpoint: ep}
	fail := func(err error) CatalogOutcome {
		out.Err = err
		return out
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = catalogScanTimeout
	}
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := NewClient(ep.URL, WithUserAgent(opts.UserAgent))
	if err != nil {
		return fail(err)
	}
	wk, err := client.WellKnown(scanCtx)
	if err != nil {
		return fail(fmt.Errorf("well-known document: %w", err))
	}
	if ep.InstanceID != "" && wk.InstanceID != ep.InstanceID {
		return fail(fmt.Errorf("instance ID mismatch: endpoint %q, configured %q", wk.InstanceID, ep.InstanceID))
	}

	var cached CatalogOutcome
	cachedHit := false
	if opts.Cache != nil {
		if c, hit := opts.Cache.Load(ep); hit {
			cached, cachedHit = c, true
		}
	}

	version := ""
	etag := ""
	totalBytes := 0
	var resources []mpapi.CatalogResource
	var cursor mpapi.CatalogKeysetCursor
	for page := 0; page < maxCatalogScanPages; page++ {
		conditional := ""
		if page == 0 && cachedHit {
			conditional = cached.ETag
		}
		result, err := client.CatalogPage(scanCtx, cursor, conditional)
		if err != nil {
			return fail(err)
		}
		totalBytes += result.bytes
		if totalBytes > maxCatalogScanBytes {
			return fail(fmt.Errorf("catalog scan exceeded %d response bytes", maxCatalogScanBytes))
		}
		if result.notModified {
			if cachedHit && cached.Complete {
				return cached
			}
			return fail(errors.New("catalog returned 304 without a usable complete cache record"))
		}
		p := result.page
		if p.APIVersion != mpapi.CatalogAPIVersionV1Alpha1 {
			return fail(fmt.Errorf("catalog api_version %q is unsupported", p.APIVersion))
		}
		if p.MarketplaceID != wk.InstanceID {
			return fail(fmt.Errorf("catalog marketplace_id %q does not match instance %q", p.MarketplaceID, wk.InstanceID))
		}
		if page == 0 {
			version = p.CatalogVersion
			if version == "" {
				return fail(errors.New("catalog_version is empty"))
			}
		} else if p.CatalogVersion != version {
			return fail(fmt.Errorf("catalog_version changed mid-scan: %q then %q", version, p.CatalogVersion))
		}
		if len(resources)+len(p.Resources) > maxCatalogScanResources {
			return fail(fmt.Errorf("catalog scan exceeded %d resources", maxCatalogScanResources))
		}
		for _, resource := range p.Resources {
			if err := validateCatalogResource(resource); err != nil {
				return fail(err)
			}
		}
		resources = append(resources, p.Resources...)
		if result.etag != "" {
			etag = result.etag
		}
		if p.Complete {
			out.Resources = resources
			out.CatalogVersion = version
			out.ETag = etag
			out.FetchedAt = time.Now()
			out.Complete = true
			if opts.Cache != nil {
				if err := opts.Cache.Store(out); err != nil {
					out.Complete = false
					return fail(fmt.Errorf("persist catalog cache: %w", err))
				}
			}
			return out
		}
		if p.NextCursor == "" {
			return fail(errors.New("catalog scan truncated: incomplete page without a cursor"))
		}
		cursor = p.NextCursor
	}
	return fail(fmt.Errorf("catalog scan exceeded %d pages", maxCatalogScanPages))
}

// catalogCacheRecord is the on-disk form of one verified complete catalog.
type catalogCacheRecord struct {
	Schema         int                     `json:"schema"`
	Endpoint       string                  `json:"endpoint"`
	InstanceID     string                  `json:"instance_id"`
	CatalogVersion string                  `json:"catalog_version"`
	ETag           string                  `json:"etag"`
	FetchedAt      time.Time               `json:"fetched_at"`
	Complete       bool                    `json:"complete"`
	Resources      []mpapi.CatalogResource `json:"resources"`
}

// CatalogCache persists verified complete catalog records per endpoint under
// the Marketplace cache root.
type CatalogCache struct {
	root string
	now  func() time.Time
}

// NewCatalogCache returns a cache rooted at the Marketplace cache root.
func NewCatalogCache(root string) *CatalogCache {
	return &CatalogCache{root: filepath.Join(root, "catalog"), now: time.Now}
}

// DefaultCatalogCache returns the shared default catalog cache.
func DefaultCatalogCache() *CatalogCache { return NewCatalogCache(DefaultCache().root) }

func (c *CatalogCache) path(ep config.MarketplaceEntry) string {
	return filepath.Join(c.root, discoveryEndpointDirectoryName(ep)+".json")
}

// Load returns a previously stored verified complete record. Missing, corrupt,
// or unsafe records are reported as a miss.
func (c *CatalogCache) Load(ep config.MarketplaceEntry) (CatalogOutcome, bool) {
	if c == nil || c.root == "" {
		return CatalogOutcome{}, false
	}
	path := c.path(ep)
	exists, err := validateCacheRegularFile(c.root, path)
	if err != nil || !exists {
		return CatalogOutcome{}, false
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return CatalogOutcome{}, false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCatalogCacheSize {
		return CatalogOutcome{}, false
	}
	var record catalogCacheRecord
	dec := json.NewDecoder(io.LimitReader(f, maxCatalogCacheSize+1))
	if err := dec.Decode(&record); err != nil {
		return CatalogOutcome{}, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return CatalogOutcome{}, false
	}
	if !c.validRecord(ep, record) {
		return CatalogOutcome{}, false
	}
	return CatalogOutcome{
		Endpoint: ep, Resources: record.Resources, CatalogVersion: record.CatalogVersion,
		ETag: record.ETag, FetchedAt: record.FetchedAt, Complete: true, FromCache: true,
	}, true
}

func (c *CatalogCache) validRecord(ep config.MarketplaceEntry, record catalogCacheRecord) bool {
	if record.Schema != catalogCacheSchema || record.Endpoint != ep.URL || record.InstanceID != ep.InstanceID ||
		!record.Complete || record.FetchedAt.IsZero() || c.now().Before(record.FetchedAt) ||
		len(record.Resources) > maxCatalogCacheRecord {
		return false
	}
	for _, resource := range record.Resources {
		if err := validateCatalogResource(resource); err != nil {
			return false
		}
	}
	return true
}

// Store persists a complete scan result under the cache mutation lock. Only
// complete records are written; a failed scan never overwrites the previous
// verified record.
func (c *CatalogCache) Store(out CatalogOutcome) error {
	if c == nil || c.root == "" {
		return nil
	}
	if !out.Complete {
		return errors.New("refusing to cache an incomplete catalog")
	}
	record := catalogCacheRecord{
		Schema: catalogCacheSchema, Endpoint: out.Endpoint.URL, InstanceID: out.Endpoint.InstanceID,
		CatalogVersion: out.CatalogVersion, ETag: out.ETag, FetchedAt: out.FetchedAt,
		Complete: true, Resources: out.Resources,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode catalog cache: %w", err)
	}
	if len(data) > maxCatalogCacheSize {
		return errors.New("catalog cache record exceeds size limit")
	}
	_, err = cache.WithCacheMutationLock(func() (struct{}, error) {
		path := c.path(out.Endpoint)
		if err := ensureCacheDirectory(c.root, filepath.Dir(path)); err != nil {
			return struct{}{}, fmt.Errorf("prepare catalog cache %q: %w", path, err)
		}
		if err := writeAtomic(c.root, path, data, 0o600, ""); err != nil {
			return struct{}{}, fmt.Errorf("write catalog cache %q: %w", path, err)
		}
		return struct{}{}, nil
	})
	return err
}

// catalogEndpoints resolves and validates the configured Marketplace endpoints.
func catalogEndpoints(ec config.EffectiveConfig) ([]searchEndpoint, error) {
	endpoints, err := resolveMarketplaceEndpoints(ec, "all")
	if err != nil {
		return nil, err
	}
	entries := make([]config.MarketplaceEntry, 0, len(endpoints))
	for _, endpoint := range endpoints {
		entries = append(entries, endpoint.MarketplaceEntry)
	}
	if err := config.ValidateMarketplaceURLs(entries, ec.AllowedMarketplaces); err != nil {
		return nil, err
	}
	return endpoints, nil
}

// CachedCatalog returns one outcome per configured endpoint read purely from
// the complete-catalog cache. Endpoints without a verified complete record
// come back with Err set so callers can mark them stale without a network
// request.
func CachedCatalog(ec config.EffectiveConfig, catalogCache *CatalogCache) ([]CatalogOutcome, error) {
	endpoints, err := catalogEndpoints(ec)
	if err != nil {
		return nil, err
	}
	if catalogCache == nil {
		catalogCache = DefaultCatalogCache()
	}
	outcomes := make([]CatalogOutcome, 0, len(endpoints))
	for _, endpoint := range endpoints {
		ep := endpoint.MarketplaceEntry
		if out, hit := catalogCache.Load(ep); hit {
			outcomes = append(outcomes, out)
			continue
		}
		outcomes = append(outcomes, CatalogOutcome{Endpoint: ep, Err: ErrCatalogNotCached})
	}
	return outcomes, nil
}

// RefreshCatalog scans every configured endpoint's catalog concurrently and
// returns one outcome per endpoint. Successful scans persist complete records;
// failures never overwrite the last verified record.
func RefreshCatalog(ctx context.Context, ec config.EffectiveConfig, opts CatalogScanOptions) ([]CatalogOutcome, error) {
	endpoints, err := catalogEndpoints(ec)
	if err != nil {
		return nil, err
	}
	if opts.Cache == nil {
		opts.Cache = DefaultCatalogCache()
	}
	outcomes := make([]CatalogOutcome, len(endpoints))
	semaphore := make(chan struct{}, MaxSearchConcurrency)
	var wg sync.WaitGroup
	for i, endpoint := range endpoints {
		wg.Add(1)
		go func(i int, ep config.MarketplaceEntry) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				outcomes[i] = CatalogOutcome{Endpoint: ep, Err: ctx.Err()}
				return
			}
			outcomes[i] = ScanCatalog(ctx, ep, opts)
		}(i, endpoint.MarketplaceEntry)
	}
	wg.Wait()
	return outcomes, nil
}
