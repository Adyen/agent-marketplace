package marketplace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

const (
	// DefaultSearchTimeout is the per-endpoint timeout for federated search.
	DefaultSearchTimeout = 30 * time.Second

	// MaxSearchConcurrency limits concurrent Marketplace requests.
	MaxSearchConcurrency = 4

	// defaultSearchLimit mirrors the server default page size when the caller
	// does not request a limit.
	defaultSearchLimit = 100
)

// SearchOpts describes a federated search request across configured
// Marketplace endpoints.
type SearchOpts struct {
	// Marketplace is an alias, a URL, or the literal string "all". An empty
	// value means "all configured".
	Marketplace string
	Kind        string
	Limit       int
	Offset      int
	Timeout     time.Duration
	UserAgent   string
}

// SearchResult is one resource hit annotated with the marketplace it came from.
type SearchResult struct {
	Marketplace string `json:"marketplace"`
	mpapi.ResourceListItem
}

// EndpointError describes a failed request to one marketplace.
type EndpointError struct {
	Marketplace string
	URL         string
	Err         error
}

type searchEndpoint struct {
	config.MarketplaceEntry
	verifyInstanceID bool
}

func (e EndpointError) Error() string {
	return fmt.Sprintf("%s (%s): %v", e.Marketplace, e.URL, e.Err)
}

// FederatedSearch queries the configured Marketplaces for the given query and
// returns aggregated results. Per-endpoint failures are returned as
// EndpointError values so callers can decide whether to treat them as fatal.
func FederatedSearch(ctx context.Context, ec config.EffectiveConfig, query string, opts SearchOpts) ([]SearchResult, []EndpointError, error) {
	endpoints, err := resolveMarketplaceEndpointsWithAuthoredRegistrations(ec, opts.Marketplace, true)
	if err != nil {
		return nil, nil, err
	}
	if len(endpoints) == 0 {
		return nil, nil, fmt.Errorf("no marketplaces configured")
	}

	validationEntries := make([]config.MarketplaceEntry, 0, len(endpoints))
	for _, ep := range endpoints {
		validationEntries = append(validationEntries, ep.MarketplaceEntry)
	}
	if err := config.ValidateMarketplaceURLs(validationEntries, ec.AllowedMarketplaces); err != nil {
		return nil, nil, err
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultSearchTimeout
	}

	semaphore := make(chan struct{}, MaxSearchConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []SearchResult
	var endpointErrs []EndpointError

	for _, ep := range endpoints {
		wg.Add(1)
		go func(ep searchEndpoint) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			if ctx.Err() != nil {
				return
			}

			r, err := searchOne(ctx, ep.MarketplaceEntry, ep.verifyInstanceID, query, opts, timeout)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				endpointErrs = append(endpointErrs, EndpointError{
					Marketplace: displayName(ep.MarketplaceEntry),
					URL:         ep.URL,
					Err:         err,
				})
				return
			}
			results = append(results, r...)
		}(ep)
	}
	wg.Wait()

	// Sort by score descending, then stable tie-breakers.
	slices.SortFunc(results, func(a, b SearchResult) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(a.Marketplace, b.Marketplace); c != 0 {
			return c
		}
		if c := cmp.Compare(a.RegistrySlug, b.RegistrySlug); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})

	// Apply offset/limit post-aggregation.
	if opts.Offset > 0 {
		if opts.Offset >= len(results) {
			return nil, endpointErrs, nil
		}
		results = results[opts.Offset:]
	}
	if opts.Limit > 0 && opts.Limit < len(results) {
		results = results[:opts.Limit]
	}

	return results, endpointErrs, nil
}

func searchOne(ctx context.Context, ep config.MarketplaceEntry, verifyInstanceID bool, query string, opts SearchOpts, timeout time.Duration) ([]SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c, err := NewClient(ep.URL, WithUserAgent(opts.UserAgent))
	if err != nil {
		return nil, err
	}

	if verifyInstanceID {
		ep, err = acquireMarketplaceIdentity(ctx, c, ep)
		if err != nil {
			return nil, err
		}
		slog.Debug("marketplace search endpoint instance ID verified", "url", ep.URL, "instance_id", ep.InstanceID)
	} else if _, err := c.WellKnown(ctx); err != nil {
		return nil, fmt.Errorf("well-known document: %w", err)
	}

	// Ask each server for enough results to cover the requested offset + limit.
	requestLimit := opts.Limit
	if requestLimit == 0 {
		requestLimit = defaultSearchLimit
	}
	if opts.Offset > 0 {
		requestLimit += opts.Offset
	}

	resp, err := c.Search(ctx, SearchRequest{
		Query: query,
		Kind:  opts.Kind,
		Limit: requestLimit,
	})
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(resp.Items))
	for _, item := range resp.Items {
		results = append(results, SearchResult{
			Marketplace:      displayName(ep),
			ResourceListItem: item,
		})
	}
	return results, nil
}

