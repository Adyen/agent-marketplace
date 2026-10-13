package localapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	inspectiontest "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/inspection"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func serveHarness(t *testing.T, api *API, bootstrapFields map[string]string) {
	t.Helper()
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()
	serveHarnessAt(t, loopback.URL, api.Credential(), bootstrapFields)
}

func serveHarnessAt(t *testing.T, serverURL, credential string, bootstrapFields map[string]string) {
	t.Helper()
	bootstrap := os.NewFile(3, "bootstrap")
	if bootstrap == nil {
		t.Fatal("fd 3 bootstrap pipe is required")
	}
	defer func() { _ = bootstrap.Close() }()

	bootstrapFields["url"] = serverURL
	bootstrapFields["credential"] = credential
	line, err := json.Marshal(bootstrapFields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Minute)
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		buf := make([]byte, 1)
		for {
			if _, err := bootstrap.Read(buf); err != nil {
				return
			}
		}
	}()
	select {
	case <-closed:
	case <-deadline:
	}
}

type work39FinalEventBarrier struct {
	reached     chan struct{}
	released    chan struct{}
	reachedOnce sync.Once
	releaseOnce sync.Once
}

func newWork39FinalEventBarrier() *work39FinalEventBarrier {
	return &work39FinalEventBarrier{reached: make(chan struct{}), released: make(chan struct{})}
}

func (b *work39FinalEventBarrier) hold(ctx context.Context) bool {
	b.reachedOnce.Do(func() { close(b.reached) })
	select {
	case <-b.released:
		return true
	case <-ctx.Done():
		return false
	}
}

func (b *work39FinalEventBarrier) release() {
	b.releaseOnce.Do(func() { close(b.released) })
}

func serveWork39Harness(t *testing.T, api *API, bootstrapFields map[string]string) {
	t.Helper()
	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()
	target, err := url.Parse(loopback.URL)
	if err != nil {
		t.Fatal(err)
	}
	barrier := newWork39FinalEventBarrier()
	backend := httputil.NewSingleHostReverseProxy(target)
	backend.Transport = &work39LoopbackTransport{base: http.DefaultTransport, target: target, barrier: barrier}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_work39/wait-final" && r.Method == http.MethodGet:
			select {
			case <-barrier.reached:
				w.WriteHeader(http.StatusNoContent)
			case <-r.Context().Done():
			}
		case r.URL.Path == "/_work39/release-final" && r.Method == http.MethodPost:
			barrier.release()
			w.WriteHeader(http.StatusNoContent)
		default:
			backend.ServeHTTP(w, r)
		}
	}))
	defer proxy.Close()
	bootstrapFields["resourceActionBarrierWaitURL"] = proxy.URL + "/_work39/wait-final"
	bootstrapFields["resourceActionBarrierReleaseURL"] = proxy.URL + "/_work39/release-final"
	serveHarnessAt(t, proxy.URL, api.Credential(), bootstrapFields)
}

type work39LoopbackTransport struct {
	base    http.RoundTripper
	target  *url.URL
	barrier *work39FinalEventBarrier
}

func (t *work39LoopbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	outbound := request.Clone(request.Context())
	outbound.Header = request.Header.Clone()
	outbound.URL.Scheme = t.target.Scheme
	outbound.URL.Host = t.target.Host
	outbound.Host = t.target.Host
	outbound.Header.Set("Origin", t.target.String())
	outbound.RequestURI = ""
	response, err := t.base.RoundTrip(outbound)
	if err == nil && request.URL.Path == "/api/events" {
		response.Body = &work39FinalEventBody{
			body:    response.Body,
			reader:  bufio.NewReader(response.Body),
			context: request.Context(),
			barrier: t.barrier,
		}
	}
	return response, err
}

type work39FinalEventBody struct {
	body    io.ReadCloser
	reader  *bufio.Reader
	context context.Context
	barrier *work39FinalEventBarrier
	pending []byte
}

