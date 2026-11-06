package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestDiscoveryHealthPresentationSanitizesEndpointAndFailureText(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "verified-instance", Alias: "market\x1b[31m\u202e<script>"}
	health := discoveryHealthForConfig(&config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, []marketplace.DiscoveryOutcome{{
		Endpoint: endpoint, Total: 3, Truncated: true, CacheState: marketplace.DiscoveryCacheStale, Err: errors.New("secret-query"),
	}}, "")
	if len(health) != 1 || strings.ContainsAny(health[0].Label, "\x1b\u202e") || !strings.Contains(health[0].Label, "<script>") {
		t.Fatal("endpoint health label was not reduced to sanitized inert scalar text")
	}
	if health[0].Message != discoveryEndpointFailureMessage || strings.Contains(health[0].Message, "secret-query") {
		t.Fatal("endpoint health exposed dynamic query or remote error text")
	}
}

func TestUserAPIDiscoveryRequestUsesStrictUserOnlyRoute(t *testing.T) {
	api, home, _ := newUserAPI(t, session.DependencyLifecycle{})
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	before, err := os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}

	invalidBodies := []string{
		`{}`,
		`{"query":"needle"}`,
		`{"requestId":1}`,
		`{"query":"needle","requestId":0}`,
		`{"query":"needle","requestId":-1}`,
		`{"query":"needle","requestId":1.0}`,
		`{"query":"needle","requestId":"1"}`,
		`{"query":1,"requestId":1}`,
		`{"query":"needle","requestId":1,"unknown":true}`,
		`{"query":"needle","query":"other","requestId":1}`,
		`{"query":"needle","Query":"other","requestId":1}`,
		`{"query":"needle","requestId":1,"RequestId":2}`,
	}
	for _, body := range invalidBodies {
		recorder := serveDiscoveryRequest(t, api, http.MethodPost, "/api/discovery", body, api.Credential(), "")
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("POST /api/discovery with malformed body status = %d, want 400", recorder.Code)
		}
		if code := responseErrorCode(t, recorder); code != codeInvalidRequest {
			t.Fatalf("POST /api/discovery malformed body code = %q, want %q", code, codeInvalidRequest)
		}
	}

	for _, path := range []string{"/api/discovery?query=needle", "/api/discovery?"} {
		recorder := serveDiscoveryRequest(t, api, http.MethodPost, path, `{"query":"needle","requestId":1}`, api.Credential(), "")
		if recorder.Code != http.StatusBadRequest || responseErrorCode(t, recorder) != codeInvalidRequest {
			t.Fatalf("POST discovery with URL query = %d/%s, want 400/%s", recorder.Code, responseErrorCode(t, recorder), codeInvalidRequest)
		}
	}

	valid := `{"query":"","requestId":1}`
	if recorder := serveDiscoveryRequest(t, api, http.MethodPost, "/api/discovery", valid, api.Credential(), ""); recorder.Code != http.StatusBadRequest || responseErrorCode(t, recorder) != codeNotReady {
		t.Fatalf("discovery before accepted Marketplace config = %d/%s, want 400/%s", recorder.Code, responseErrorCode(t, recorder), codeNotReady)
	}
	if recorder := serveDiscoveryRequest(t, api, http.MethodGet, "/api/discovery", "", api.Credential(), ""); recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/discovery status = %d, want 405", recorder.Code)
	}
	if recorder := serveDiscoveryRequest(t, api, http.MethodPost, "/api/discovery", valid, api.Credential(), "https://invalid.example"); recorder.Code != http.StatusForbidden {
		t.Fatalf("wrong Origin status = %d, want 403", recorder.Code)
	}
	if recorder := serveDiscoveryRequest(t, api, http.MethodPost, "/api/discovery", valid, "", ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing credential status = %d, want 401", recorder.Code)
	}

	generic := newAPI(t, session.DependencyLifecycle{}, nil)
	if recorder := serveDiscoveryRequest(t, generic, http.MethodPost, "/api/discovery", valid, generic.Credential(), ""); recorder.Code != http.StatusBadRequest || responseErrorCode(t, recorder) != codeNotSupported {
		t.Fatalf("generic session discovery = %d/%s, want 400/%s", recorder.Code, responseErrorCode(t, recorder), codeNotSupported)
	}

	if api.current != nil || api.started || api.sequence != 0 || api.requests.Active(session.RequestDiscovery) != 0 {
		t.Fatalf("invalid discovery requests changed session state: current=%#v started=%t sequence=%d discovery=%d", api.current, api.started, api.sequence, api.requests.Active(session.RequestDiscovery))
	}
	if after, err := os.ReadFile(userConfig); err != nil || string(after) != string(before) {
		t.Fatalf("discovery trigger changed user config: err=%v", err)
	}
}

