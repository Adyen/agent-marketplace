package marketplace

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"golang.org/x/sys/unix"
)

const (
	DiscoveryPageLimit          = 50
	DiscoveryFreshTTL           = 5 * time.Minute
	DiscoveryStaleTTL           = 24 * time.Hour
	DiscoveryQueryLimit         = 50
	discoveryCacheSchema        = 1
	maxDiscoveryCacheSize       = 2 * 1024 * 1024
	maxDiscoveryMaintenanceDirs = 4096
	maxDiscoveryDirEntries      = DiscoveryQueryLimit + 128
	discoveryCacheLockName      = ".discovery.lock"
)

type DiscoveryCacheState string

const (
	DiscoveryCacheMiss  DiscoveryCacheState = "miss"
	DiscoveryCacheFresh DiscoveryCacheState = "fresh"
	DiscoveryCacheStale DiscoveryCacheState = "stale"
)

type DiscoveryOutcome struct {
	Endpoint   config.MarketplaceEntry
	Items      []mpapi.ResourceListItem
	Total      int
	Truncated  bool
	CacheState DiscoveryCacheState
	ObservedAt time.Time
	Err        error
}

type DiscoveryOptions struct {
	Kind      string
	Limit     int
	Timeout   time.Duration
	UserAgent string
	Cache     *DiscoveryCache
}

type DiscoveryCache struct {
	root string
	now  func() time.Time
}

type discoveryCacheRecord struct {
	Schema     int                      `json:"schema"`
	Endpoint   string                   `json:"endpoint"`
	InstanceID string                   `json:"instance_id"`
	Query      string                   `json:"query"`
	Kind       string                   `json:"kind"`
	Limit      int                      `json:"limit"`
	ObservedAt time.Time                `json:"observed_at"`
	Items      []mpapi.ResourceListItem `json:"items"`
	Total      int                      `json:"total"`
}

func NewDiscoveryCache(root string) *DiscoveryCache {
	return &DiscoveryCache{root: filepath.Join(root, "discovery"), now: time.Now}
}

func DefaultDiscoveryCache() *DiscoveryCache { return NewDiscoveryCache(DefaultCache().root) }

