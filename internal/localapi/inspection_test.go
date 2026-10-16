package localapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/session"
	inspectiontest "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/inspection"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type inspectionResponseDTO struct {
	RowID              string              `json:"rowId"`
	Generation         uint64              `json:"generation"`
	Revision           string              `json:"revision"`
	ManifestDigest     string              `json:"manifestDigest"`
	Files              []inspectionFileDTO `json:"files"`
	PrimaryFileID      string              `json:"primaryFileId"`
	SelectedFileID     string              `json:"selectedFileId"`
	Content            string              `json:"content"`
	Truncated          bool                `json:"truncated"`
	UnsupportedMessage string              `json:"unsupportedMessage"`
}

type inspectionEventRows struct {
	Rows []rowDTO `json:"rows"`
}

type inspectionAPIFixture struct {
	api        *API
	row        rowDTO
	generation uint64
	protected  []discoveryFixtureFile
}

func newInspectionAPIFixture(t *testing.T, fixture *inspectiontest.Fixture, endpointURLs ...string) *inspectionAPIFixture {
	t.Helper()
	home, _ := newUserAPIFixture(t, true)
	endpoint := fixture.Endpoint()
	if len(endpointURLs) > 0 {
		endpoint.URL = endpointURLs[0]
	}
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	orgConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: " + endpoint.URL + "}\n"
	if err := os.WriteFile(orgPath, []byte(orgConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	if err := config.ValidateOrganizationTrustUnit(orgPath, true, true); err != nil {
		t.Fatalf("validate trusted inspection endpoint fixture: %v", err)
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: endpoint, Items: []mpapi.ResourceListItem{fixture.Item()}, Total: 1, ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatalf("seed fresh inspection discovery page: %v", err)
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

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	events := streamEvents(t, api)
	row, generation := waitForInspectionRow(t, events)
	waitForInspectionFinal(t, api, events)
	api.mu.Lock()
	marketplaceCount, resourceCount := 0, -1
	for _, currentRow := range api.current.Rows {
		if currentRow.ID == row.ID {
			row = currentRow
			generation = uint64(api.currentG)
			break
		}
	}
	acceptedInstanceID := ""
	if accepted := api.acceptedMergedCfg; accepted != nil {
		marketplaceCount = len(accepted.Marketplaces)
		if marketplaceCount == 1 {
			acceptedInstanceID = accepted.Marketplaces[0].InstanceID
			resourceCount = len(accepted.Marketplaces[0].Resources)
		}
	}
	api.mu.Unlock()
	if !row.Inspectable {
		t.Fatal("real NewUser API did not publish the cache-discovered skill as inspectable")
	}
	if marketplaceCount != 1 || acceptedInstanceID != endpoint.InstanceID || resourceCount != 0 {
		t.Fatal("NewUser inspection fixture did not accept the verified endpoint without resource pins")
	}
	return &inspectionAPIFixture{api: api, row: row, generation: generation, protected: protected}
}

func waitForInspectionRow(t *testing.T, events <-chan sessionEvent) (rowDTO, uint64) {
	t.Helper()
	for {
		event := nextEvent(t, events)
		var rows []rowDTO
		switch event.Type {
		case "snapshot":
			rows = event.snapshotPayload(t).Rows
		case "discovery":
			data, err := json.Marshal(event.Payload)
			if err != nil {
				t.Fatal(err)
			}
			var payload inspectionEventRows
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			rows = payload.Rows
		}
		for _, row := range rows {
			if row.Name == "grill" && row.Discovery {
				return row, event.Generation
			}
		}
	}
}

func waitForInspectionFinal(t *testing.T, api *API, events <-chan sessionEvent) {
	t.Helper()
	for {
		api.mu.Lock()
		final := api.current != nil && api.current.Stage == "final" && !api.current.DiscoveryLoading
		api.mu.Unlock()
		if final {
			return
		}
		nextEvent(t, events)
	}
}

func inspectionFileID(rowID, path, digest string) string {
	payload, _ := json.Marshal([3]string{rowID, path, digest})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func inspectionContentRequests(paths []string) []string {
	var requests []string
	for _, path := range paths {
		if strings.Contains(path, "/inspection/content") {
			requests = append(requests, path)
		}
	}
	return requests
}

func assertInspectionFilesUnchanged(t *testing.T, before []discoveryFixtureFile) {
	t.Helper()
	for _, original := range before {
		after := captureDiscoveryFixtureFile(t, original.path)
		if after.exists != original.exists || after.content != original.content {
			t.Fatalf("inspection changed protected file %s", original.path)
		}
	}
}

func decodeInspectionResponse(t *testing.T, recorder *httptest.ResponseRecorder) inspectionResponseDTO {
	t.Helper()
	var response inspectionResponseDTO
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode inspection response: %v", err)
	}
	return response
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestUserAPIInspectionReturnsVerifiedPrimaryAndSupportingFilesReadOnly(t *testing.T) {
	fixture := inspectiontest.NewFixtureContent(t,
		[]byte("# Grill inspection\n\nLiteral <script>text</script> remains inert."),
		map[string][]byte{
			"notes.md":   []byte("# Notes\n\nSupporting inspection.\u202e"),
			"logo.png":   []byte("\x89PNG\r\n\x1a\nfake-image"),
			"unsafe.svg": []byte("<svg onload=alert(1)></svg>"),
		}, nil)
	state := newInspectionAPIFixture(t, fixture)
	beforeState, beforeGeneration, beforeSequence, beforeRequests := inspectionAPIState(t, state.api)
	beforeOperation, beforeKind, beforeActive := state.api.gate.Active()

	primary := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", `{"rowId":"`+state.row.ID+`"}`, state.api.Credential(), "")
	if primary.Code != http.StatusOK {
		t.Fatalf("primary inspection status = %d body=%s", primary.Code, primary.Body.String())
	}
	primaryResponse := decodeInspectionResponse(t, primary)
	if primaryResponse.RowID != state.row.ID || primaryResponse.Generation != state.generation || primaryResponse.Revision != fixture.Revision || primaryResponse.ManifestDigest != fixture.Manifest.ManifestDigest || primaryResponse.PrimaryFileID == "" || primaryResponse.SelectedFileID != primaryResponse.PrimaryFileID || primaryResponse.Content != string(fixture.Primary) || primaryResponse.Truncated || primaryResponse.UnsupportedMessage != "" {
		t.Fatalf("primary inspection response = %+v", primaryResponse)
	}
	if len(primaryResponse.Files) != 4 {
		t.Fatalf("inspection members = %#v, want primary, two text/image support files and metadata SVG", primaryResponse.Files)
	}
	for _, member := range primaryResponse.Files {
		if len(member.ID) != 64 || strings.Trim(member.ID, "0123456789abcdef") != "" {
			t.Fatalf("inspection file ID is not opaque lower-case SHA256: %#v", member)
		}
	}

	var notes inspectionFileDTO
	var image inspectionFileDTO
	var svg inspectionFileDTO
	for _, member := range primaryResponse.Files {
		switch member.Path {
		case "support/notes.md":
			notes = member
		case "support/logo.png":
			image = member
		case "support/unsafe.svg":
			svg = member
		}
	}
	if notes.ID == "" || image.ID == "" || svg.ID == "" {
		t.Fatalf("inspection member IDs missing from %#v", primaryResponse.Files)
	}

	support := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", `{"rowId":"`+state.row.ID+`","fileId":"`+notes.ID+`"}`, state.api.Credential(), "")
	if support.Code != http.StatusOK {
		t.Fatalf("support inspection status = %d body=%s", support.Code, support.Body.String())
	}
	supportResponse := decodeInspectionResponse(t, support)
	if supportResponse.SelectedFileID != notes.ID || supportResponse.Content != session.SanitizeTerminalData(string(fixture.Support["notes.md"]), session.TerminalMarkdownBody) || supportResponse.UnsupportedMessage != "" {
		t.Fatalf("support inspection response = %+v", supportResponse)
	}

	pathsBeforeUnsupported := fixture.Log()
	for _, member := range []inspectionFileDTO{image, svg} {
		unsupported := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", `{"rowId":"`+state.row.ID+`","fileId":"`+member.ID+`"}`, state.api.Credential(), "")
		if unsupported.Code != http.StatusOK {
			t.Fatalf("unsupported inspection status = %d body=%s", unsupported.Code, unsupported.Body.String())
		}
		response := decodeInspectionResponse(t, unsupported)
		if response.Content != "" || response.UnsupportedMessage == "" || response.SelectedFileID != member.ID || !strings.Contains(response.UnsupportedMessage, member.MediaType) || !strings.Contains(response.UnsupportedMessage, "bytes") {
			t.Fatalf("unsupported inspection response = %+v", response)
		}
	}
	if got := inspectionContentRequests(fixture.Log()); len(got) != 2 {
		t.Fatalf("only the primary/support text bodies may be fetched, content routes=%v baseline=%v", got, pathsBeforeUnsupported)
	}

	if afterState, generation, sequence, requests := inspectionAPIState(t, state.api); !bytes.Equal(afterState, beforeState) || generation != beforeGeneration || sequence != beforeSequence || requests != beforeRequests {
		t.Fatalf("read-only inspection changed core state: generation=%d/%d sequence=%d/%d requests=%+v/%+v", generation, beforeGeneration, sequence, beforeSequence, requests, beforeRequests)
	}
	afterOperation, afterKind, afterActive := state.api.gate.Active()
	if afterOperation != beforeOperation || afterKind != beforeKind || afterActive != beforeActive {
		t.Fatalf("inspection changed operation gate: before=%d/%q/%t after=%d/%q/%t", beforeOperation, beforeKind, beforeActive, afterOperation, afterKind, afterActive)
	}
	assertInspectionFilesUnchanged(t, state.protected)
}

func TestInspectionPreviewBoundsSanitizedExpansion(t *testing.T) {
	raw := []byte(strings.Repeat("\u202e", session.MaxDetailSourceBytes/3))
	if len(raw) >= session.MaxDetailSourceBytes || !inspection.IsValidText(raw) {
		t.Fatal("control fixture must be valid SDK text under the raw preview limit")
	}
	content, truncated := inspectionPreview(raw)
	if !truncated || len(content) > session.MaxDetailSourceBytes+len(session.DetailTruncationMarker)+8 || !utf8.ValidString(content) {
		t.Fatalf("sanitized expansion exceeded the preview budget: truncated=%t bytes=%d", truncated, len(content))
	}
	if strings.Count(content, session.DetailTruncationMarker) != 1 || strings.ContainsRune(content, '\u202e') {
		t.Fatal("sanitized expansion did not produce exactly one marker without raw bidi controls")
	}
}

func TestUserAPIInspectionBoundsTextPreviewAtTheSharedLimit(t *testing.T) {
	primary := []byte(strings.Repeat("界", session.MaxDetailSourceBytes/3+64) + "TAIL-SENTINEL")
	fixture := inspectiontest.NewFixtureContent(t, primary, map[string][]byte{"notes.md": []byte("# Notes")}, nil)
	state := newInspectionAPIFixture(t, fixture)
	response := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", `{"rowId":"`+state.row.ID+`"}`, state.api.Credential(), "")
	if response.Code != http.StatusOK {
		t.Fatalf("oversize preview status = %d body=%s", response.Code, response.Body.String())
	}
	payload := decodeInspectionResponse(t, response)
	if !payload.Truncated || !strings.Contains(payload.Content, session.DetailTruncationMarker) || strings.Contains(payload.Content, "TAIL-SENTINEL") {
		t.Fatalf("oversize preview was not bounded with its marker: truncated=%t bytes=%d", payload.Truncated, len(payload.Content))
	}
	if len(payload.Content) > session.MaxDetailSourceBytes+len(session.DetailTruncationMarker)+8 || !utf8.ValidString(payload.Content) {
		t.Fatalf("bounded preview length/UTF-8 invalid: bytes=%d", len(payload.Content))
	}
}

func TestUserAPIInspectionStrictRequestAuthorityHasNoSDKTraffic(t *testing.T) {
	fixture := inspectiontest.NewFixture(t, nil)
	state := newInspectionAPIFixture(t, fixture)
	before := fixture.Log()
	invalid := []string{
		`{}`,
		`{"rowId":1}`,
		`{"rowId":"row","extra":true}`,
		`{"rowId":"row","rowId":"other"}`,
		`{"rowId":"row","RowId":"other"}`,
		`{"rowId":"row"} {}`,
		`{"rowId":"row","fileId":"` + strings.Repeat("a", 64) + `","fileId":"` + strings.Repeat("a", 64) + `"}`,
		`{"rowId":"row","FileId":"` + strings.Repeat("a", 64) + `"}`,
		`{"rowId":"row","fileId":""}`,
		`{"rowId":"row","fileId":"` + strings.Repeat("A", 64) + `"}`,
	}
	for _, body := range invalid {
		recorder := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", body, state.api.Credential(), "")
		if recorder.Code != http.StatusBadRequest || responseErrorCode(t, recorder) != codeInvalidRequest {
			t.Fatalf("malformed inspection request = %d/%s", recorder.Code, responseErrorCode(t, recorder))
		}
		if after := fixture.Log(); !equalStrings(before, after) {
			t.Fatalf("malformed request caused SDK traffic: before=%v after=%v", before, after)
		}
	}
	for _, request := range []struct {
		method string
		path   string
		token  string
		origin string
		status int
		code   string
	}{
		{method: http.MethodPost, path: "/api/inspection?fileId=ignored", token: state.api.Credential(), status: http.StatusBadRequest, code: codeInvalidRequest},
		{method: http.MethodGet, path: "/api/inspection", token: state.api.Credential(), status: http.StatusMethodNotAllowed},
		{method: http.MethodPost, path: "/api/inspection", token: state.api.Credential(), origin: "https://invalid.example", status: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/inspection", status: http.StatusUnauthorized},
	} {
		recorder := serveDiscoveryRequest(t, state.api, request.method, request.path, `{"rowId":"`+state.row.ID+`"}`, request.token, request.origin)
		if recorder.Code != request.status || (request.code != "" && responseErrorCode(t, recorder) != request.code) {
			t.Fatalf("inspection boundary request = %d/%s, want %d/%s", recorder.Code, responseErrorCode(t, recorder), request.status, request.code)
		}
	}
	if !equalStrings(before, fixture.Log()) {
		t.Fatalf("invalid/auth/method/origin inspection requests caused SDK traffic: before=%v after=%v", before, fixture.Log())
	}

	missingRow := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", `{"rowId":"unknown-row"}`, state.api.Credential(), "")
	if missingRow.Code != http.StatusBadRequest || responseErrorCode(t, missingRow) != codeNotReady {
		t.Fatalf("unknown row inspection = %d/%s, want 400/%s", missingRow.Code, responseErrorCode(t, missingRow), codeNotReady)
	}
	if !equalStrings(before, fixture.Log()) {
		t.Fatalf("unknown row caused SDK traffic: before=%v after=%v", before, fixture.Log())
	}

	localRowID := ""
	state.api.mu.Lock()
	for _, row := range state.api.current.Rows {
		if row.Name == "solo" {
			localRowID = row.ID
			break
		}
	}
	state.api.mu.Unlock()
	if localRowID == "" {
		t.Fatal("NewUser control row missing for ineligible-row check")
	}
	ineligible := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", `{"rowId":"`+localRowID+`"}`, state.api.Credential(), "")
	if ineligible.Code != http.StatusBadRequest || responseErrorCode(t, ineligible) != codeNotSupported {
		t.Fatalf("ineligible row inspection = %d/%s, want 400/%s", ineligible.Code, responseErrorCode(t, ineligible), codeNotSupported)
	}
	if !equalStrings(before, fixture.Log()) {
		t.Fatalf("ineligible row caused SDK traffic: before=%v after=%v", before, fixture.Log())
	}

	state.api.mu.Lock()
	generation := state.api.beginSnapshotLocked()
	state.api.mu.Unlock()
	if state.api.deliverSnapshot(generation, session.Snapshot{
		Generation: generation, Stage: session.SnapshotStageFinal, DriftAssessment: session.DriftReady,
		Rows: []session.Row{}, MergedCfg: &config.Config{},
	}) == nil {
		t.Fatal("authoritative snapshot did not accept the row-removal control")
	}
	beforeStale := fixture.Log()
	stale := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", `{"rowId":"`+state.row.ID+`"}`, state.api.Credential(), "")
	if stale.Code != http.StatusBadRequest || responseErrorCode(t, stale) != codeNotReady {
		t.Fatalf("stale row inspection = %d/%s, want 400/%s", stale.Code, responseErrorCode(t, stale), codeNotReady)
	}
	if !equalStrings(beforeStale, fixture.Log()) {
		t.Fatalf("stale row caused SDK traffic: before=%v after=%v", beforeStale, fixture.Log())
	}

	generic := newAPI(t, session.DependencyLifecycle{}, nil)
	unsupported := serveDiscoveryRequest(t, generic, http.MethodPost, "/api/inspection", `{"rowId":"row"}`, generic.Credential(), "")
	if unsupported.Code != http.StatusBadRequest || responseErrorCode(t, unsupported) != codeNotSupported {
		t.Fatalf("generic session inspection = %d/%s, want 400/%s", unsupported.Code, responseErrorCode(t, unsupported), codeNotSupported)
	}
}

func TestUserAPIInspectionUnknownManifestFileDoesNotFetchContent(t *testing.T) {
	fixture := inspectiontest.NewFixture(t, nil)
	state := newInspectionAPIFixture(t, fixture)
	missing := inspectionFileID(state.row.ID, "support/not-in-manifest.md", "sha256:"+strings.Repeat("f", 64))
	before := inspectionContentRequests(fixture.Log())
	response := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/inspection", `{"rowId":"`+state.row.ID+`","fileId":"`+missing+`"}`, state.api.Credential(), "")
	if response.Code != http.StatusBadRequest || responseErrorCode(t, response) != codeInvalidRequest {
		t.Fatalf("unknown inspection file = %d/%s, want 400/%s", response.Code, responseErrorCode(t, response), codeInvalidRequest)
	}
	if got := inspectionContentRequests(fixture.Log()); !equalStrings(before, got) {
		t.Fatalf("unknown manifest file caused a content GET: before=%v after=%v", before, got)
	}
}

func TestUserAPIInspectionErrorsAreStaticForUnverifiedContent(t *testing.T) {
	cases := []struct {
		name      string
		mutate    inspectiontest.Mutate
		wellKnown string
		corrupt   bool
	}{
		{name: "well-known instance mismatch", wellKnown: "another-instance"},
		{name: "missing assessment", mutate: func(_ *inspectiontest.Fixture, _ *mpapi.WellKnown, revision *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			revision.Assessment.InspectionManifestDigest = ""
		}},
		{name: "revision coordinate mismatch", mutate: func(_ *inspectiontest.Fixture, _ *mpapi.WellKnown, revision *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			revision.Coordinate.Name = "other"
		}},
		{name: "revision digest mismatch", mutate: func(_ *inspectiontest.Fixture, _ *mpapi.WellKnown, revision *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			revision.ContentDigest = store.RevisionDigest("sha256:" + strings.Repeat("e", 64))
		}},
		{name: "manifest canonical digest mismatch", mutate: func(_ *inspectiontest.Fixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, payload *[]byte, _ map[string][]byte) {
			*payload = append(append([]byte(nil), *payload...), ' ')
		}},
		{name: "corrupt primary body", mutate: func(f *inspectiontest.Fixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			f.Primary = []byte("corrupt primary")
		}},
		{name: "corrupt support body", mutate: func(f *inspectiontest.Fixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, bodies map[string][]byte) {
			bodies[inspection.Digest(f.Support["notes.md"])] = []byte("corrupt support")
		}, corrupt: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := inspectiontest.NewFixture(t, tc.mutate)
			state := newInspectionAPIFixture(t, fixture)
			if tc.wellKnown != "" {
				fixture.SetWellKnownInstanceID(tc.wellKnown)
			}
			body := `{"rowId":"` + state.row.ID + `"}`
			if tc.corrupt {
				manifest := fixture.Manifest
				for _, file := range manifest.Files {
					if file.Path == "support/notes.md" {
						body = `{"rowId":"` + state.row.ID + `","fileId":"` + inspectionFileID(state.row.ID, file.Path, file.ContentDigest) + `"}`
					}
				}
			}
			before := captureInspectionResponse(t, state.api, body)
			if before.Code != http.StatusBadGateway || responseErrorCode(t, before) != codeInspectionUnavailable {
				t.Fatalf("unverified inspection response = %d/%s, want 502/%s", before.Code, responseErrorCode(t, before), codeInspectionUnavailable)
			}
			if strings.Contains(before.Body.String(), fixture.Server.URL) || strings.Contains(before.Body.String(), "corrupt") || strings.Contains(before.Body.String(), "digest mismatch") {
				t.Fatalf("inspection leaked dynamic SDK details: %s", before.Body.String())
			}
			assertInspectionFilesUnchanged(t, state.protected)
		})
	}
}

type inspectionRequestGate struct {
	server      *httptest.Server
	started     chan struct{}
	canceled    chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	cancelOnce  sync.Once
}

func newInspectionRequestGate(t *testing.T, targetURL, blockedPath string) *inspectionRequestGate {
	t.Helper()
	target, err := url.Parse(targetURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	gate := &inspectionRequestGate{started: make(chan struct{}, 1), canceled: make(chan struct{}), release: make(chan struct{})}
	gate.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == blockedPath {
			select {
			case gate.started <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
				gate.cancelOnce.Do(func() { close(gate.canceled) })
				return
			case <-gate.release:
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		gate.releaseOnce.Do(func() { close(gate.release) })
		gate.server.Close()
	})
	return gate
}

func (g *inspectionRequestGate) releaseRequest() {
	g.releaseOnce.Do(func() { close(g.release) })
}

func TestUserAPIInspectionCancelsUpstreamOnRequestAbortAndAPIClose(t *testing.T) {
	for _, tc := range []struct {
		name     string
		closeAPI bool
	}{
		{name: "request cancellation"},
		{name: "API close", closeAPI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := inspectiontest.NewFixture(t, nil)
			blockedPath := "/api/v1/resources/registry-1/skill/grill/revisions/" + fixture.Revision + "/inspection/content"
			gate := newInspectionRequestGate(t, fixture.Server.URL, blockedPath)
			state := newInspectionAPIFixture(t, fixture, gate.server.URL)
			apiServer := httptest.NewServer(state.api.Handler())
			t.Cleanup(apiServer.Close)

			requestContext, cancelRequest := context.WithCancel(context.Background())
			defer cancelRequest()
			request, err := http.NewRequestWithContext(requestContext, http.MethodPost, apiServer.URL+"/api/inspection", strings.NewReader(`{"rowId":"`+state.row.ID+`"}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set(tokenHeader, state.api.Credential())
			request.Header.Set("Content-Type", "application/json")
			requestDone := make(chan struct{})
			go func() {
				response, _ := apiServer.Client().Do(request)
				if response != nil {
					_ = response.Body.Close()
				}
				close(requestDone)
			}()
			select {
			case <-gate.started:
			case <-time.After(10 * time.Second):
				t.Fatal("inspection did not reach the held primary content request")
			}

			if tc.closeAPI {
				closed := make(chan struct{})
				go func() {
					state.api.Close()
					close(closed)
				}()
				select {
				case <-closed:
				case <-time.After(10 * time.Second):
					t.Fatal("API.Close did not cancel the held inspection request")
				}
			} else {
				cancelRequest()
				select {
				case <-requestDone:
				case <-time.After(10 * time.Second):
					t.Fatal("aborted inspection HTTP request did not return")
				}
			}
			select {
			case <-gate.canceled:
			case <-time.After(10 * time.Second):
				t.Fatal("inspection cancellation did not reach the SDK request context")
			}
			select {
			case <-requestDone:
			case <-time.After(10 * time.Second):
				t.Fatal("inspection HTTP handler did not finish after SDK cancellation")
			}
			gate.releaseRequest()
		})
	}
}

func captureInspectionResponse(t *testing.T, api *API, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serveDiscoveryRequest(t, api, http.MethodPost, "/api/inspection", body, api.Credential(), "")
}

func inspectionAPIState(t *testing.T, api *API) ([]byte, uint64, uint64, session.RequestState) {
	t.Helper()
	api.mu.Lock()
	defer api.mu.Unlock()
	var snapshot []byte
	if api.current != nil {
		var err error
		snapshot, err = json.Marshal(api.current)
		if err != nil {
			t.Fatal(err)
		}
	}
	return snapshot, uint64(api.currentG), api.sequence, api.requests
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