func TestUserAPIDiscoveryDebouncesAndCancelsSupersededSearches(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})
	const instanceID = "work35-marketplace"
	var server *httptest.Server
	var searchCount atomic.Int32
	searchStarted := map[string]chan struct{}{"first": make(chan struct{}, 1), "second": make(chan struct{}, 1)}
	firstCanceled := make(chan struct{})
	secondCanceled := make(chan struct{})
	closeStarted := make(chan struct{}, 1)
	closeCanceled := make(chan struct{})
	var cancelOnce sync.Once
	var secondCancelOnce sync.Once
	var closeCancelOnce sync.Once
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	var releaseOnce sync.Once
	var releaseSecondOnce sync.Once
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
		case "/api/v1/search":
			searchCount.Add(1)
			query := r.URL.Query().Get("q")
			if started := searchStarted[query]; started != nil {
				select {
				case started <- struct{}{}:
				default:
				}
			}
			if query == "first" {
				select {
				case <-r.Context().Done():
					cancelOnce.Do(func() { close(firstCanceled) })
				case <-releaseFirst:
				}
				return
			}
			if query == "second" {
				select {
				case <-r.Context().Done():
					secondCancelOnce.Do(func() { close(secondCanceled) })
				case <-releaseSecond:
				}
				if r.Context().Err() != nil {
					return
				}
			}
			if query == "secret-query" {
				http.Error(w, "private-search-failure", http.StatusInternalServerError)
				return
			}
			if query == "close" {
				select {
				case closeStarted <- struct{}{}:
				default:
				}
				<-r.Context().Done()
				closeCancelOnce.Do(func() { close(closeCanceled) })
				return
			}
			item := mpapi.ResourceListItem{
				RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "second-result",
				Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64),
				Availability: mpapi.AvailabilityAvailable, Score: 1,
			}
			_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{item}, Total: 1, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseFirst) })
		releaseSecondOnce.Do(func() { close(releaseSecond) })
		server.Close()
	})

	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: instanceID, Alias: "work35"}
	cached := mpapi.ResourceListItem{
		RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "cached-result",
		Revision: "sha256:" + strings.Repeat("d", 64), ContentDigest: "sha256:" + strings.Repeat("d", 64),
		Availability: mpapi.AvailabilityAvailable, Score: 1,
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: endpoint, Items: []mpapi.ResourceListItem{cached}, Total: 1, ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatalf("seed real discovery cache: %v", err)
	}
	staleItem := mpapi.ResourceListItem{
		RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "stale-result",
		Revision: "sha256:" + strings.Repeat("f", 64), ContentDigest: "sha256:" + strings.Repeat("f", 64),
		Availability: mpapi.AvailabilityAvailable, Score: 1,
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: endpoint, Items: []mpapi.ResourceListItem{staleItem}, Total: 1, ObservedAt: time.Now().Add(-(marketplace.DiscoveryFreshTTL + time.Minute)),
	}, "secret-query", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatalf("seed stale discovery cache: %v", err)
	}

	if _, ok := api.gate.Begin(session.OperationSync); !ok {
		t.Fatal("could not hold operation state for discovery projection control")
	}
	operationID, _, active := api.gate.Active()
	if !active {
		t.Fatal("operation gate did not retain active operation")
	}
	t.Cleanup(func() { api.gate.Finish(operationID) })

	provisional := session.Row{
		Artifact:            resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "pending-local"}},
		PendingVerification: true,
	}
	events := prepareDiscoveryAPISession(t, api, &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, []session.Row{provisional})
	initialEvent, initial := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 0 && payload.Query == ""
	})
	if initialEvent.Generation == 0 || initialEvent.Operation != nil {
		t.Fatalf("initial discovery envelope generation/operation = %d/%#v", initialEvent.Generation, initialEvent.Operation)
	}
	if len(initial.Rows) != 2 || initial.Rows[0].Name != "pending-local" || !initial.Rows[0].PendingVerification || initial.Rows[1].Name != "cached-result" || len(initial.Health) != 1 || initial.Health[0].CacheState != "fresh" || initial.Loading {
		t.Fatalf("cached startup discovery = rows:%#v health:%#v loading:%t", initial.Rows, initial.Health, initial.Loading)
	}
	if searchCount.Load() != 0 {
		t.Fatalf("fresh startup cache issued %d remote searches", searchCount.Load())
	}

	apiServer := httptest.NewServer(api.Handler())
	t.Cleanup(apiServer.Close)
	firstAt := time.Now()
	firstDone := postDiscoveryAsync(t, apiServer, api, context.Background(), "first", 1)
	_, firstLoading := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 1 && payload.Query == "first" && payload.Loading
	})
	if len(firstLoading.Rows) != 2 || firstLoading.Rows[1].Name != "cached-result" {
		t.Fatal("query transition dropped the accepted browse cache before SDK lookup")
	}
	firstBarrier := time.NewTimer(session.DiscoverySearchDebounce - 25*time.Millisecond)
	select {
	case <-searchStarted["first"]:
		t.Fatal("remote search started before the shared debounce interval")
	case <-firstBarrier.C:
	}
	if searchCount.Load() != 0 {
		t.Fatalf("remote search counter crossed the negative debounce barrier: %d", searchCount.Load())
	}
	select {
	case <-searchStarted["first"]:
		if time.Since(firstAt) < session.DiscoverySearchDebounce {
			t.Fatal("remote search started before the shared debounce interval")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first remote search did not start after the shared debounce interval")
	}

	secondDone := postDiscoveryAsync(t, apiServer, api, context.Background(), "second", 2)
	select {
	case <-firstCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("superseding discovery did not cancel the active endpoint request")
	}
	select {
	case <-searchStarted["second"]:
	case <-time.After(5 * time.Second):
		t.Fatal("current remote search did not start")
	}

	api.mu.Lock()
	beforeQuery, beforeRequestID := api.discovery.query, api.discovery.requestID
	beforeRows := make([]string, 0, len(api.current.Rows))
	for _, row := range api.current.Rows {
		beforeRows = append(beforeRows, row.ID)
	}
	api.mu.Unlock()
	staleResponse := <-postDiscoveryAsync(t, apiServer, api, context.Background(), "first", 1)
	if staleResponse.err != nil || staleResponse.status != http.StatusNoContent || len(staleResponse.body) != 0 {
		t.Fatal("older browser request was not ignored with empty 204")
	}
	duplicateResponse := <-postDiscoveryAsync(t, apiServer, api, context.Background(), "second", 2)
	if duplicateResponse.err != nil || duplicateResponse.status != http.StatusNoContent || len(duplicateResponse.body) != 0 {
		t.Fatal("duplicate current browser request was not idempotent")
	}
	conflictingDuplicate := serveDiscoveryRequest(t, api, http.MethodPost, "/api/discovery", `{"query":"conflict","requestId":2}`, api.Credential(), "")
	var conflictBody struct {
		Code string `json:"code"`
	}
	if conflictingDuplicate.Code != http.StatusBadRequest || json.Unmarshal(conflictingDuplicate.Body.Bytes(), &conflictBody) != nil || conflictBody.Code != codeInvalidRequest {
		t.Fatal("same browser request identity with a different query was not rejected")
	}
	api.mu.Lock()
	afterQuery, afterRequestID := api.discovery.query, api.discovery.requestID
	afterRows := make([]string, 0, len(api.current.Rows))
	for _, row := range api.current.Rows {
		afterRows = append(afterRows, row.ID)
	}
	loading := api.discovery.loading
	api.mu.Unlock()
	if beforeQuery != afterQuery || beforeRequestID != afterRequestID || strings.Join(beforeRows, "\x00") != strings.Join(afterRows, "\x00") || !loading || searchCount.Load() != 2 {
		t.Fatal("older or duplicate browser request changed current discovery state or reached the SDK")
	}
	select {
	case <-secondCanceled:
		t.Fatal("older or duplicate browser request canceled the current SDK request")
	default:
	}
	releaseSecondOnce.Do(func() { close(releaseSecond) })
	secondResponse := <-secondDone
	if secondResponse.err != nil || secondResponse.status != http.StatusNoContent || len(secondResponse.body) != 0 {
		t.Fatalf("current discovery response = %d body=%d err=%v, want empty 204", secondResponse.status, len(secondResponse.body), secondResponse.err)
	}
	firstResponse := <-firstDone
	if firstResponse.err != nil || firstResponse.status != http.StatusNoContent || len(firstResponse.body) != 0 {
		t.Fatalf("superseded discovery response = %d body=%d err=%v, want empty 204", firstResponse.status, len(firstResponse.body), firstResponse.err)
	}

	var currentEvent sessionEvent
	var current testDiscoveryPayload
	for {
		event := nextEvent(t, events)
		if event.Type != "discovery" {
			continue
		}
		payload := decodeTestDiscoveryPayload(t, event)
		if payload.RequestID == 2 && payload.Query == "second" && !payload.Loading {
			currentEvent, current = event, payload
			break
		}
	}
	if currentEvent.Generation != initialEvent.Generation || currentEvent.Operation != nil || currentEvent.Sequence <= initialEvent.Sequence {
		t.Fatalf("current discovery envelope = generation:%d sequence:%d operation:%#v", currentEvent.Generation, currentEvent.Sequence, currentEvent.Operation)
	}
	if current.DiscoverySequence <= initial.DiscoverySequence || len(current.Rows) != 2 || current.Rows[0].Name != "pending-local" || current.Rows[1].Name != "second-result" {
		t.Fatalf("current discovery state = sequence:%d rows:%#v", current.DiscoverySequence, current.Rows)
	}
	if searchCount.Load() != 2 {
		t.Fatalf("remote search requests = %d, want the cancelled first and current second requests", searchCount.Load())
	}

	api.mu.Lock()
	currentSnapshot := *api.current
	currentGeneration := api.currentG
	activeOperation, activeKind, stillActive := api.gate.Active()
	settled := api.lastSettledOperation
	api.mu.Unlock()
	if currentSnapshot.Stage != "config" || currentSnapshot.Drift != "pending" || uint64(currentGeneration) != initialEvent.Generation {
		t.Fatalf("discovery changed core snapshot state: stage=%q drift=%q generation=%d", currentSnapshot.Stage, currentSnapshot.Drift, currentGeneration)
	}
	if !stillActive || activeOperation != operationID || activeKind != session.OperationSync || settled != nil {
		t.Fatalf("discovery changed operation state: active=%v/%v/%t settled=%#v", activeOperation, activeKind, stillActive, settled)
	}

	reconnected := streamEvents(t, api)
	reconnectedSnapshot := nextEvent(t, reconnected)
	if reconnectedSnapshot.Type != "snapshot" {
		t.Fatalf("reconnection event type = %q, want snapshot", reconnectedSnapshot.Type)
	}
	reconnectedState := decodeTestSnapshotDiscovery(t, reconnectedSnapshot)
	if reconnectedState.Query != "second" || reconnectedState.RequestID != 2 || reconnectedState.DiscoverySequence != current.DiscoverySequence || len(reconnectedState.Rows) != 2 || reconnectedState.Rows[0].Name != "pending-local" || reconnectedState.Rows[1].Name != "second-result" {
		t.Fatal("reconnection snapshot did not retain the current discovery state")
	}

	logs := captureSlog(t)
	failureResponse := serveDiscoveryRequest(t, api, http.MethodPost, "/api/discovery", `{"query":"secret-query","requestId":3}`, api.Credential(), "")
	if failureResponse.Code != http.StatusNoContent || failureResponse.Body.Len() != 0 {
		t.Fatalf("failed discovery transport = %d body=%d, want empty 204", failureResponse.Code, failureResponse.Body.Len())
	}
	_, failure := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 3 && payload.Query == "secret-query" && !payload.Loading
	})
	foundStale := false
	for _, row := range failure.Rows {
		if row.Name == "stale-result" && row.Discovery {
			foundStale = true
		}
	}
	if len(failure.Health) != 1 || failure.Health[0].CacheState != string(marketplace.DiscoveryCacheStale) || failure.Health[0].Total != 1 || failure.Health[0].Message == "" || strings.Contains(failure.Health[0].Message, "secret-query") || strings.Contains(failure.Health[0].Message, "private-search-failure") || !foundStale {
		t.Fatal("failed stale revalidation did not retain cached rows and report static endpoint health")
	}
	if strings.Contains(logs.String(), "secret-query") || strings.Contains(logs.String(), "private-search-failure") {
		t.Fatal("discovery logged request or remote response text")
	}

	closeDone := postDiscoveryAsync(t, apiServer, api, context.Background(), "close", 4)
	select {
	case <-closeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown control did not reach the held remote request")
	}
	closed := make(chan struct{})
	go func() {
		api.Close()
		close(closed)
	}()
	select {
	case <-closeCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("API close did not cancel its blocked discovery worker")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("API close waited on a leaked discovery producer")
	}
	closeResponse := <-closeDone
	if closeResponse.err != nil || closeResponse.status != http.StatusNoContent || len(closeResponse.body) != 0 {
		t.Fatal("closed discovery trigger did not complete with an empty 204")
	}
}