func (b *work39FinalEventBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(b.pending) == 0 {
		frame, err := b.nextFrame()
		if len(frame) == 0 && err != nil {
			return 0, err
		}
		if work39FinalMutationEvent(frame) && !b.barrier.hold(b.context) {
			return 0, b.context.Err()
		}
		b.pending = frame
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}

func (b *work39FinalEventBody) nextFrame() ([]byte, error) {
	var frame []byte
	for {
		line, err := b.reader.ReadBytes('\n')
		frame = append(frame, line...)
		if bytes.HasSuffix(frame, []byte("\n\n")) || bytes.HasSuffix(frame, []byte("\r\n\r\n")) {
			return frame, nil
		}
		if err != nil {
			return frame, err
		}
	}
}

func (b *work39FinalEventBody) Close() error {
	return b.body.Close()
}

func work39FinalMutationEvent(frame []byte) bool {
	data := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(frame), []byte("data:")))
	var event struct {
		Type    string `json:"type"`
		Payload struct {
			Stage            string `json:"stage"`
			SettledOperation *struct {
				Kind string `json:"kind"`
			} `json:"settledOperation"`
		} `json:"payload"`
	}
	if json.Unmarshal(data, &event) != nil {
		return false
	}
	return event.Type == "snapshot" && event.Payload.Stage == "final" && event.Payload.SettledOperation != nil && event.Payload.SettledOperation.Kind == string(session.OperationMutation)
}

func TestLocalAPIHarness(t *testing.T) {
	if os.Getenv("AM_LOCALAPI_E2E") != "1" {
		t.Skip("real-core harness; enabled only by the Playwright E2E driver")
	}
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	serveHarness(t, api, map[string]string{"repo": ws.RepoRoot()})
}