func (c *DiscoveryCache) withFilesystemLock(createParent bool, fn func() error) error {
	parent := filepath.Dir(c.root)
	if createParent {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return fmt.Errorf("create discovery cache parent: %w", err)
		}
	}
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) && !createParent {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("discovery cache lock parent is unsafe")
	}
	lockPath := filepath.Join(parent, discoveryCacheLockName)
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open discovery cache lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	lockInfo, err := lock.Stat()
	if err != nil || !lockInfo.Mode().IsRegular() {
		return errors.New("discovery cache lock is unsafe")
	}
	for {
		err = unix.Flock(int(lock.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("lock discovery cache: %w", err)
	}
	defer func() { _ = unix.Flock(int(lock.Fd()), unix.LOCK_UN) }()
	return fn()
}

func normalizeDiscoveryQuery(query string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
}

func (c *DiscoveryCache) key(ep config.MarketplaceEntry, query, kind string, limit int) string {
	sum := sha256.Sum256([]byte(fmt.Sprint(discoveryCacheSchema) + "\x00" + ep.URL + "\x00" + ep.InstanceID + "\x00" + query + "\x00" + kind + "\x00" + fmt.Sprint(limit)))
	return hex.EncodeToString(sum[:])
}

func (c *DiscoveryCache) path(ep config.MarketplaceEntry, query, kind string, limit int) string {
	epSum := sha256.Sum256([]byte(ep.URL + "\x00" + ep.InstanceID))
	return filepath.Join(c.root, hex.EncodeToString(epSum[:]), c.key(ep, query, kind, limit)+".json")
}

func (c *DiscoveryCache) Load(ep config.MarketplaceEntry, query, kind string, limit int) (DiscoveryOutcome, bool) {
	// A cache namespace is exact evidence, never a source of endpoint identity.
	if c == nil || c.root == "" || strings.TrimSpace(ep.InstanceID) == "" {
		return DiscoveryOutcome{}, false
	}
	path := c.path(ep, query, kind, limit)
	for _, dir := range []string{c.root, filepath.Dir(path)} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return DiscoveryOutcome{}, false
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDiscoveryCacheSize {
		return DiscoveryOutcome{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return DiscoveryOutcome{}, false
	}
	defer func() { _ = f.Close() }()
	var record discoveryCacheRecord
	dec := json.NewDecoder(io.LimitReader(f, maxDiscoveryCacheSize+1))
	if err := dec.Decode(&record); err != nil {
		return DiscoveryOutcome{}, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return DiscoveryOutcome{}, false
	}
	if record.Schema != discoveryCacheSchema || record.Endpoint != ep.URL || record.InstanceID != ep.InstanceID || record.Query != query || record.Kind != kind || record.Limit != limit || record.ObservedAt.IsZero() {
		return DiscoveryOutcome{}, false
	}
	if err := validateDiscoveryPage(record.Items, record.Total, record.Limit); err != nil {
		return DiscoveryOutcome{}, false
	}
	age := c.now().Sub(record.ObservedAt)
	if age < 0 || age > DiscoveryStaleTTL {
		return DiscoveryOutcome{}, false
	}
	state := DiscoveryCacheStale
	if age <= DiscoveryFreshTTL {
		state = DiscoveryCacheFresh
	}
	return DiscoveryOutcome{Endpoint: ep, Items: record.Items, Total: record.Total, Truncated: record.Total > len(record.Items), CacheState: state, ObservedAt: record.ObservedAt}, true
}

func (c *DiscoveryCache) Store(out DiscoveryOutcome, query, kind string, limit int) error {
	if c == nil || c.root == "" {
		return nil
	}
	if err := validateDiscoveryPage(out.Items, out.Total, limit); err != nil {
		return fmt.Errorf("invalid discovery cache page: %w", err)
	}
	record := discoveryCacheRecord{Schema: discoveryCacheSchema, Endpoint: out.Endpoint.URL, InstanceID: out.Endpoint.InstanceID, Query: query, Kind: kind, Limit: limit, ObservedAt: out.ObservedAt, Items: out.Items, Total: out.Total}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode discovery cache: %w", err)
	}
	if len(data) > maxDiscoveryCacheSize {
		return errors.New("discovery cache record exceeds size limit")
	}
	return c.withFilesystemLock(true, func() error {
		return c.storeLocked(out.Endpoint, query, kind, limit, data)
	})
}

func (c *DiscoveryCache) storeLocked(endpoint config.MarketplaceEntry, query, kind string, limit int, data []byte) error {
	path := c.path(endpoint, query, kind, limit)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(c.root, 0o700); err != nil {
		return fmt.Errorf("create discovery cache root: %w", err)
	}
	if info, err := os.Lstat(c.root); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("discovery cache root is unsafe")
	}
	rootEntries, err := readDirWithinCapacity(c.root, maxDiscoveryMaintenanceDirs)
	if err != nil {
		return fmt.Errorf("prepare discovery cache root: %w", err)
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if len(rootEntries) == maxDiscoveryMaintenanceDirs {
			return errors.New("discovery cache root is at capacity")
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			return fmt.Errorf("create discovery cache endpoint directory: %w", err)
		}
		info, err = os.Lstat(dir)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("discovery cache directory is unsafe")
	}
	entries, err := readDirWithinCapacity(dir, maxDiscoveryDirEntries)
	if err != nil {
		return fmt.Errorf("prepare discovery cache directory: %w", err)
	}
	_, statErr := os.Lstat(path)
	replacing := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect discovery cache destination: %w", statErr)
	}
	if replacing {
		// The lock makes the temporary entry the only transient capacity excess;
		// rename removes it while replacing this recognized record count-neutrally.
		if _, recognized := c.readRecognizedRecord(dir, filepath.Base(path)); !recognized {
			return errors.New("discovery cache destination is not an owned record")
		}
	} else if len(entries) == maxDiscoveryDirEntries {
		return errors.New("discovery cache directory is at capacity")
	}
	tmp, err := os.CreateTemp(dir, ".discovery-*")
	if err != nil {
		return fmt.Errorf("create discovery cache temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write discovery cache: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace discovery cache: %w", err)
	}
	return c.retainLocked(dir)
}

func readDirBounded(path string, limit int) ([]os.DirEntry, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	entries, readErr := f.ReadDir(limit + 1)
	closeErr := f.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, false, readErr
	}
	if closeErr != nil {
		return nil, false, closeErr
	}
	if len(entries) > limit {
		return nil, true, nil
	}
	return entries, false, nil
}

func readDirWithinCapacity(path string, limit int) ([]os.DirEntry, error) {
	entries, overCap, err := readDirBounded(path, limit)
	if err != nil {
		return nil, err
	}
	if overCap {
		return nil, errors.New("discovery cache directory capacity exceeded")
	}
	return entries, nil
}

func (c *DiscoveryCache) retain(dir string) error {
	return c.withFilesystemLock(false, func() error { return c.retainLocked(dir) })
}