func TestUserAPIDiscoveryRapidSupersessionSkipsFirstRemoteCall(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})
	const instanceID = "rapid-discovery-instance"
	var searchCount atomic.Int32
	searches := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
		case "/api/v1/search":
			searchCount.Add(1)
			searches <- r.URL.Query().Get("q")
			item := mpapi.ResourceListItem{
				RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "current-result",
				Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64),
				Availability: mpapi.AvailabilityAvailable, Score: 1,
			}
			_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{item}, Total: 1, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: instanceID, Alias: "rapid"}
	cached := mpapi.ResourceListItem{
		RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "startup-cache",
		Revision: "sha256:" + strings.Repeat("d", 64), ContentDigest: "sha256:" + strings.Repeat("d", 64),
		Availability: mpapi.AvailabilityAvailable, Score: 1,
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: endpoint, Items: []mpapi.ResourceListItem{cached}, Total: 1, ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatal("seed startup cache")
	}
	local := session.Row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "pending-local"}}, PendingVerification: true}
	events := prepareDiscoveryAPISession(t, api, &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, []session.Row{local})
	_, initial := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 0 && payload.Query == ""
	})
	if len(initial.Rows) != 2 || initial.Rows[0].Name != "pending-local" || initial.Rows[1].Name != "startup-cache" || searchCount.Load() != 0 {
		t.Fatal("fresh startup cache was not used before user queries")
	}

	apiServer := httptest.NewServer(api.Handler())
	t.Cleanup(apiServer.Close)
	startedAt := time.Now()
	firstDone := postDiscoveryAsync(t, apiServer, api, context.Background(), "first", 1)
	_, firstLoading := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 1 && payload.Loading
	})
	if firstLoading.Query == "" || time.Since(startedAt) >= session.DiscoverySearchDebounce {
		t.Fatal("first query did not enter its debounce window before supersession")
	}
	currentDone := postDiscoveryAsync(t, apiServer, api, context.Background(), "current", 2)
	_, current := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 2 && payload.Query == "current" && !payload.Loading
	})
	firstResponse, currentResponse := <-firstDone, <-currentDone
	if firstResponse.err != nil || firstResponse.status != http.StatusNoContent || len(firstResponse.body) != 0 || currentResponse.err != nil || currentResponse.status != http.StatusNoContent || len(currentResponse.body) != 0 {
		t.Fatal("rapid superseded discovery triggers did not complete with empty 204 responses")
	}
	if searchCount.Load() != 1 || <-searches != "current" || len(searches) != 0 || len(current.Rows) != 2 || current.Rows[1].Name != "current-result" {
		t.Fatal("rapid supersession made a remote call for an obsolete query")
	}
}

