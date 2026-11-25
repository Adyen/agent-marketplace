package pipeline

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

type pinDelayTransport struct {
	next    http.RoundTripper
	delays  map[string]time.Duration
	current atomic.Int32
	peak    atomic.Int32
}

func (tr *pinDelayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	name := strings.TrimPrefix(req.URL.Path, "/api/v1/resources/source/skill/")
	delay, ok := tr.delays[name]
	if !ok {
		return tr.next.RoundTrip(req)
	}
	active := tr.current.Add(1)
	for observed := tr.peak.Load(); active > observed; observed = tr.peak.Load() {
		if tr.peak.CompareAndSwap(observed, active) {
			break
		}
	}
	defer tr.current.Add(-1)
	time.Sleep(delay)
	return tr.next.RoundTrip(req)
}

func TestPlanMarketplaceUpdatesUsesBoundedConcurrencyAndKeepsMissingPinPosition(t *testing.T) {
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	names := []string{"slow", "missing", "fast", "mid"}
	pins := make([]config.MarketplaceResourcePin, 0, len(names))
	for _, name := range names {
		digest := "sha256:" + strings.Repeat("f", 64)
		if name != "missing" {
			fake.addSkill(t, registry, name)
			digest = string(fake.resources["source/skill/"+name].revision.ContentDigest)
		}
		pins = append(pins, config.MarketplaceResourcePin{RegistryID: string(registry.ID), RegistrySlug: registry.Slug, Kind: "skill", Name: name, Revision: digest, Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror})
	}
	tr := &pinDelayTransport{next: http.DefaultTransport, delays: map[string]time.Duration{
		"slow": 150 * time.Millisecond, "missing": 120 * time.Millisecond, "fast": 90 * time.Millisecond, "mid": 60 * time.Millisecond,
	}}
	resolverMP := &marketplace.Resolver{HTTPClient: &http.Client{Transport: tr}, Cache: marketplace.NewCache(t.TempDir())}
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID, Source: config.RepoCommitted, Alias: "public", Resources: pins}
	ec := &config.EffectiveConfig{Merged: &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}}
	plans, err := planMarketplaceUpdates(context.Background(), UpdateOptions{RepoRoot: t.TempDir(), MarketplaceResolver: resolverMP}, ec)
	if err != nil {
		t.Fatalf("planMarketplaceUpdates: %v", err)
	}
	if len(plans) != len(names) || tr.peak.Load() <= 1 || tr.peak.Load() > 8 {
		t.Fatalf("plans=%d peak=%d, want %d plans and 1 < peak <= 8", len(plans), tr.peak.Load(), len(names))
	}
	for i, name := range names {
		if plans[i].OldPin.Name != name {
			t.Fatalf("plans[%d].OldPin.Name=%q, want %q", i, plans[i].OldPin.Name, name)
		}
		if name == "missing" && (!plans[i].Skipped || plans[i].Reason != marketplaceResourceMissingReason) {
			t.Fatalf("missing plan=%#v, want skipped missing-resource plan", plans[i])
		}
	}
}
