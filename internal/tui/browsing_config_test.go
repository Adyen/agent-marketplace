package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestBrowsingConfigStageRendersCachedRowsBeforeBlockedAcquisition(t *testing.T) {
	repoRoot, homeDir := progressiveFixture(t)

	var searchRequests atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "http://"+r.Host))
		case r.URL.Path == "/api/v1/search":
			searchRequests.Add(1)
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			select {
			case <-release:
			case <-r.Context().Done():
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.Close)

	endpoint := config.MarketplaceEntry{URL: fixture.URL, InstanceID: "mp", Alias: "mp"}
	item := mpapi.ResourceListItem{
		RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "remote",
		Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64),
		Availability: mpapi.AvailabilityAvailable,
	}
	err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now()}, "", "", marketplace.DiscoveryPageLimit)
	if err != nil {
		t.Fatalf("seed browse cache: %v", err)
	}

	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "config.yaml")
	orgConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: " + fixture.URL + "}\nresources:\n  mp: {include: [tools/skill/remote]}\n"
	if err := os.WriteFile(orgPath, []byte(orgConfig), 0o600); err != nil {
		t.Fatalf("write organization config: %v", err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	t.Cleanup(swapLoadOrgConfig(func() (*config.Config, error) {
		return config.LoadOrganizationConfig()
	}))

	m := newTestModel()
	m.rows = nil
	m.authoritativeRows = nil
	m.flatItems = nil
	m.loading = true
	m.width, m.height = 120, 40
	generation := m.beginRequest(requestSnapshot)

	ctx, cancel := context.WithCancel(context.Background())
	stages := readSnapshotStages(ctx, repoRoot, homeDir, catalogLoadCached, generation)
	t.Cleanup(func() {
		unblock()
		cancel()
		done := make(chan struct{})
		go func() {
			for range stages {
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("stage producer did not finish after release and cancellation")
		}
	})

	var first snapshotLoadedMsg
	select {
	case first = <-stages:
	case <-time.After(30 * time.Second):
		t.Fatal("no config stage published while organization acquisition was blocked")
	}
	if first.provisionalCfg != nil {
		select {
		case first = <-stages:
		case <-time.After(30 * time.Second):
			t.Fatal("no browsing config stage after the provisional stage")
		}
	}
	if first.stage != snapshotStageConfig || first.mergedCfg == nil {
		t.Fatalf("first stage = %+v, want browsing config", first)
	}
	if len(first.mergedCfg.Marketplaces) != 1 {
		t.Fatalf("browsing config marketplaces = %#v", first.mergedCfg.Marketplaces)
	}
	entry := first.mergedCfg.Marketplaces[0]
	if entry.InstanceID != "mp" {
		t.Fatalf("browsing endpoint instance_id = %q, want well-known verified %q", entry.InstanceID, "mp")
	}
	if len(entry.Resources) != 0 {
		t.Fatalf("browsing endpoint carried %d provisional pins", len(entry.Resources))
	}

	produced := make(chan tea.Msg, 32)
	var run func(cmd tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		go func() {
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, inner := range batch {
					run(inner)
				}
				return
			}
			if msg != nil {
				produced <- msg
			}
		}()
	}
	updated, cmd := m.Update(first)
	m = updated.(Model)
	run(cmd)

	timeout := time.After(15 * time.Second)
	for len(m.rows) == 0 {
		select {
		case msg := <-produced:
			updated, next := m.Update(msg)
			m = updated.(Model)
			run(next)
		case <-timeout:
			t.Fatalf("no rows rendered while acquisition was blocked: %#v", m.rows)
		}
	}

	if len(m.authoritativeRows) != 0 {
		t.Fatal("authoritative inventory published before acquisition finished")
	}
	if searchRequests.Load() != 0 {
		t.Fatalf("browse hit the remote search route %d times despite a fresh cache", searchRequests.Load())
	}
	select {
	case extra, ok := <-stages:
		t.Fatalf("resolve advanced past acquisition while blocked: %+v closed=%t", extra, !ok)
	default:
	}
	view := m.View()
	if !strings.Contains(view, "remote") {
		t.Fatalf("rendered view lacks the cached resource row:\n%s", view)
	}
	if strings.Contains(view, "Loading resources") {
		t.Fatalf("view still shows the loading placeholder with rows present:\n%s", view)
	}
}