func TestUserAPIDiscoveryRequiresValidatedMarketplaceConfiguration(t *testing.T) {
	unboundLock, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeOrganization,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://unbound.example", InstanceID: "unbound-instance",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		allowlist bool
		lockBytes []byte
	}{
		{name: "denied allowed URL", allowlist: true},
		{name: "malformed deployed lock", lockBytes: []byte("not: [valid\n")},
		{name: "unbound endpoint lock", lockBytes: unboundLock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := newUserAPIFixture(t, true)
			const instanceID = "validated-marketplace"
			var searchCount atomic.Int32
			searchStarted := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/agent-marketplace.json":
					_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
				case "/api/v1/search":
					searchCount.Add(1)
					select {
					case searchStarted <- struct{}{}:
					default:
					}
					_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{}, Total: 0, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			orgPath := filepath.Join(t.TempDir(), "config.yaml")
			organization := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n"
			if tc.allowlist {
				organization += "allowed_marketplaces: ['^https://blocked.example$']\n"
			}
			organization += "sources:\n  mp: {marketplace: " + server.URL + "}\nresources:\n  mp: {include: [tools/skill/pending]}\n"
			if err := os.WriteFile(orgPath, []byte(organization), 0o600); err != nil {
				t.Fatal(err)
			}
			config.SetTestOrganizationConfigPath(t, orgPath)
			if tc.lockBytes != nil {
				lockPaths, err := config.ResolveConfigLockPathsTolerateMissing(orgPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(lockPaths.CanonicalLock, tc.lockBytes, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(api.Close)
			events := streamEvents(t, api)
			var final snapshotDTO
			for {
				event := nextEvent(t, events)
				if event.Type == "discovery" {
					t.Fatal("rejected Marketplace configuration emitted a discovery event")
				}
				if event.Type == "snapshot" && event.snapshotStage() == "final" {
					final = event.snapshotPayload(t)
					break
				}
			}
			if final.DiscoveryEnabled || searchCount.Load() != 0 {
				t.Fatal("rejected Marketplace configuration enabled discovery or reached search")
			}
			select {
			case <-searchStarted:
				t.Fatal("rejected Marketplace configuration started a remote search")
			case <-time.After(session.DiscoverySearchDebounce):
			}
			response := serveDiscoveryRequest(t, api, http.MethodPost, "/api/discovery", `{"query":"","requestId":1}`, api.Credential(), "")
			if response.Code != http.StatusBadRequest || responseErrorCode(t, response) != codeNotReady {
				t.Fatal("API did not fail closed without an accepted Marketplace configuration")
			}
		})
	}
}

func TestAPIStartupDiscoveryDoesNotApplyUserQueryDebounce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stale bool
	}{
		{name: "cache miss"},
		{name: "stale cache", stale: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, _, _ := newUserAPI(t, session.DependencyLifecycle{})
			const instanceID = "startup-discovery-instance"
			searchStarted := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/agent-marketplace.json":
					_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
				case "/api/v1/search":
					select {
					case searchStarted <- struct{}{}:
					default:
					}
					_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{}, Total: 0, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: instanceID, Alias: "startup"}
			if tc.stale {
				item := mpapi.ResourceListItem{
					RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "stale-startup",
					Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64),
					Availability: mpapi.AvailabilityAvailable, Score: 1,
				}
				if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
					Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now().Add(-(marketplace.DiscoveryFreshTTL + time.Minute)),
				}, "", "", marketplace.DiscoveryPageLimit); err != nil {
					t.Fatal("seed stale startup cache")
				}
			}

			startedAt := time.Now()
			events := prepareDiscoveryAPISession(t, api, &config.Config{Marketplaces: []config.MarketplaceEntry{endpoint}}, nil)
			_, cached := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
				return payload.RequestID == 0 && payload.Query == "" && payload.Loading
			})
			if tc.stale && (len(cached.Rows) != 1 || cached.Rows[0].Name != "stale-startup" || len(cached.Health) != 1 || cached.Health[0].CacheState != string(marketplace.DiscoveryCacheStale)) {
				t.Fatal("stale startup cache was not rendered before revalidation")
			}
			select {
			case <-searchStarted:
				if time.Since(startedAt) >= session.DiscoverySearchDebounce {
					t.Fatal("initial SDK cache miss or stale revalidation waited for the user query debounce")
				}
			case <-time.After(session.DiscoverySearchDebounce - 25*time.Millisecond):
				t.Fatal("initial SDK cache miss or stale revalidation did not reach the endpoint before the user query debounce")
			}
		})
	}
}

