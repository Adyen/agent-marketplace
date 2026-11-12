package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type concurrentPinFixture struct {
	pin      config.MarketplaceResourcePin
	revision store.AssessedResourceRevision
	raw      []byte
	archive  string
}

func concurrentPinServer(t *testing.T, names []string, missing map[string]bool, delay func(string) time.Duration) (*httptest.Server, []config.MarketplaceResourcePin, *atomic.Int32) {
	t.Helper()
	fixtures := make(map[string]concurrentPinFixture, len(names))
	pins := make([]config.MarketplaceResourcePin, 0, len(names))
	for _, name := range names {
		artifact, err := hccartifact.FromIR(ir.NewSkill(name, name, "Concurrent Marketplace fixture.", []string{"user"}, nil, ir.SourceRef{}))
		if err != nil {
			t.Fatal(err)
		}
		artifact.Manifest.RegistryID, artifact.Manifest.RegistrySlug = "src", "source-slug"
		artifact.Manifest.Revision = "refs/heads/main"
		raw, archive, err := hccartifact.Encode(artifact)
		if err != nil {
			t.Fatal(err)
		}
		pin := config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: "source-slug", Kind: "skill", Name: name, Revision: artifact.Manifest.Revision, Digest: artifact.Manifest.ContentDigest}
		revision := assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: name},
			ContentDigest: store.RevisionDigest(pin.Digest), ArchiveDigest: store.ArchiveDigest(archive),
		})
		fixtures[name] = concurrentPinFixture{pin: pin, revision: revision, raw: raw, archive: archive}
		pins = append(pins, pin)
	}
	var current, peak atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, _ *http.Request) {
		writeTestRegistry(w, store.Registry{ID: "src", Slug: "source-slug", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/resources/"), "/")
		if len(parts) != 5 || parts[0] != "src" || parts[1] != "skill" || parts[3] != "revisions" {
			http.NotFound(w, r)
			return
		}
		fixture, ok := fixtures[parts[2]]
		if !ok || missing[parts[2]] || fixture.pin.Digest != parts[4] {
			http.NotFound(w, r)
			return
		}
		now := current.Add(1)
		for observed := peak.Load(); now > observed; observed = peak.Load() {
			if peak.CompareAndSwap(observed, now) {
				break
			}
		}
		if delay != nil {
			time.Sleep(delay(parts[2]))
		}
		current.Add(-1)
		_ = json.NewEncoder(w).Encode(fixture.revision)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		for _, fixture := range fixtures {
			if r.URL.Path == "/api/v1/artifacts/"+strings.Replace(fixture.archive, ":", "/", 1) {
				_, _ = w.Write(fixture.raw)
				return
			}
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, pins, &peak
}

func TestResolveWithPolicyResolvesLeafPinsWithBoundedConcurrency(t *testing.T) {
	names := make([]string, 16)
	for i := range names {
		names[i] = fmt.Sprintf("pin-%02d", i)
	}
	latency := 300 * time.Millisecond
	server, pins, peak := concurrentPinServer(t, names, nil, func(string) time.Duration { return latency })
	start := time.Now()
	resolved, errs := (&Resolver{Cache: NewCache(t.TempDir())}).ResolveWithPolicy(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: pins}}}, nil, resolver.MarketplaceResolveModeRefresh, nil)
	elapsed := time.Since(start)
	if len(errs) != 0 || len(resolved) != len(pins) {
		t.Fatalf("resolved=%d errors=%v", len(resolved), errs)
	}
	if elapsed >= 3*latency || peak.Load() <= 1 || peak.Load() > 8 {
		t.Fatalf("elapsed=%s peak=%d, want < %s and 1 < peak <= 8", elapsed, peak.Load(), 3*latency)
	}
	for i := range pins {
		if resolved[i].Name != pins[i].Name {
			t.Fatalf("resolved[%d].Name=%q, want %q", i, resolved[i].Name, pins[i].Name)
		}
	}
}

func TestResolveWithPolicyKeepsLeafResultsAndErrorsInConfigurationOrder(t *testing.T) {
	names := []string{"pin-0", "pin-1", "pin-2", "pin-3"}
	delays := map[string]time.Duration{"pin-0": 150 * time.Millisecond, "pin-1": 120 * time.Millisecond, "pin-2": 90 * time.Millisecond, "pin-3": 60 * time.Millisecond}
	server, pins, _ := concurrentPinServer(t, names, map[string]bool{"pin-1": true, "pin-3": true}, func(name string) time.Duration { return delays[name] })
	resolved, errs := (&Resolver{Cache: NewCache(t.TempDir())}).ResolveWithPolicy(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: pins}}}, nil, resolver.MarketplaceResolveModeRefresh, nil)
	if len(resolved) != 2 || resolved[0].Name != "pin-0" || resolved[1].Name != "pin-2" {
		t.Fatalf("resolved names = %v, want [pin-0 pin-2]", []string{resolved[0].Name, resolved[1].Name})
	}
	if len(errs) != 2 {
		t.Fatalf("errors = %v, want two pin errors", errs)
	}
	for i, name := range []string{"pin-1", "pin-3"} {
		var pinErr *PinError
		if !errors.As(errs[i], &pinErr) || pinErr.Pin.Name != name {
			t.Fatalf("errors[%d]=%v, want PinError for %s", i, errs[i], name)
		}
	}
}
