package marketplace

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
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
	endpoints, err := resolveMarketplaceEndpoints(ec, opts.Marketplace)
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
		if c := cmp.Compare(a.SourceSlug, b.SourceSlug); c != 0 {
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

	wellKnown, err := c.WellKnown(ctx)
	if err != nil {
		return nil, fmt.Errorf("well-known document: %w", err)
	}
	if verifyInstanceID {
		if strings.TrimSpace(ep.InstanceID) == "" {
			return nil, fmt.Errorf("configured marketplace instance ID is required")
		}
		if wellKnown.InstanceID != ep.InstanceID {
			slog.Warn("marketplace search endpoint instance ID mismatch", "url", ep.URL, "endpoint_instance_id", wellKnown.InstanceID, "configured_instance_id", ep.InstanceID)
			return nil, fmt.Errorf("instance ID mismatch: endpoint %q, configured %q", wellKnown.InstanceID, ep.InstanceID)
		}
		slog.Debug("marketplace search endpoint instance ID verified", "url", ep.URL, "instance_id", ep.InstanceID)
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

func resolveMarketplaceEndpoints(ec config.EffectiveConfig, flag string) ([]searchEndpoint, error) {
	var entries []config.MarketplaceEntry
	if ec.Merged != nil {
		entries = ec.Merged.Marketplaces
	}

	flag = strings.TrimSpace(flag)
	switch flag {
	case "", "all":
		if len(entries) == 0 {
			return nil, fmt.Errorf("no marketplaces configured; add one to your config or use --marketplace <url>")
		}
		endpoints := make([]searchEndpoint, 0, len(entries))
		for _, ep := range entries {
			endpoints = append(endpoints, searchEndpoint{MarketplaceEntry: ep, verifyInstanceID: true})
		}
		return endpoints, nil
	}

	// Try to match by alias or exact URL.
	for _, ep := range entries {
		if ep.Alias == flag || ep.URL == flag {
			return []searchEndpoint{{MarketplaceEntry: ep, verifyInstanceID: true}}, nil
		}
	}

	// Treat as an ad-hoc URL. It has no configured instance ID to verify, but
	// still fetches the well-known document before issuing the search request.
	if strings.HasPrefix(flag, "http://") || strings.HasPrefix(flag, "https://") {
		return []searchEndpoint{{
			MarketplaceEntry: config.MarketplaceEntry{URL: flag, Alias: flag},
		}}, nil
	}

	return nil, fmt.Errorf("marketplace %q not found", flag)
}

func displayName(ep config.MarketplaceEntry) string {
	if ep.Alias != "" {
		return ep.Alias
	}
	u := strings.TrimSuffix(strings.TrimSpace(ep.URL), "/")
	if i := strings.LastIndexAny(u, "/:"); i != -1 {
		return u[i+1:]
	}
	return u
}

// Unwrap supports errors.Is/As over the wrapped error.
func (e EndpointError) Unwrap() error {
	return e.Err
}

var _ error = (EndpointError{})