func TestUserAPICachedDiscoveryRendersDuringBrowseSafeAcquisition(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	const instanceID = "browse-safe-instance"
	var searchRequests atomic.Int32
	var wellKnownRequests atomic.Int32
	acquisitionStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			wellKnownRequests.Add(1)
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
		case r.URL.Path == "/api/v1/search":
			searchRequests.Add(1)
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			select {
			case acquisitionStarted <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
	}()

	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	orgConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: " + server.URL + "}\nresources:\n  mp: {include: [tools/skill/pending]}\n"
	if err := os.WriteFile(orgPath, []byte(orgConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	if err := config.ValidateOrganizationTrustUnit(orgPath, true, true); err != nil {
		t.Fatalf("trusted organization control failed: %v", err)
	}

	endpoint := config.MarketplaceEntry{URL: server.URL, InstanceID: instanceID, Alias: "mp"}
	item := mpapi.ResourceListItem{
		RegistryID: "tools", RegistrySlug: "tools", Kind: "skill", Name: "remote",
		Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64),
		Availability: mpapi.AvailabilityAvailable, Score: 1,
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatalf("seed fresh discovery cache: %v", err)
	}

	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	userLockPaths, err := config.ResolveConfigLockPathsTolerateMissing(userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	orgLockPaths, err := config.ResolveConfigLockPathsTolerateMissing(orgPath)
	if err != nil {
		t.Fatal(err)
	}
	materializedPath := filepath.Join(home, ".devin", "skills", "work35-protected", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(materializedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materializedPath, []byte("protected materialized bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	protectedFiles := make([]discoveryFixtureFile, 0, 5)
	for _, path := range []string{userConfigPath, orgPath, userLockPaths.CanonicalLock, orgLockPaths.CanonicalLock, materializedPath} {
		protectedFiles = append(protectedFiles, captureDiscoveryFixtureFile(t, path))
	}

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	events := streamEvents(t, api)
	select {
	case <-acquisitionStarted:
	case <-time.After(30 * time.Second):
		t.Fatal("trusted organization Marketplace acquisition did not reach its held request")
	}

	var configSeen bool
	var cachedEvent sessionEvent
	var cached testDiscoveryPayload
	for {
		event := nextEvent(t, events)
		if event.Type == "snapshot" {
			stage := event.snapshotStage()
			if stage == "inventory" || stage == "final" {
				t.Fatalf("snapshot advanced to %q while acquisition was held", stage)
			}
			if stage == "config" {
				payload := event.snapshotPayload(t)
				configSeen = payload.DiscoveryEnabled
			}
			continue
		}
		if event.Type != "discovery" {
			continue
		}
		payload := decodeTestDiscoveryPayload(t, event)
		if payload.RequestID == 0 && payload.Query == "" && len(payload.Rows) >= 2 {
			cachedEvent, cached = event, payload
			break
		}
	}
	if !configSeen || cachedEvent.Generation == 0 {
		t.Fatal("cached discovery was not bound to a validated BrowseSafe MergedCfg config stage")
	}
	foundPending, foundCached := false, false
	for _, row := range cached.Rows {
		if row.Name == "solo" && row.PendingVerification {
			foundPending = true
		}
		if row.Name == "remote" {
			foundCached = true
		}
	}
	if !foundPending || !foundCached {
		t.Fatalf("held config-stage rows lack the pending local and cached Marketplace controls: %#v", cached.Rows)
	}
	if len(cached.Health) != 1 || cached.Health[0].CacheState != "fresh" || cached.Loading {
		t.Fatalf("BrowseSafe cache health = %#v, loading=%t", cached.Health, cached.Loading)
	}
	if searchRequests.Load() != 0 {
		t.Fatalf("fresh BrowseSafe cache made %d remote search requests", searchRequests.Load())
	}
	api.mu.Lock()
	currentStage, currentGeneration := api.current.Stage, api.currentG
	accepted := api.acceptedMergedCfg
	acceptedEndpoints := []config.MarketplaceEntry(nil)
	if accepted != nil {
		acceptedEndpoints = append(acceptedEndpoints, accepted.Marketplaces...)
	}
	api.mu.Unlock()
	if currentStage != "config" || uint64(currentGeneration) != cachedEvent.Generation {
		t.Fatalf("discovery changed held core snapshot: stage=%q generation=%d event=%d", currentStage, currentGeneration, cachedEvent.Generation)
	}
	if wellKnownRequests.Load() == 0 || len(acceptedEndpoints) != 1 || acceptedEndpoints[0].InstanceID != instanceID || len(acceptedEndpoints[0].Resources) != 0 {
		t.Fatal("cached discovery did not use the trusted BrowseSafe endpoint identity without resource pins")
	}

	releaseOnce.Do(func() { close(release) })
	for {
		event := nextEvent(t, events)
		if event.Type == "snapshot" && event.snapshotStage() == "final" {
			break
		}
	}
	api.mu.Lock()
	finalSnapshot := *api.current
	finalRows := append([]rowDTO{}, api.current.Rows...)
	api.mu.Unlock()
	if finalSnapshot.Stage != "final" || finalSnapshot.Drift == "pending" || !finalSnapshot.DiscoveryEnabled || finalSnapshot.DiscoveryLoading || finalSnapshot.DiscoveryQuery != "" || finalSnapshot.DiscoveryRequestID != 0 {
		t.Fatal("released startup did not publish a terminal authoritative snapshot retaining cached discovery state")
	}
	foundFinalRemote := false
	for _, row := range finalRows {
		if row.Name == "remote" && row.Discovery {
			foundFinalRemote = true
		}
	}
	if !foundFinalRemote {
		t.Fatal("authoritative final snapshot dropped the cached Marketplace discovery row")
	}
	for i, before := range protectedFiles {
		after := captureDiscoveryFixtureFile(t, before.path)
		if before.exists != after.exists || before.content != after.content {
			t.Fatalf("snapshot discovery changed protected fixture file %d", i)
		}
	}
}

func TestAPIDiscoveryRowsSurviveAuthoritativeStagesAndDropOnlyRemovedEndpoint(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})
	endpointA := config.MarketplaceEntry{URL: "https://market-a.example", InstanceID: "market-a", Alias: "a"}
	endpointB := config.MarketplaceEntry{URL: "https://market-b.example", InstanceID: "market-b", Alias: "b"}
	for _, endpoint := range []config.MarketplaceEntry{endpointA, endpointB} {
		item := mpapi.ResourceListItem{
			RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "cached-" + endpoint.InstanceID,
			Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64),
			Availability: mpapi.AvailabilityAvailable, Score: 1,
		}
		if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
			Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now(),
		}, "", "", marketplace.DiscoveryPageLimit); err != nil {
			t.Fatalf("seed discovery cache: %v", err)
		}
	}
	localRow := session.Row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "authoritative-local"}}}
	events := prepareDiscoveryAPISession(t, api, &config.Config{Marketplaces: []config.MarketplaceEntry{endpointA, endpointB}}, []session.Row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "pending-local"}}, PendingVerification: true},
	})
	_, initial := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 0 && payload.Query == ""
	})
	if len(initial.Rows) != 3 || initial.Rows[0].Name != "pending-local" || initial.Rows[1].Name != "cached-market-a" || initial.Rows[2].Name != "cached-market-b" {
		t.Fatalf("pre-inventory merged rows = %#v", initial.Rows)
	}

	api.mu.Lock()
	generation := api.currentG
	api.mu.Unlock()
	inventory := api.deliverSnapshot(generation, session.Snapshot{
		Generation: generation, Stage: session.SnapshotStageInventory, DriftAssessment: session.DriftUnknown,
		Rows: []session.Row{localRow}, MergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpointA, endpointB}},
	})
	if inventory == nil || len(inventory.Rows) != 3 || inventory.Rows[0].Name != "authoritative-local" || inventory.Rows[1].Name != "cached-market-a" || inventory.Rows[2].Name != "cached-market-b" {
		t.Fatalf("inventory merge = %#v", inventory)
	}
	for _, endpoint := range []config.MarketplaceEntry{endpointA, endpointB} {
		item := mpapi.ResourceListItem{
			RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: "needle-" + endpoint.InstanceID,
			Revision: "sha256:" + strings.Repeat("d", 64), ContentDigest: "sha256:" + strings.Repeat("d", 64),
			Availability: mpapi.AvailabilityAvailable, Score: 1,
		}
		if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
			Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now(),
		}, "needle", "", marketplace.DiscoveryPageLimit); err != nil {
			t.Fatalf("seed query cache: %v", err)
		}
	}
	request := serveDiscoveryRequest(t, api, http.MethodPost, "/api/discovery", `{"query":"needle","requestId":4}`, api.Credential(), "")
	if request.Code != http.StatusNoContent || request.Body.Len() != 0 {
		t.Fatalf("cached query trigger = %d body=%d, want empty 204", request.Code, request.Body.Len())
	}
	api.mu.Lock()
	finalGeneration := api.beginSnapshotLocked()
	api.mu.Unlock()
	final := api.deliverSnapshot(finalGeneration, session.Snapshot{
		Generation: finalGeneration, Stage: session.SnapshotStageFinal, DriftAssessment: session.DriftReady,
		Rows: []session.Row{localRow}, MergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpointB}},
	})
	if final == nil || final.DiscoveryQuery != "needle" || final.DiscoveryRequestID != 4 || len(final.Rows) != 2 || final.Rows[0].Name != "authoritative-local" || final.Rows[1].Name != "needle-market-b" {
		t.Fatal("final snapshot did not preserve current discovery state and the retained endpoint")
	}
	if len(final.DiscoveryHealth) != 1 || final.DiscoveryHealth[0].Label != "b" || final.DiscoverySequence <= initial.DiscoverySequence {
		t.Fatalf("endpoint-removal health/sequence = %#v/%d, want retained endpoint and monotonic sequence", final.DiscoveryHealth, final.DiscoverySequence)
	}
}