// validateMarketplaceEndpointRegistrations keeps exact evidence on the config
// validator, while allowing resource-free authored declarations before discovery.
// Alias checks still span both shapes so an unresolved declaration cannot hide a
// conflicting exact endpoint identity.
func validateMarketplaceEndpointRegistrations(entries []config.MarketplaceEntry) []error {
	var exact []config.MarketplaceEntry
	var errs []error
	aliases := make(map[string]config.MarketplaceEntry)
	for i, configured := range entries {
		entry, err := canonicalMarketplaceEntry(configured)
		if err != nil {
			errs = append(errs, fmt.Errorf("marketplaces[%d]: %w", i, err))
			continue
		}
		if !unacquiredMarketplaceRegistration(entry) {
			exact = append(exact, entry)
		} else if entry.Alias != "" {
			if err := validateLocatorSegment("alias", entry.Alias); err != nil {
				errs = append(errs, fmt.Errorf("marketplaces[%d].%w", i, err))
			}
			if entry.Alias == "all" {
				errs = append(errs, fmt.Errorf("marketplaces[%d].alias %q is reserved for federated search", i, entry.Alias))
			}
		}
		if entry.Alias == "" {
			continue
		}
		if previous, ok := aliases[entry.Alias]; ok {
			if previous.URL != entry.URL || previous.InstanceID != entry.InstanceID || previous.Source == config.SourceUnset || entry.Source == config.SourceUnset {
				errs = append(errs, fmt.Errorf("marketplaces[%d].alias %q duplicates another Marketplace alias", i, entry.Alias))
			}
		} else {
			aliases[entry.Alias] = entry
		}
	}
	return append(errs, config.ValidateMarketplaceEntries(exact)...)
}

// Discovery also uses this selector for offline cache reads. Keep its exact
// identity requirement until live discovery can bind registrations safely.
func resolveMarketplaceEndpoints(ec config.EffectiveConfig, flag string) ([]searchEndpoint, error) {
	return resolveMarketplaceEndpointsWithAuthoredRegistrations(ec, flag, false)
}

func resolveMarketplaceEndpointsWithAuthoredRegistrations(ec config.EffectiveConfig, flag string, allowAuthored bool) ([]searchEndpoint, error) {
	var entries []config.MarketplaceEntry
	if ec.Merged != nil {
		entries = ec.Merged.Marketplaces
	}
	var errs []error
	if allowAuthored {
		errs = validateMarketplaceEndpointRegistrations(entries)
	} else {
		errs = config.ValidateMarketplaceEntries(entries)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configured Marketplace entries: %w", errors.Join(errs...))
	}

	flag = strings.TrimSpace(flag)
	var requestedURL string
	if strings.HasPrefix(strings.ToLower(flag), "http://") || strings.HasPrefix(strings.ToLower(flag), "https://") {
		var err error
		requestedURL, err = urlutil.NormalizeEndpointURL(flag)
		if err != nil {
			return nil, fmt.Errorf("invalid marketplace URL %q: %w", flag, err)
		}
	}

	switch flag {
	case "", "all":
		if len(entries) == 0 {
			return nil, fmt.Errorf("no marketplaces configured; add one to your config or use --marketplace <url>")
		}
		endpoints := make([]searchEndpoint, 0, len(entries))
		seen := make(map[string]struct{}, len(entries))
		for _, configured := range entries {
			ep, err := canonicalMarketplaceEntry(configured)
			if err != nil {
				return nil, fmt.Errorf("invalid configured marketplace URL %q: %w", configured.URL, err)
			}
			identity := ep.URL + "\x00" + ep.InstanceID
			if _, ok := seen[identity]; ok {
				continue
			}
			seen[identity] = struct{}{}
			endpoints = append(endpoints, searchEndpoint{MarketplaceEntry: ep, verifyInstanceID: true})
		}
		return endpoints, nil
	}

	// Try to match by alias or canonical endpoint URL. Matching URLs through
	// the shared normalizer makes scheme and trailing-slash variants identical.
	for _, configured := range entries {
		ep, err := canonicalMarketplaceEntry(configured)
		if err != nil {
			return nil, fmt.Errorf("invalid configured marketplace URL %q: %w", configured.URL, err)
		}
		if ep.Alias == flag || (requestedURL != "" && ep.URL == requestedURL) {
			return []searchEndpoint{{MarketplaceEntry: ep, verifyInstanceID: true}}, nil
		}
	}

	// Treat as an ad-hoc URL. It has no configured instance ID to verify, but
	// still fetches the well-known document before issuing the search request.
	if requestedURL != "" {
		return []searchEndpoint{{
			MarketplaceEntry: config.MarketplaceEntry{URL: requestedURL, Alias: requestedURL},
		}}, nil
	}

	return nil, fmt.Errorf("marketplace %q not found", flag)
}

func displayName(ep config.MarketplaceEntry) string {
	if ep.Alias != "" {
		return ep.Alias
	}
	if normalized, err := urlutil.NormalizeEndpointURL(ep.URL); err == nil {
		return normalized
	}
	return strings.TrimSuffix(strings.TrimSpace(ep.URL), "/")
}

// Unwrap supports errors.Is/As over the wrapped error.
func (e EndpointError) Unwrap() error {
	return e.Err
}

var _ error = (EndpointError{})