func TestLocalAPIUserHarness(t *testing.T) {
	if os.Getenv("AM_LOCALAPI_USER_E2E") != "1" {
		t.Skip("user-scope harness; enabled only by the Playwright E2E driver")
	}
	fixture := os.Getenv("AM_LOCALAPI_USER_FIXTURE")
	var discoveryCounterWriteFailed atomic.Bool
	home, repo := newUserAPIFixture(t, fixture != "first-run" && fixture != "work39-resource-actions" && fixture != "work40-resource-actions")
	var orgConfigPath string
	if fixture == "first-run" {
		if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if fixture == "harnesses" {
		userConfig := filepath.Join(home, ".agents", "config.yaml")
		source := filepath.Join(home, "fixture-source")
		writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
		writeFile(t, userConfig,
			"apiVersion: agent-marketplace/v4\nharnesses: [devin, e2e-future-harness]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true}\n")
		orgPath := filepath.Join(t.TempDir(), "org.yaml")
		orgSource := filepath.Join(t.TempDir(), "org-regs")
		writeFile(t, filepath.Join(orgSource, "orgskill", "SKILL.md"), fixtureSkillBody)
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  orgdir: {directory: "+orgSource+"}\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		orgConfigPath = orgPath
	}
	if fixture == "legacy" {
		source := filepath.Join(home, "fixture-source")
		writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
		writeLegacyUserRoot(t, home, legacyUserRootBody("agent-marketplace/v2", source))
	}
	bootstrap := map[string]string{
		"home":      home,
		"repo":      repo,
		"codexHome": os.Getenv("CODEX_HOME"),
		"xdgConfig": os.Getenv("XDG_CONFIG_HOME"),
		"xdgState":  os.Getenv("XDG_STATE_HOME"),
		"xdgCache":  os.Getenv("XDG_CACHE_HOME"),
	}
	var work36Protected []discoveryFixtureFile
	if fixture == "work36-inspection" {
		work36Protected = setupWork36InspectionFixture(t, home, bootstrap)
		orgConfigPath = bootstrap["orgConfig"]
	}
	var work37Fixture *work37InstalledFixture
	if fixture == "work37-installed" || fixture == "work37-installed-missing" {
		work37Fixture = setupWork37InstalledFixture(t, home, bootstrap, true)
		orgConfigPath = bootstrap["orgConfig"]
	}
	var work39Fixture *work39MarketplaceFixture
	if fixture == "work39-resource-actions" {
		mode := work39InitialMode(os.Getenv("AM_LOCALAPI_USER_INITIAL_MODE"))
		required := os.Getenv("AM_LOCALAPI_USER_ORG_REQUIRED") == "1"
		risk := os.Getenv("AM_LOCALAPI_USER_RISK") == "1"
		work39Fixture = setupWork39MarketplaceFixture(t, home, repo, bootstrap, mode, required, risk)
		orgConfigPath = work39Fixture.orgConfigPath
		bootstrap["resourceActionUserConfig"] = work39Fixture.userConfigPath
		bootstrap["resourceActionUserLock"] = work39Fixture.userLockPath
		bootstrap["resourceActionOrgConfig"] = work39Fixture.orgConfigPath
		bootstrap["resourceActionOrgLock"] = work39Fixture.orgLockPath
		bootstrap["resourceActionMaterialized"] = work39Fixture.materializedPath
		bootstrap["resourceActionUnrelatedMaterialized"] = work39Fixture.unrelatedMaterialized
	}
	var work40Fixture *work40DirectoryFixture
	if fixture == "work40-resource-actions" {
		mode := work40DirectoryMode(os.Getenv("AM_LOCALAPI_USER_INITIAL_MODE"))
		work40Fixture = setupWork40DirectoryFixture(t, home, repo, bootstrap, mode)
		orgConfigPath = work40Fixture.orgConfigPath
	}
	if orgConfigPath != "" {
		bootstrap["orgConfig"] = orgConfigPath
	}
	if fixture == "sources" || fixture == "marketplace" || fixture == "sources-git" {
		gitSource := fixture == "sources-git"
		userDir := filepath.Join(home, ".agents")
		keepDir := filepath.Join(home, "keep-source")
		fragDir := filepath.Join(home, "frag-source")
		addDir := filepath.Join(home, "added-source")
		writeFile(t, filepath.Join(keepDir, "solo", "SKILL.md"), fixtureSkillBody)
		writeFile(t, filepath.Join(fragDir, "frag", "SKILL.md"), fixtureSkillBody)
		fragResources := "{all: true}"
		rootLogging := ""
		keepResources := "{all: true}"
		if gitSource {
			fragResources = "{all: true, harnesses: [claude]}"
			rootLogging = "logging: {level: debug}\n"
			keepResources = "{all: true, harnesses: [devin]}"
		} else {
			writeFile(t, filepath.Join(addDir, "added", "SKILL.md"), fixtureSkillBody)
		}
		writeFile(t, filepath.Join(userDir, "fragment.yaml"),
			"sources:\n  frag: {directory: "+fragDir+"}\nresources:\n  frag: "+fragResources+"\n")
		writeFile(t, filepath.Join(userDir, "config.yaml"),
			"apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\n"+rootLogging+"sources:\n  keep: {directory: "+keepDir+"}\nresources:\n  keep: "+keepResources+"\n")
		orgPath := filepath.Join(t.TempDir(), "org.yaml")
		orgSource := filepath.Join(t.TempDir(), "org-regs")
		writeFile(t, filepath.Join(orgSource, "orgskill", "SKILL.md"), fixtureSkillBody)
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  orgdir: {directory: "+orgSource+"}\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		bootstrap["orgConfig"] = orgPath
		if gitSource {
			bootstrap["gitSourceURL"] = "file://" + filepath.ToSlash(newBareGitSource(t))
		} else {
			bootstrap["addSourceDir"] = addDir
		}
	}
	if fixture == "marketplace" {
		marketplace := newMarketplaceEndpointTestServer(t, "/playwright/catalog", "playwright-marketplace", "", 0)
		bootstrap["marketplaceEndpoint"] = marketplace.endpoint
	}
	if fixture == "first-run" {
		addDir := filepath.Join(home, "optional-directory")
		writeFile(t, filepath.Join(addDir, "queued", "SKILL.md"), fixtureSkillBody)
		bootstrap["addSourceDir"] = addDir
		marketplace := newMarketplaceEndpointTestServer(t, "/playwright/first-run", "first-run-marketplace", "", 0)
		marketplace.failNextStatus(http.StatusInternalServerError)
		bootstrap["marketplaceEndpoint"] = marketplace.endpoint
	}
	if fixture == "navigation" {
		source := filepath.Join(home, "navigation-source")
		for i := 0; i < 60; i++ {
			name := fmt.Sprintf("skill-%03d", i)
			body := fmt.Sprintf("---\nname: %s\ndescription: Navigation fixture %s\n---\n# %s\n", name, name, name)
			writeFile(t, filepath.Join(source, name, "SKILL.md"), body)
		}
		writeFile(t, filepath.Join(home, ".agents", "config.yaml"),
			"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  navigation: {directory: "+source+"}\nresources:\n  navigation: {all: true}\n")
		bootstrap["navigationSourceDir"] = source
	}
	if fixture == "work35-discovery" {
		setupWork35DiscoveryFixture(t, bootstrap, &discoveryCounterWriteFailed)
	}
	lifecycle := session.RuntimeLifecycle()
	if fixture == "held-sync" {
		markerDir := t.TempDir()
		started := filepath.Join(markerDir, "sync-started")
		release := filepath.Join(markerDir, "sync-release")
		bootstrap["syncStarted"] = started
		bootstrap["syncRelease"] = release
		inner := lifecycle.Sync
		lifecycle.Sync = func(ctx context.Context, ws pipeline.Workspace) ([]error, error) {
			if err := os.WriteFile(started, []byte("1"), 0o644); err != nil {
				return nil, err
			}
			deadline := time.Now().Add(90 * time.Second)
			for {
				if _, err := os.Stat(release); err == nil {
					break
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if time.Now().After(deadline) {
					return nil, fmt.Errorf("held-sync release file never appeared")
				}
				time.Sleep(20 * time.Millisecond)
			}
			return inner(ctx, ws)
		}
	}
	if fixture == "release" {
		orgPath := filepath.Join(t.TempDir(), "org.yaml")
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		bootstrap["orgConfig"] = orgPath
	}
	apiCtx := context.Background()
	var telemetryHarness *localTelemetryHarness
	if mode := os.Getenv("AM_LOCALAPI_TELEMETRY_MODE"); mode != "" {
		orgPath := filepath.Join(t.TempDir(), "org.yaml")
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\ngui:\n  version_check: false\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		bootstrap["orgConfig"] = orgPath
		var telemetrySetupErr error
		apiCtx, telemetryHarness, telemetrySetupErr = newLocalTelemetryHarness(t, home, mode, os.Getenv("AM_LOCALAPI_TELEMETRY_CAPTURE"))
		if telemetrySetupErr != nil {
			t.Fatal("setup private telemetry harness")
		}
	}
	api, err := NewUser(apiCtx, home, lifecycle, nil, WithVersion(os.Getenv("AM_LOCALAPI_RELEASE_VERSION")))
	if err != nil {
		if telemetryHarness != nil {
			telemetryHarness.finish(t)
		}
		t.Fatal(err)
	}
	if telemetryHarness == nil {
		defer api.Close()
	} else {
		defer func() {
			api.Close()
			telemetryHarness.finish(t)
		}()
	}
	if work40Fixture != nil {
		assertWork40ProtectedFilesUnchanged(t, work40Fixture.protected)
	}
	if work37Fixture != nil {
		events := streamEvents(t, api)
		waitForInspectionFinal(t, api, events)
		api.mu.Lock()
		rowID := ""
		generation := api.currentG
		for _, row := range api.current.Rows {
			if row.Name == "review" && !row.Discovery {
				rowID = row.ID
				break
			}
		}
		api.mu.Unlock()
		if rowID == "" {
			t.Fatal("Work37 NewUser pipeline did not publish the installed skill")
		}
		row, capturedGeneration, code := api.visibleResourceRow(rowID, session.MarketplaceInstalledSkillFiles)
		if code != "" || capturedGeneration != generation || row.Artifact.Path != work37Fixture.stagePath {
			t.Fatalf("Work37 installed row lookup = generation %d/%d path %q code %q", capturedGeneration, generation, row.Artifact.Path, code)
		}
		if fixture == "work37-installed-missing" {
			if err := os.Remove(filepath.Join(row.Artifact.Path, "notes.md")); err != nil {
				t.Fatal(err)
			}
		}
		work37Fixture.protected = captureWork37InstalledProtected(t, home, work37Fixture, row)
		work37Fixture.baselineRequests = work37Fixture.requests.Load()
	}
	if marker := os.Getenv("AM_LOCALAPI_RELEASE_MARK"); marker != "" {
		api.releaseClient.Transport = releaseHarnessTransport(marker, os.Getenv("AM_LOCALAPI_RELEASE_STATUS"), os.Getenv("AM_LOCALAPI_RELEASE_BODY"))
	}
	if work39Fixture != nil || work40Fixture != nil {
		serveWork39Harness(t, api, bootstrap)
	} else {
		serveHarness(t, api, bootstrap)
	}
	if work39Fixture != nil {
		assertWork39ProtectedFilesUnchangedExcept(t, work39Fixture.protected, work39Fixture.userConfigPath, work39Fixture.userLockPath, work39Fixture.materializedPath)
	}
	if work40Fixture != nil {
		if work40Fixture.mode == work40RootOwned {
			assertWork40ProtectedFilesUnchanged(t, work40Fixture.protected, work40Fixture.userConfigPath)
			work40UserRootMode(t, work40Fixture, work40Fixture.protected)
		} else {
			assertWork40ProtectedFilesUnchanged(t, work40Fixture.protected)
		}
	}
	if fixture == "work36-inspection" {
		assertInspectionFilesUnchanged(t, work36Protected)
	}
	if work37Fixture != nil {
		assertInspectionFilesUnchanged(t, work37Fixture.protected)
		assertWork37CacheTreeUnchanged(t, work37Fixture)
		if got := work37Fixture.requests.Load(); got != work37Fixture.baselineRequests {
			t.Fatalf("Work37 installed-file browsing made SDK requests: before=%d after=%d", work37Fixture.baselineRequests, got)
		}
	}
	if discoveryCounterWriteFailed.Load() {
		t.Fatal("Work35 discovery search counter write failed")
	}
}

func setupWork35DiscoveryFixture(t *testing.T, bootstrap map[string]string, counterWriteFailed *atomic.Bool) {
	t.Helper()
	markerDir := t.TempDir()
	discoveryStarted := filepath.Join(markerDir, "acquisition-started")
	discoveryRelease := filepath.Join(markerDir, "acquisition-release")
	searchCountPath := filepath.Join(markerDir, "search-count")
	firstSearchStarted := filepath.Join(markerDir, "first-search-started")
	firstSearchCanceled := filepath.Join(markerDir, "first-search-canceled")
	secondSearchStarted := filepath.Join(markerDir, "second-search-started")
	if err := os.WriteFile(searchCountPath, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	const instanceID = "work35-marketplace"
	var server *httptest.Server
	var searchCount atomic.Int32
	var countMu sync.Mutex
	writeCount := func() {
		countMu.Lock()
		count := searchCount.Add(1)
		err := os.WriteFile(searchCountPath, []byte(strconv.Itoa(int(count))), 0o600)
		countMu.Unlock()
		if err != nil {
			counterWriteFailed.Store(true)
		}
	}
	writeMarker := func(path string) {
		_ = os.WriteFile(path, []byte("1"), 0o600)
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
		case r.URL.Path == "/api/v1/search":
			writeCount()
			switch r.URL.Query().Get("q") {
			case "alpha":
				writeMarker(firstSearchStarted)
				select {
				case <-r.Context().Done():
					writeMarker(firstSearchCanceled)
				case <-work35ReleaseSignal(r.Context(), discoveryRelease):
				}
				return
			case "beta":
				writeMarker(secondSearchStarted)
				item := mpapi.ResourceListItem{
					RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "remote-beta",
					Revision: "sha256:" + strings.Repeat("d", 64), ContentDigest: "sha256:" + strings.Repeat("d", 64),
					Availability: mpapi.AvailabilityAvailable, Score: 1,
				}
				_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{item}, Total: 1, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
			default:
				_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{}, Total: 0, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
			}
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			writeMarker(discoveryStarted)
			select {
			case <-r.Context().Done():
			case <-work35ReleaseSignal(r.Context(), discoveryRelease):
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	organization := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: " + server.URL + "}\nresources:\n  mp: {include: [tools/skill/pending]}\n"
	if err := os.WriteFile(orgPath, []byte(organization), 0o600); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	bootstrap["orgConfig"] = orgPath
	bootstrap["marketplaceEndpoint"] = server.URL
	bootstrap["discoveryAcquisitionStarted"] = discoveryStarted
	bootstrap["discoveryRelease"] = discoveryRelease
	bootstrap["discoverySearchCount"] = searchCountPath
	bootstrap["discoveryFirstSearchStarted"] = firstSearchStarted
	bootstrap["discoveryFirstSearchCanceled"] = firstSearchCanceled
	bootstrap["discoverySecondSearchStarted"] = secondSearchStarted

	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: instanceID, Alias: "mp"}
	item := mpapi.ResourceListItem{
		RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "cached-resource",
		Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64),
		Availability: mpapi.AvailabilityAvailable, Score: 2,
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatalf("seed Work35 discovery cache: %v", err)
	}
}

func setupWork36InspectionFixture(t *testing.T, home string, bootstrap map[string]string) []discoveryFixtureFile {
	t.Helper()
	fixture := inspectiontest.NewFixtureContent(t,
		[]byte("# Grill inspection\n\nLiteral <script>text</script> remains inert."),
		map[string][]byte{
			"notes.md":   []byte("# Notes\n\nSupporting inspection."),
			"logo.png":   []byte("\x89PNG\r\n\x1a\nfake-image"),
			"unsafe.svg": []byte("<svg onload=alert(1)></svg>"),
		}, nil)
	if !fixture.CorruptNextSupportBody("notes.md") {
		t.Fatal("inspection support member missing from verified fixture")
	}
	endpoint := fixture.Endpoint()
	endpoint.Alias = "mp"
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	orgConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: " + endpoint.URL + "}\n"
	if err := os.WriteFile(orgPath, []byte(orgConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	if err := config.ValidateOrganizationTrustUnit(orgPath, true, true); err != nil {
		t.Fatalf("validate trusted Work36 Marketplace endpoint: %v", err)
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: endpoint, Items: []mpapi.ResourceListItem{fixture.Item()}, Total: 1, ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatalf("seed fresh Work36 discovery page: %v", err)
	}
	bootstrap["orgConfig"] = orgPath
	bootstrap["marketplaceEndpoint"] = endpoint.URL

	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	userLockPaths, err := config.ResolveConfigLockPathsTolerateMissing(userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	orgLockPaths, err := config.ResolveConfigLockPathsTolerateMissing(orgPath)
	if err != nil {
		t.Fatal(err)
	}
	materializedPath := filepath.Join(home, ".devin", "skills", "work36-protected", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(materializedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materializedPath, []byte("work36 protected materialized bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	protected := make([]discoveryFixtureFile, 0, 5)
	for _, path := range []string{userConfigPath, orgPath, userLockPaths.CanonicalLock, orgLockPaths.CanonicalLock, materializedPath} {
		protected = append(protected, captureDiscoveryFixtureFile(t, path))
	}
	return protected
}

func work35ReleaseSignal(ctx context.Context, path string) <-chan struct{} {
	released := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(path); err == nil {
				close(released)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return released
}

func releaseHarnessTransport(marker, statusEnv, bodyPath string) http.RoundTripper {
	status := http.StatusOK
	if statusEnv != "" {
		if parsed, err := strconv.Atoi(statusEnv); err == nil {
			status = parsed
		}
	}
	return releaseRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		file, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		if _, err := file.WriteString(r.Method + " " + r.URL.String() + "\n"); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
		var body []byte
		if bodyPath != "" {
			if data, err := os.ReadFile(bodyPath); err == nil {
				body = data
			}
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d", status),
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	})
}