func TestAPIDiscoveryConfigChangeCancelsRemovedEndpointWorker(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})
	const instanceA = "market-a"
	const instanceB = "market-b"
	newServer := func(instanceID string, search http.HandlerFunc) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/agent-marketplace.json" {
				_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
				return
			}
			if r.URL.Path == "/api/v1/search" {
				search(w, r)
				return
			}
			http.NotFound(w, r)
		}))
	}
	searchStarted := make(chan struct{}, 1)
	searchCanceled := make(chan struct{})
	var searchCanceledOnce sync.Once
	var searchCountA, searchCountB atomic.Int32
	serverA := newServer(instanceA, func(w http.ResponseWriter, r *http.Request) {
		searchCountA.Add(1)
		select {
		case searchStarted <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		searchCanceledOnce.Do(func() { close(searchCanceled) })
	})
	serverB := newServer(instanceB, func(w http.ResponseWriter, r *http.Request) {
		searchCountB.Add(1)
		_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{}, Total: 0, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
	})
	t.Cleanup(serverA.Close)
	t.Cleanup(serverB.Close)
	endpointA := config.MarketplaceEntry{URL: serverA.URL, InstanceID: instanceA, Alias: "a"}
	endpointB := config.MarketplaceEntry{URL: serverB.URL, InstanceID: instanceB, Alias: "b"}
	store := func(endpoint config.MarketplaceEntry, query, name string, observed time.Time) {
		t.Helper()
		item := mpapi.ResourceListItem{
			RegistryID: "registry", RegistrySlug: "tools", Kind: "skill", Name: name,
			Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64),
			Availability: mpapi.AvailabilityAvailable, Score: 1,
		}
		if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
			Endpoint: endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: observed,
		}, query, "", marketplace.DiscoveryPageLimit); err != nil {
			t.Fatalf("seed discovery cache: %v", err)
		}
	}
	store(endpointA, "", "cached-a", time.Now())
	store(endpointB, "", "cached-b", time.Now())
	store(endpointB, "needle", "needle-market-b", time.Now())
	local := session.Row{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "pending-local"}}, PendingVerification: true}
	events := prepareDiscoveryAPISession(t, api, &config.Config{Marketplaces: []config.MarketplaceEntry{endpointA, endpointB}}, []session.Row{local})
	_, initial := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 0 && payload.Query == "" && !payload.Loading
	})
	if len(initial.Rows) != 3 || searchCountA.Load() != 0 || searchCountB.Load() != 0 {
		t.Fatal("initial endpoint caches did not provide the browse rows")
	}

	apiServer := httptest.NewServer(api.Handler())
	t.Cleanup(apiServer.Close)
	request := postDiscoveryAsync(t, apiServer, api, context.Background(), "needle", 1)
	_, pending := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 1 && payload.Query == "needle" && payload.Loading && len(payload.Rows) == 2 && payload.Rows[1].Name == "needle-market-b"
	})
	if len(pending.Rows) != 2 || pending.Rows[1].Name != "needle-market-b" {
		t.Fatal("cached endpoint rows were not retained while the missing endpoint revalidated")
	}
	select {
	case <-searchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("removed endpoint request did not reach the remote search")
	}

	api.mu.Lock()
	generation := api.beginSnapshotLocked()
	api.mu.Unlock()
	final := api.deliverSnapshot(generation, session.Snapshot{
		Generation: generation, Stage: session.SnapshotStageFinal, DriftAssessment: session.DriftReady,
		Rows: []session.Row{local}, MergedCfg: &config.Config{Marketplaces: []config.MarketplaceEntry{endpointB}},
	})
	if final == nil || final.DiscoveryQuery != "needle" || final.DiscoveryRequestID != 1 || len(final.DiscoveryHealth) != 1 || final.DiscoveryHealth[0].Label != "b" {
		t.Fatal("validated endpoint removal did not preserve the active query on the remaining endpoint")
	}
	select {
	case <-searchCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("endpoint removal did not cancel the removed endpoint request")
	}
	_, retained := nextTestDiscovery(t, events, func(payload testDiscoveryPayload) bool {
		return payload.RequestID == 1 && payload.Query == "needle" && !payload.Loading && len(payload.Health) == 1 && payload.Health[0].Label == "b"
	})
	if len(retained.Rows) != 2 || retained.Rows[0].Name != "pending-local" || retained.Rows[1].Name != "needle-market-b" || searchCountA.Load() != 1 || searchCountB.Load() != 0 {
		t.Fatal("removed endpoint worker replaced the accepted result or remaining endpoint cache")
	}
	response := <-request
	if response.err != nil || response.status != http.StatusNoContent || len(response.body) != 0 {
		t.Fatal("cancelled config-stage discovery trigger did not complete with empty 204")
	}
}