func (c *DiscoveryCache) retainLocked(dir string) error {
	entries, overCap, err := readDirBounded(dir, maxDiscoveryDirEntries)
	if err != nil {
		return err
	}
	if overCap {
		return nil
	}
	type candidate struct {
		path     string
		observed time.Time
	}
	var empty, nonEmpty []candidate
	for _, entry := range entries {
		record, ok := c.readRecognizedRecord(dir, entry.Name())
		if !ok {
			continue
		}
		candidate := candidate{path: filepath.Join(dir, entry.Name()), observed: record.ObservedAt}
		if record.Query == "" {
			empty = append(empty, candidate)
		} else {
			nonEmpty = append(nonEmpty, candidate)
		}
	}
	newestFirst := func(a, b candidate) int {
		if c := b.observed.Compare(a.observed); c != 0 {
			return c
		}
		return cmp.Compare(a.path, b.path)
	}
	slices.SortFunc(empty, newestFirst)
	slices.SortFunc(nonEmpty, newestFirst)
	for _, candidates := range [][]candidate{empty[min(len(empty), 1):], nonEmpty[min(len(nonEmpty), DiscoveryQueryLimit):]} {
		for _, old := range candidates {
			if err := os.Remove(old.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func discoveryEndpointDirectoryName(ep config.MarketplaceEntry) string {
	sum := sha256.Sum256([]byte(ep.URL + "\x00" + ep.InstanceID))
	return hex.EncodeToString(sum[:])
}

func validDiscoveryHashName(name string) bool {
	if len(name) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(name)
	return err == nil && hex.EncodeToString(decoded) == name
}

func (c *DiscoveryCache) readRecognizedRecord(dir, name string) (discoveryCacheRecord, bool) {
	if filepath.Ext(name) != ".json" || !validDiscoveryHashName(strings.TrimSuffix(name, ".json")) {
		return discoveryCacheRecord{}, false
	}
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDiscoveryCacheSize {
		return discoveryCacheRecord{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return discoveryCacheRecord{}, false
	}
	defer func() { _ = f.Close() }()
	var record discoveryCacheRecord
	dec := json.NewDecoder(io.LimitReader(f, maxDiscoveryCacheSize+1))
	if err := dec.Decode(&record); err != nil {
		return discoveryCacheRecord{}, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return discoveryCacheRecord{}, false
	}
	ep := config.MarketplaceEntry{URL: record.Endpoint, InstanceID: record.InstanceID}
	if record.Schema != discoveryCacheSchema || discoveryEndpointDirectoryName(ep) != filepath.Base(dir) || c.key(ep, record.Query, record.Kind, record.Limit)+".json" != name || record.ObservedAt.IsZero() || c.now().Before(record.ObservedAt) {
		return discoveryCacheRecord{}, false
	}
	if err := validateDiscoveryPage(record.Items, record.Total, record.Limit); err != nil {
		return discoveryCacheRecord{}, false
	}
	return record, true
}

// maintain removes only fully recognized cache content. Scans are capped so a
// hostile or accidentally polluted cache cannot turn discovery into unbounded
// filesystem work.
func (c *DiscoveryCache) maintain(endpoints []config.MarketplaceEntry) error {
	if c == nil || c.root == "" {
		return nil
	}
	return c.withFilesystemLock(false, func() error { return c.maintainLocked(endpoints) })
}

func (c *DiscoveryCache) maintainLocked(endpoints []config.MarketplaceEntry) error {
	info, err := os.Lstat(c.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	entries, overCap, err := readDirBounded(c.root, maxDiscoveryMaintenanceDirs)
	if err != nil {
		return err
	}
	if overCap {
		return nil
	}
	current := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		current[discoveryEndpointDirectoryName(endpoint)] = struct{}{}
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !validDiscoveryHashName(entry.Name()) {
			continue
		}
		dir := filepath.Join(c.root, entry.Name())
		children, overCap, readErr := readDirBounded(dir, maxDiscoveryDirEntries)
		if readErr != nil || overCap {
			continue
		}
		_, retained := current[entry.Name()]
		recognized := make([]string, 0, len(children))
		allRecognized := true
		for _, child := range children {
			record, ok := c.readRecognizedRecord(dir, child.Name())
			if !ok {
				allRecognized = false
				continue
			}
			path := filepath.Join(dir, child.Name())
			recognized = append(recognized, path)
			if retained && c.now().Sub(record.ObservedAt) > DiscoveryStaleTTL {
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
		if retained || !allRecognized {
			continue
		}
		for _, path := range recognized {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// CachedDiscovery returns retained endpoint pages without network access and
// reports whether any endpoint needs fetching or stale revalidation.
func CachedDiscovery(ec config.EffectiveConfig, query string, opts DiscoveryOptions) ([]DiscoveryOutcome, bool, error) {
	query = normalizeDiscoveryQuery(query)
	limit := opts.Limit
	if limit <= 0 || limit > DiscoveryPageLimit {
		limit = DiscoveryPageLimit
	}
	endpoints, err := resolveMarketplaceEndpoints(ec, "all")
	if err != nil {
		return nil, false, err
	}
	entries := make([]config.MarketplaceEntry, 0, len(endpoints))
	for _, endpoint := range endpoints {
		entries = append(entries, endpoint.MarketplaceEntry)
	}
	if err := config.ValidateMarketplaceURLs(entries, ec.AllowedMarketplaces); err != nil {
		return nil, false, err
	}
	cache := opts.Cache
	if cache == nil {
		cache = DefaultDiscoveryCache()
	}
	if err := cache.maintain(entries); err != nil {
		slog.Warn("marketplace discovery cache maintenance failed", "error", err)
	}
	outcomes := make([]DiscoveryOutcome, 0, len(endpoints))
	needsNetwork := false
	for _, endpoint := range endpoints {
		cached, hit := cache.Load(endpoint.MarketplaceEntry, query, opts.Kind, limit)
		if hit {
			outcomes = append(outcomes, cached)
		}
		if !hit || cached.CacheState == DiscoveryCacheStale {
			needsNetwork = true
		}
	}
	slices.SortFunc(outcomes, func(a, b DiscoveryOutcome) int {
		if c := cmp.Compare(displayName(a.Endpoint), displayName(b.Endpoint)); c != 0 {
			return c
		}
		return cmp.Compare(a.Endpoint.URL, b.Endpoint.URL)
	})
	return outcomes, needsNetwork, nil
}

// DiscoverUpdates returns deterministic cumulative snapshots as endpoint
// outcomes arrive. Fresh cache hits do not issue network requests; stale hits
// are retained if revalidation fails.
func DiscoverUpdates(ctx context.Context, ec config.EffectiveConfig, query string, opts DiscoveryOptions) (<-chan []DiscoveryOutcome, error) {
	started := time.Now()
	query = normalizeDiscoveryQuery(query)
	limit := opts.Limit
	if limit <= 0 || limit > DiscoveryPageLimit {
		limit = DiscoveryPageLimit
	}
	// Only live discovery may bind resource-free authored registrations. The
	// offline selector above continues to require an already acquired identity.
	endpoints, err := resolveMarketplaceEndpointsWithAuthoredRegistrations(ec, "all", true)
	if err != nil {
		return nil, err
	}
	entries := make([]config.MarketplaceEntry, 0, len(endpoints))
	for _, ep := range endpoints {
		entries = append(entries, ep.MarketplaceEntry)
	}
	if err := config.ValidateMarketplaceURLs(entries, ec.AllowedMarketplaces); err != nil {
		return nil, err
	}
	slog.Info("marketplace discovery started", "endpoints", len(endpoints), "query_empty", query == "", "limit", limit)
	cache := opts.Cache
	if cache == nil {
		cache = DefaultDiscoveryCache()
	}
	if err := cache.maintain(entries); err != nil {
		slog.Warn("marketplace discovery cache maintenance failed", "error", err)
	}
	updates := make(chan []DiscoveryOutcome, len(endpoints)+1)
	results := make(chan DiscoveryOutcome, len(endpoints))
	semaphore := make(chan struct{}, MaxSearchConcurrency)
	outcomes := make([]DiscoveryOutcome, 0, len(endpoints))
	pending := 0
	for _, endpoint := range endpoints {
		ep := endpoint.MarketplaceEntry
		cached, hit := cache.Load(ep, query, opts.Kind, limit)
		if hit {
			outcomes = append(outcomes, cached)
			if cached.CacheState == DiscoveryCacheFresh {
				continue
			}
		}
		pending++
		go func(endpoint searchEndpoint, cached DiscoveryOutcome, hit bool) {
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				if hit {
					cached.Err = ctx.Err()
					results <- cached
				} else {
					results <- DiscoveryOutcome{Endpoint: endpoint.MarketplaceEntry, CacheState: DiscoveryCacheMiss, Err: ctx.Err()}
				}
				return
			}
			out, fetchErr := discoverOne(ctx, endpoint.MarketplaceEntry, endpoint.verifyInstanceID, query, opts, limit)
			if fetchErr != nil {
				if hit {
					cached.Err = fetchErr
					results <- cached
				} else {
					results <- DiscoveryOutcome{Endpoint: endpoint.MarketplaceEntry, CacheState: DiscoveryCacheMiss, Err: fetchErr}
				}
				return
			}
			out.ObservedAt = cache.now()
			if err := cache.Store(out, query, opts.Kind, limit); err != nil {
				slog.Warn("marketplace discovery cache write failed", "marketplace", displayName(endpoint.MarketplaceEntry), "error", err)
			}
			results <- out
		}(endpoint, cached, hit)
	}
	go func() {
		defer close(updates)
		if len(outcomes) > 0 {
			updates <- sortedDiscoveryOutcomes(outcomes)
		}
		for range pending {
			outcomes = upsertDiscoveryOutcome(outcomes, <-results)
			updates <- sortedDiscoveryOutcomes(outcomes)
		}
		logDiscoveryCompletion(started, outcomes)
	}()
	return updates, nil
}

// Discover returns the final deterministic endpoint-granular snapshot.
func Discover(ctx context.Context, ec config.EffectiveConfig, query string, opts DiscoveryOptions) ([]DiscoveryOutcome, error) {
	updates, err := DiscoverUpdates(ctx, ec, query, opts)
	if err != nil {
		return nil, err
	}
	var outcomes []DiscoveryOutcome
	for update := range updates {
		outcomes = update
	}
	return outcomes, nil
}

func validateDiscoveryPage(items []mpapi.ResourceListItem, total, limit int) error {
	if limit <= 0 || limit > DiscoveryPageLimit || total < 0 || len(items) > limit || len(items) > total {
		return errors.New("discovery page exceeds bounds")
	}
	return validateAPISearchResponse(SearchResponse{Items: items, Total: total, Limit: limit}, SearchRequest{Limit: limit})
}

func upsertDiscoveryOutcome(outcomes []DiscoveryOutcome, update DiscoveryOutcome) []DiscoveryOutcome {
	for i := range outcomes {
		if outcomes[i].Endpoint.URL == update.Endpoint.URL && outcomes[i].Endpoint.InstanceID == update.Endpoint.InstanceID {
			outcomes[i] = update
			return outcomes
		}
	}
	return append(outcomes, update)
}

func sortedDiscoveryOutcomes(outcomes []DiscoveryOutcome) []DiscoveryOutcome {
	sorted := slices.Clone(outcomes)
	slices.SortFunc(sorted, func(a, b DiscoveryOutcome) int {
		if c := cmp.Compare(displayName(a.Endpoint), displayName(b.Endpoint)); c != 0 {
			return c
		}
		return cmp.Compare(a.Endpoint.URL, b.Endpoint.URL)
	})
	return sorted
}

func logDiscoveryCompletion(started time.Time, outcomes []DiscoveryOutcome) {
	var failures, results, truncated, miss, fresh, stale int
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			failures++
		}
		results += len(outcome.Items)
		if outcome.Truncated {
			truncated++
		}
		switch outcome.CacheState {
		case DiscoveryCacheMiss:
			miss++
		case DiscoveryCacheFresh:
			fresh++
		case DiscoveryCacheStale:
			stale++
		}
	}
	slog.Info("marketplace discovery completed", "endpoints", len(outcomes), "failures", failures, "results", results, "truncated_endpoints", truncated, "cache_miss", miss, "cache_fresh", fresh, "cache_stale", stale, "duration_ms", time.Since(started).Milliseconds())
}

func discoverOne(ctx context.Context, ep config.MarketplaceEntry, verify bool, query string, opts DiscoveryOptions, limit int) (DiscoveryOutcome, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultSearchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, err := NewClient(ep.URL, WithUserAgent(opts.UserAgent))
	if err != nil {
		return DiscoveryOutcome{}, err
	}
	if verify {
		ep, err = acquireMarketplaceIdentity(ctx, client, ep)
		if err != nil {
			return DiscoveryOutcome{}, err
		}
	} else if _, err := client.WellKnown(ctx); err != nil {
		return DiscoveryOutcome{}, fmt.Errorf("well-known document: %w", err)
	}
	resp, err := client.Search(ctx, SearchRequest{Query: query, Kind: opts.Kind, Limit: limit})
	if err != nil {
		return DiscoveryOutcome{}, err
	}
	items := slices.Clone(resp.Items)
	return DiscoveryOutcome{Endpoint: ep, Items: items, Total: resp.Total, Truncated: resp.Total > len(items), CacheState: DiscoveryCacheMiss, ObservedAt: time.Now()}, nil
}