func prepareDiscoveryAPISession(t *testing.T, api *API, merged *config.Config, provisional []session.Row) <-chan sessionEvent {
	t.Helper()
	api.mu.Lock()
	api.started = true
	generation := api.beginSnapshotLocked()
	api.mu.Unlock()
	events := streamEvents(t, api)
	if provisional != nil {
		provisionalSnapshot := api.deliverSnapshot(generation, session.Snapshot{
			Generation: generation, Stage: session.SnapshotStageConfig,
			ProvisionalCfg: &config.Config{}, ProvisionalRows: provisional,
		})
		if provisionalSnapshot == nil {
			t.Fatal("provisional snapshot was rejected")
		}
		if provisionalSnapshot.DiscoveryEnabled || len(provisionalSnapshot.DiscoveryHealth) != 0 || provisionalSnapshot.DiscoveryQuery != "" || provisionalSnapshot.DiscoveryRequestID != 0 || provisionalSnapshot.DiscoverySequence != 0 || provisionalSnapshot.DiscoveryLoading {
			t.Fatalf("provisional snapshot published discovery state: %+v", provisionalSnapshot)
		}
	}
	if api.deliverSnapshot(generation, session.Snapshot{Generation: generation, Stage: session.SnapshotStageConfig, MergedCfg: merged}) == nil {
		t.Fatal("validated config snapshot was rejected")
	}
	return events
}

type testDiscoveryHealth struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Total      int    `json:"total"`
	Truncated  bool   `json:"truncated"`
	CacheState string `json:"cacheState"`
	Message    string `json:"message"`
}

type testDiscoveryPayload struct {
	RequestID         uint64                `json:"requestId"`
	Query             string                `json:"query"`
	DiscoverySequence uint64                `json:"discoverySequence"`
	Rows              []rowDTO              `json:"rows"`
	Health            []testDiscoveryHealth `json:"health"`
	Loading           bool                  `json:"loading"`
}

func nextTestDiscovery(t *testing.T, events <-chan sessionEvent, matches func(testDiscoveryPayload) bool) (sessionEvent, testDiscoveryPayload) {
	t.Helper()
	for {
		event := nextEvent(t, events)
		if event.Type != "discovery" {
			continue
		}
		payload := decodeTestDiscoveryPayload(t, event)
		if matches(payload) {
			return event, payload
		}
	}
}

func decodeTestDiscoveryPayload(t *testing.T, event sessionEvent) testDiscoveryPayload {
	t.Helper()
	data, err := json.Marshal(event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var payload testDiscoveryPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

type testSnapshotDiscovery struct {
	Rows              []rowDTO `json:"rows"`
	Query             string   `json:"discoveryQuery"`
	RequestID         uint64   `json:"discoveryRequestId"`
	DiscoverySequence uint64   `json:"discoverySequence"`
}

func decodeTestSnapshotDiscovery(t *testing.T, event sessionEvent) testSnapshotDiscovery {
	t.Helper()
	data, err := json.Marshal(event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot testSnapshotDiscovery
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func postDiscoveryAsync(t *testing.T, server *httptest.Server, api *API, parent context.Context, query string, requestID uint64) <-chan discoveryHTTPResult {
	t.Helper()
	body, err := json.Marshal(struct {
		Query     string `json:"query"`
		RequestID uint64 `json:"requestId"`
	}{Query: query, RequestID: requestID})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(parent, http.MethodPost, server.URL+"/api/discovery", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(tokenHeader, api.Credential())
	result := make(chan discoveryHTTPResult, 1)
	go func() {
		response, err := server.Client().Do(request)
		if err != nil {
			result <- discoveryHTTPResult{err: err}
			return
		}
		defer func() { _ = response.Body.Close() }()
		data, readErr := io.ReadAll(response.Body)
		result <- discoveryHTTPResult{status: response.StatusCode, body: data, err: readErr}
	}()
	return result
}

type discoveryHTTPResult struct {
	status int
	body   []byte
	err    error
}

func serveDiscoveryRequest(t *testing.T, api *API, method, path, body, token, origin string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var requestBody interface{ Read([]byte) (int, error) }
	if reader != nil {
		requestBody = reader
	}
	req := httptest.NewRequest(method, path, requestBody)
	if token != "" {
		req.Header.Set(tokenHeader, token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	return recorder
}

func responseErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode API error response: %v", err)
	}
	return response.Code
}

type discoveryFixtureFile struct {
	path    string
	exists  bool
	content string
}

func captureDiscoveryFixtureFile(t *testing.T, path string) discoveryFixtureFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return discoveryFixtureFile{path: path}
		}
		t.Fatalf("read protected discovery fixture file: %v", err)
	}
	return discoveryFixtureFile{path: path, exists: true, content: string(data)}
}
