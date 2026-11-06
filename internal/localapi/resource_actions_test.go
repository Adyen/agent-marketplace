package localapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/session"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type work39InitialMode string

const (
	work39ModeAvailable     work39InitialMode = "available"
	work39ModeUserInstalled work39InitialMode = "user-installed"
	work39ModeOrgOptional   work39InitialMode = "org-optional"
)

type work39MarketplaceFixture struct {
	home                  string
	repo                  string
	userConfigPath        string
	userLockPath          string
	orgConfigPath         string
	orgLockPath           string
	materializedPath      string
	unrelatedMaterialized string
	endpoint              config.MarketplaceEntry
	contentDigest         string
	archiveDigest         string
	requests              atomic.Int64
	holdRegistry          atomic.Bool
	registryStarted       chan struct{}
	registryStartedOnce   sync.Once
	protected             []work39ProtectedFile
}

func setupWork39MarketplaceFixture(t *testing.T, home, repo string, bootstrap map[string]string, mode work39InitialMode, required, risk bool) *work39MarketplaceFixture {
	t.Helper()
	if mode != work39ModeAvailable && mode != work39ModeUserInstalled && mode != work39ModeOrgOptional {
		t.Fatalf("unsupported Work39 initial mode %q", mode)
	}

	resource := ir.NewSkill("review", "Review code", "# Review code\n", []string{"user"}, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatalf("build canonical Work39 resource: %v", err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "source"
	canonical.Manifest.Revision = canonical.Manifest.ContentDigest
	canonical.Manifest.Provenance = hccartifact.CanonicalProvenance{
		RegistryURL: "https://git.example/repo.git",
		Ref:         "main",
		Path:        ".agents",
		Generator:   "test",
	}
	canonical.Manifest.SupportedSchemas = []string{hccartifact.CanonicalArtifactSchemaV1}
	rawArtifact, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("encode canonical Work39 resource: %v", err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	coordinate := store.ResourceCoordinate{Marketplace: store.MarketplaceID("mp"), Registry: store.RegistryID("src"), Kind: "skill", Name: "review"}
	decision := store.AssessmentDecisionAllow
	findings := []store.Finding(nil)
	if risk {
		decision = store.AssessmentDecisionWarn
		findings = []store.Finding{{Code: "fixture-risk", Severity: "warning", Policy: "warn", Message: "Fixture content requires explicit acknowledgement"}}
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               coordinate,
		ContentDigest:            store.RevisionDigest(contentDigest),
		AnalyzerVersion:          "gui-test",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 decision,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
		Findings:                 findings,
	})
	if err != nil {
		t.Fatalf("finalize Work39 assessment: %v", err)
	}
	revision := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{
			Coordinate: coordinate, ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
		},
		Assessment: assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: coordinate, ContentDigest: store.RevisionDigest(contentDigest), Commit: strings.Repeat("a", 40),
			ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}
	fixture := &work39MarketplaceFixture{
		home: home, repo: repo, contentDigest: contentDigest, archiveDigest: archiveDigest,
		userConfigPath:        filepath.Join(home, ".agents", "config.yaml"),
		materializedPath:      filepath.Join(home, ".config", "devin", "skills", "review", "SKILL.md"),
		unrelatedMaterialized: filepath.Join(home, ".config", "devin", "skills", "work39-protected", "SKILL.md"),
		registryStarted:       make(chan struct{}),
	}
	var server *httptest.Server
	item := mpapi.ResourceListItem{
		RegistryID: "src", RegistrySlug: "source", Kind: "skill", Name: "review",
		Revision: contentDigest, ContentDigest: contentDigest, Availability: mpapi.AvailabilityAvailable,
		Description: "Review code",
	}
	registry := mpapi.PublicRegistry{
		ID: store.RegistryID("src"), Slug: "source", Origins: []string{"https://git.example/repo.git"}, Ref: "main",
		Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true,
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.requests.Add(1)
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", server.URL))
		case "/api/v1/registries/src", "/api/v1/registries/source":
			if fixture.holdRegistry.Load() {
				fixture.registryStartedOnce.Do(func() { close(fixture.registryStarted) })
				<-r.Context().Done()
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(registry)
		case "/api/v1/resources/src/skill/review/revisions/" + contentDigest,
			"/api/v1/resources/source/skill/review/revisions/" + contentDigest:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(revision)
		case "/api/v1/artifacts/sha256/" + strings.TrimPrefix(archiveDigest, "sha256:"):
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(rawArtifact)
		case "/api/v1/search":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(marketplace.SearchResponse{Items: []mpapi.ResourceListItem{item}, Total: 1, Offset: 0, Limit: marketplace.DiscoveryPageLimit})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	fixture.endpoint = config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Alias: "mp"}

	cache := marketplace.DefaultCache()
	if _, err := cache.WriteArtifact(archiveDigest, rawArtifact); err != nil {
		t.Fatalf("seed Work39 verified artifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("seed Work39 verified marker: %v", err)
	}
	if _, err := cache.WriteResourceRevision(server.URL, "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("seed Work39 assessed resource revision: %v", err)
	}
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: fixture.endpoint, Items: []mpapi.ResourceListItem{item}, Total: 1, ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatalf("seed Work39 empty-query discovery page: %v", err)
	}

	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	orgConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n"
	if mode == work39ModeOrgOptional {
		orgConfig += "sources:\n  mp: {marketplace: " + server.URL + "}\nresources:\n  mp:\n"
		if required {
			orgConfig += "    required: true\n"
		}
		orgConfig += "    include:\n      - source/skill/review: {revision: " + contentDigest + "}\n"
	}
	writeFile(t, orgPath, orgConfig)
	config.SetTestOrganizationConfigPath(t, orgPath)
	if err := config.ValidateOrganizationTrustUnit(orgPath, true, true); err != nil {
		t.Fatalf("validate Work39 organization config: %v", err)
	}
	fixture.orgConfigPath = orgPath
	orgLockPaths, err := config.ResolveConfigLockPathsTolerateMissing(orgPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.orgLockPath = orgLockPaths.CanonicalLock

	userConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n"
	if mode == work39ModeAvailable {
		userConfig += "sources:\n  mp: {marketplace: " + server.URL + "}\n"
	}
	if mode == work39ModeUserInstalled {
		userConfig += "sources:\n  mp: {marketplace: " + server.URL + "}\nresources:\n  mp:\n    include:\n      - source/skill/review: {revision: " + contentDigest + "}\n"
	}
	writeFile(t, fixture.userConfigPath, userConfig)
	userLockPaths, err := config.ResolveConfigLockPathsTolerateMissing(fixture.userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.userLockPath = userLockPaths.CanonicalLock

	if mode == work39ModeAvailable || mode == work39ModeUserInstalled {
		userResources := []lockfile.MarketplaceResourceLock(nil)
		bindings := []lockfile.MarketplaceRegistryBinding(nil)
		if mode == work39ModeUserInstalled {
			bindings = []lockfile.MarketplaceRegistryBinding{{Name: "source", RegistryID: "src"}}
			userResources = []lockfile.MarketplaceResourceLock{{
				RegistryName: "source", RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest,
				ContentDigest: lockfile.Digest(contentDigest), ArchiveDigest: lockfile.Digest(archiveDigest), Acquisition: lockfile.MarketplaceAcquisitionMirror,
			}}
		}
		userLock, err := lockfile.Encode(lockfile.Document{
			APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
			Marketplaces: []lockfile.MarketplaceLock{{URL: server.URL, InstanceID: "mp", RegistryBindings: bindings, Resources: userResources}},
		})
		if err != nil {
			t.Fatalf("encode Work39 user lock: %v", err)
		}
		writeFile(t, fixture.userLockPath, string(userLock))
	}
	if mode == work39ModeOrgOptional {
		orgLock, err := lockfile.Encode(lockfile.Document{
			APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeOrganization,
			Marketplaces: []lockfile.MarketplaceLock{{
				URL: server.URL, InstanceID: "mp",
				RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "source", RegistryID: "src"}},
				Resources: []lockfile.MarketplaceResourceLock{{
					RegistryName: "source", RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest,
					ContentDigest: lockfile.Digest(contentDigest), ArchiveDigest: lockfile.Digest(archiveDigest), Acquisition: lockfile.MarketplaceAcquisitionMirror,
				}},
			}},
		})
		if err != nil {
			t.Fatalf("encode Work39 organization lock: %v", err)
		}
		writeFile(t, fixture.orgLockPath, string(orgLock))
	}
	writeFile(t, fixture.unrelatedMaterialized, "work39 protected materialized bytes\n")
	if bootstrap != nil {
		bootstrap["orgConfig"] = orgPath
		bootstrap["marketplaceEndpoint"] = server.URL
	}
	fixture.protected = work39ProtectedFiles(t, fixture)
	return fixture
}

type work39APITestState struct {
	api        *API
	events     <-chan sessionEvent
	fixture    *work39MarketplaceFixture
	rowID      string
	row        session.Row
	generation session.RequestGeneration
	protected  []work39ProtectedFile
}

func newWork39APITestState(t *testing.T, mode work39InitialMode, required, risk bool) *work39APITestState {
	t.Helper()
	home, repo := newUserAPIFixture(t, false)
	fixture := setupWork39MarketplaceFixture(t, home, repo, nil, mode, required, risk)
	api, err := NewUser(context.Background(), home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	events := streamEvents(t, api)
	rowID, row, generation := waitForWork39ResourceRow(t, api, events, func(row session.Row) bool {
		return row.Artifact.IsMarketplace && row.Artifact.Name == "review"
	})
	assertWork39ProtectedFilesUnchanged(t, fixture.protected)
	return &work39APITestState{api: api, events: events, fixture: fixture, rowID: rowID, row: row, generation: generation, protected: fixture.protected}
}

func waitForWork39ResourceRow(t *testing.T, api *API, events <-chan sessionEvent, matches func(session.Row) bool) (string, session.Row, session.RequestGeneration) {
	t.Helper()
	var lastSequence uint64
	for {
		api.mu.Lock()
		rowsKnown := api.snapshotRowsKnown
		currentFinal := api.current != nil && api.current.Stage == "final" && !api.current.DiscoveryLoading
		generation := api.currentG
		sequence := api.sequence
		rows := session.MergeDiscoveryRows(api.authoritativeRows, api.provisionalRows, api.discovery.rows)
		for _, row := range rows {
			if rowsKnown && currentFinal && lastSequence >= sequence && generation != 0 && !row.PendingVerification && matches(row) {
				rowID := rowPresentation(row).ID
				api.mu.Unlock()
				return rowID, cloneInspectionRow(row), generation
			}
		}
		api.mu.Unlock()
		if event := nextEvent(t, events); event.Sequence > lastSequence {
			lastSequence = event.Sequence
		}
	}
}

type work39ProtectedFile struct {
	path   string
	exists bool
	mode   os.FileMode
	data   []byte
}

func work39ProtectedFiles(t *testing.T, fixture *work39MarketplaceFixture) []work39ProtectedFile {
	t.Helper()
	paths := []string{
		fixture.userConfigPath, fixture.userLockPath, fixture.orgConfigPath, fixture.orgLockPath,
		fixture.materializedPath, fixture.unrelatedMaterialized,
		filepath.Join(fixture.repo, ".agents", "config.yaml"), filepath.Join(fixture.repo, ".agents", "config.local.yaml"),
	}
	files := make([]work39ProtectedFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				files = append(files, work39ProtectedFile{path: path})
				continue
			}
			t.Fatalf("capture Work39 protected file: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read Work39 protected file: %v", err)
		}
		files = append(files, work39ProtectedFile{path: path, exists: true, mode: info.Mode().Perm(), data: data})
	}
	return files
}

func assertWork39ProtectedFilesUnchanged(t *testing.T, files []work39ProtectedFile) {
	assertWork39ProtectedFilesUnchangedExcept(t, files)
}

func assertWork39ProtectedFilesUnchangedExcept(t *testing.T, files []work39ProtectedFile, exceptions ...string) {
	t.Helper()
	ignored := make(map[string]struct{}, len(exceptions))
	for _, path := range exceptions {
		ignored[path] = struct{}{}
	}
	for _, before := range files {
		if _, skip := ignored[before.path]; skip {
			continue
		}
		info, err := os.Lstat(before.path)
		if os.IsNotExist(err) {
			if before.exists {
				t.Fatalf("Work39 protected file %s disappeared", filepath.Base(before.path))
			}
			continue
		}
		if err != nil {
			t.Fatalf("stat Work39 protected file %s: %v", filepath.Base(before.path), err)
		}
		if !before.exists || info.Mode().Perm() != before.mode {
			t.Fatalf("Work39 protected file %s existence/mode changed", filepath.Base(before.path))
		}
		after, err := os.ReadFile(before.path)
		if err != nil {
			t.Fatalf("read Work39 protected file %s: %v", filepath.Base(before.path), err)
		}
		if !bytes.Equal(before.data, after) {
			t.Fatalf("Work39 protected file %s bytes changed", filepath.Base(before.path))
		}
	}
}

func TestWork39FixturePublishesMarketplaceRowsThroughNewUser(t *testing.T) {
	for _, test := range []struct {
		name     string
		mode     work39InitialMode
		required bool
		want     string
	}{
		{name: "available", mode: work39ModeAvailable, want: session.UserMarketplaceActionInstall},
		{name: "user installed", mode: work39ModeUserInstalled, want: session.UserMarketplaceActionRemove},
		{name: "organization optional", mode: work39ModeOrgOptional, want: session.UserMarketplaceActionExclude},
		{name: "organization required", mode: work39ModeOrgOptional, required: true, want: session.UserMarketplaceActionNone},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newWork39APITestState(t, test.mode, test.required, false)
			got, err := session.UserMarketplaceAction(state.row)
			if err != nil || got != test.want {
				t.Fatalf("NewUser-published Marketplace action = %q, %v, want %q; row facts: %s", got, err, test.want, work39ActionFacts(state.row))
			}
			projected := work39CurrentRowDTO(t, state.api, state.rowID)
			if projected.ID != state.rowID || projected.ResourceAction != test.want {
				t.Fatalf("NewUser Resource DTO row/action = %t/%q, want %q", projected.ID == state.rowID, projected.ResourceAction, test.want)
			}
			if state.row.Artifact.Name != "review" || state.row.Artifact.Kind.String() != "skill" {
				t.Fatalf("NewUser row identity = %s/%s", state.row.Artifact.Kind, state.row.Artifact.Name)
			}
			if test.mode == work39ModeAvailable && state.row.MarketplaceDiscovery == nil {
				t.Fatal("available fixture did not arrive through the discovery projection")
			}
			if test.mode == work39ModeUserInstalled && (state.row.MarketplaceDiscovery != nil || state.row.Artifact.Source != config.UserPersonal) {
				t.Fatalf("user-installed fixture row source/discovery = %q/%t", state.row.Artifact.Source, state.row.MarketplaceDiscovery != nil)
			}
			if test.mode == work39ModeOrgOptional && (state.row.Artifact.Source != config.Organization || state.row.CanOverrideMarketplace() != !test.required) {
				t.Fatalf("organization fixture row ownership/optional state = %v/%t", state.row.Artifact.Source, state.row.CanOverrideMarketplace())
			}
			assertWork39ProtectedFilesUnchanged(t, state.protected)
		})
	}
}

func work39CurrentRowDTO(t *testing.T, api *API, rowID string) rowDTO {
	t.Helper()
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.current != nil {
		for _, row := range api.current.Rows {
			if row.ID == rowID {
				return row
			}
		}
	}
	t.Fatalf("current API snapshot omitted Work39 row; generation=%d", api.currentG)
	return rowDTO{}
}

type work39PlanResponse struct {
	RowID                  string `json:"rowId"`
	Generation             uint64 `json:"generation"`
	PlanID                 string `json:"planId"`
	Action                 string `json:"action"`
	Preview                string `json:"preview"`
	RequiresRiskAcceptance bool   `json:"requiresRiskAcceptance"`
}

func validWork39PlanID(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func work39ActionFacts(row session.Row) string {
	userSelection, hasUserSelection := row.MarketplaceSelections[config.UserPersonal]
	organizationSelection, hasOrganizationSelection := row.MarketplaceSelections[config.Organization]
	discoveryAvailability := "none"
	if row.MarketplaceDiscovery != nil {
		discoveryAvailability = string(row.MarketplaceDiscovery.Item.Availability)
	}
	return fmt.Sprintf("source=%q marketplace=%t required=%t pending=%t enabledUser=%t collectionOnly=%t discovery=%q userSelection=%t userDirect=%t userSelectedBy=%d userSlug=%t organizationSelection=%t organizationSlug=%t", row.Artifact.Source, row.Artifact.IsMarketplace, row.Artifact.Required, row.PendingVerification, row.EnabledGlobal, row.MarketplaceCollectionOnly, discoveryAvailability, hasUserSelection, userSelection.Pin.Direct, len(userSelection.Pin.SelectedBy.List()), userSelection.Pin.RegistrySlug != "", hasOrganizationSelection, organizationSelection.Pin.RegistrySlug != "")
}

func postWork39(t *testing.T, api *API, path, body string) (int, []byte) {
	t.Helper()
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, authedRequest(t, api, path, body))
	return recorder.Code, bytes.Clone(recorder.Body.Bytes())
}

func decodeWork39Plan(t *testing.T, body []byte) work39PlanResponse {
	t.Helper()
	var plan work39PlanResponse
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("decode Work39 plan response: %v", err)
	}
	return plan
}

func work39OperationResponse(t *testing.T, body []byte) operationRef {
	t.Helper()
	var response struct {
		Operation operationRef `json:"operation"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode Work39 operation response: %v", err)
	}
	return response.Operation
}

func waitForWork39Operation(t *testing.T, state *work39APITestState, operation operationRef, wantSeverity string) snapshotDTO {
	t.Helper()
	var final snapshotDTO
	finalSeen := false
	for {
		event := nextEvent(t, state.events)
		if event.Type == "snapshot" {
			payload := event.snapshotPayload(t)
			if payload.Stage == "final" && payload.SettledOperation != nil && payload.SettledOperation.ID == operation.ID {
				if event.Operation != nil {
					t.Fatal("final Work39 operation snapshot still had an active operation")
				}
				final = payload
				finalSeen = true
			}
		}
		if event.Type == "notification" && event.Operation != nil && event.Operation.ID == operation.ID {
			if !finalSeen {
				t.Fatal("Work39 completion notification arrived before its authoritative final snapshot")
			}
			if got := notificationSeverity(t, event); wantSeverity == "success" {
				if got != "info" && got != "warning" {
					t.Fatalf("Work39 operation notification severity = %q, want info or warning", got)
				}
			} else if got != wantSeverity {
				t.Fatalf("Work39 operation notification severity = %q, want %q", got, wantSeverity)
			}
			return final
		}
	}
}

func work39CurrentRow(t *testing.T, state *work39APITestState, matches func(session.Row) bool) session.Row {
	t.Helper()
	state.api.mu.Lock()
	defer state.api.mu.Unlock()
	rows := session.MergeDiscoveryRows(state.api.authoritativeRows, state.api.provisionalRows, state.api.discovery.rows)
	for _, row := range rows {
		if matches(row) {
			return cloneInspectionRow(row)
		}
	}
	t.Fatalf("no current Work39 Resource row matched; generation=%d", state.api.currentG)
	return session.Row{}
}

func work39PlanRequestBody(rowID string) string {
	body, _ := json.Marshal(struct {
		RowID string `json:"rowId"`
	}{RowID: rowID})
	return string(body)
}

func work39ActionRequestBody(rowID, planID string, acceptRisk bool) string {
	body, _ := json.Marshal(struct {
		RowID      string `json:"rowId"`
		PlanID     string `json:"planId"`
		AcceptRisk bool   `json:"acceptRisk"`
	}{RowID: rowID, PlanID: planID, AcceptRisk: acceptRisk})
	return string(body)
}

func decodeWork39ErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode static Work39 API error: %v", err)
	}
	return response.Code
}

// spec: session-interactions/guarded-submission
func TestResourcePlanUsesOnlyCurrentOpaqueUserRowAndStrictBody(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	beforeRequests := state.fixture.requests.Load()
	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{name: "empty object", path: "/api/resource-plan", body: `{}`},
		{name: "empty row id", path: "/api/resource-plan", body: `{"rowId":""}`},
		{name: "duplicate row id", path: "/api/resource-plan", body: `{"rowId":"x","rowId":"y"}`},
		{name: "unknown coordinate", path: "/api/resource-plan", body: `{"rowId":"x","url":"https://untrusted.example"}`},
		{name: "wrong row id type", path: "/api/resource-plan", body: `{"rowId":null}`},
		{name: "query string", path: "/api/resource-plan?rowId=x", body: work39PlanRequestBody(state.rowID)},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, body := postWork39(t, state.api, test.path, test.body)
			if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeInvalidRequest {
				t.Fatalf("invalid resource-plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
			}
		})
	}
	if got := state.fixture.requests.Load(); got != beforeRequests {
		t.Fatalf("malformed plan requests reached SDK: before=%d after=%d", beforeRequests, got)
	}
	assertWork39ProtectedFilesUnchanged(t, state.protected)

	beforeCore, beforeGeneration, beforeSequence, beforeRequestState := inspectionAPIState(t, state.api)
	_, _, wasBusy := state.api.gate.Active()
	if wasBusy {
		t.Fatal("resource-plan state baseline has an active operation")
	}
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("resource-plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	var plan work39PlanResponse
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("decode resource plan: %v", err)
	}
	if plan.RowID != state.rowID || plan.Generation != uint64(state.generation) || plan.Action != session.UserMarketplaceActionInstall || !validWork39PlanID(plan.PlanID) || plan.RequiresRiskAcceptance {
		t.Fatalf("resource plan identity/action/risk = row:%t generation:%t action:%q planID:%t risk:%t", plan.RowID == state.rowID, plan.Generation == uint64(state.generation), plan.Action, validWork39PlanID(plan.PlanID), plan.RequiresRiskAcceptance)
	}
	marker := "\n\n> " + session.DetailTruncationMarker + "\n"
	if len([]byte(plan.Preview)) > session.MaxDetailSourceBytes+len(marker) || strings.Contains(plan.Preview, state.fixture.home) {
		t.Fatalf("resource plan preview is unbounded or contains a local home path")
	}
	afterCore, afterGeneration, afterSequence, afterRequestState := inspectionAPIState(t, state.api)
	if !bytes.Equal(afterCore, beforeCore) || afterGeneration != beforeGeneration || afterSequence != beforeSequence || afterRequestState != beforeRequestState {
		t.Fatalf("resource-plan changed authoritative core, request generation/state, or SSE sequence: generation=%d/%d sequence=%d/%d requests=%+v/%+v", afterGeneration, beforeGeneration, afterSequence, beforeSequence, afterRequestState, beforeRequestState)
	}
	if _, _, active := state.api.gate.Active(); active {
		t.Fatal("resource-plan retained the guarded operation after preparation")
	}
	select {
	case event := <-state.events:
		t.Fatalf("resource-plan published a session event: %s", event.Type)
	default:
	}
	assertWork39ProtectedFilesUnchanged(t, state.protected)
}

// spec: session-interactions/guarded-submission
func TestResourceActionCommitRejectsChangedHarnessTargetsWithoutWrites(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("resource-plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	plan := decodeWork39Plan(t, body)
	if plan.RequiresRiskAcceptance || plan.Action != session.UserMarketplaceActionInstall {
		t.Fatalf("changed-target fixture plan action/risk = %q/%t", plan.Action, plan.RequiresRiskAcceptance)
	}
	if err := config.SetHarnessesInConfig(state.fixture.userConfigPath, []string{"codex"}); err != nil {
		t.Fatalf("change only the user target after preparation: %v", err)
	}
	beforeCommit := work39ProtectedFiles(t, state.fixture)
	userConfig, err := config.ParseV4(mustWork39File(t, state.fixture.userConfigPath))
	if err != nil || len(userConfig.Harnesses) != 1 || userConfig.Harnesses[0] != "codex" {
		t.Fatalf("target change was not present before commit: harnesses=%v err=%v", userConfig.Harnesses, err)
	}

	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, plan.PlanID, false))
	if status != http.StatusAccepted {
		t.Fatalf("prepared action status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	operation := work39OperationResponse(t, body)
	final := waitForWork39Operation(t, state, operation, "error")
	if final.Stage != "final" || final.SettledOperation == nil || final.SettledOperation.ID != operation.ID || len(final.Harnesses) != 1 || final.Harnesses[0] != "codex" {
		t.Fatalf("changed-target final state = stage:%q settled:%#v harnesses:%v", final.Stage, final.SettledOperation, final.Harnesses)
	}
	assertWork39ProtectedFilesUnchanged(t, beforeCommit)
}

// spec: session-interactions/guarded-submission
func TestResourceActionCommitReturnsBusyWithoutConsumingPreparedPlan(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("resource-plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	plan := decodeWork39Plan(t, body)
	beforeCore, beforeGeneration, beforeSequence, beforeRequestState := inspectionAPIState(t, state.api)
	beforeRequests := state.fixture.requests.Load()
	beforeFiles := work39ProtectedFiles(t, state.fixture)
	held, ok := state.api.gate.Begin(session.OperationSync)
	if !ok {
		t.Fatal("could not hold the existing operation gate")
	}
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, plan.PlanID, false))
	state.api.gate.Finish(held)
	if status != http.StatusConflict || decodeWork39ErrorCode(t, body) != codeBusy {
		t.Fatalf("busy confirmation status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	if got := state.fixture.requests.Load(); got != beforeRequests {
		t.Fatalf("busy confirmation reached Marketplace SDK: before=%d after=%d", beforeRequests, got)
	}
	assertWork39ProtectedFilesUnchanged(t, beforeFiles)
	afterCore, afterGeneration, afterSequence, afterRequestState := inspectionAPIState(t, state.api)
	if !bytes.Equal(afterCore, beforeCore) || afterGeneration != beforeGeneration || afterSequence != beforeSequence || afterRequestState != beforeRequestState {
		t.Fatalf("busy confirmation changed authoritative core or request state: generation=%d/%d sequence=%d/%d requests=%+v/%+v", afterGeneration, beforeGeneration, afterSequence, beforeSequence, afterRequestState, beforeRequestState)
	}
	state.api.mu.Lock()
	stored := state.api.resourceActionPlan
	planRetained := stored != nil && stored.planID == plan.PlanID && stored.rowID == state.rowID && stored.generation == state.generation
	state.api.mu.Unlock()
	if !planRetained {
		t.Fatal("busy confirmation consumed the prepared plan")
	}
	if _, _, active := state.api.gate.Active(); active {
		t.Fatal("busy confirmation retained the operation gate")
	}
	select {
	case event := <-state.events:
		t.Fatalf("busy confirmation published a session event: %s", event.Type)
	default:
	}
}

// spec: session-interactions/guarded-submission
func TestResourcePlanRejectsRequiredAndUnsupportedRowsBeforePreparation(t *testing.T) {
	state := newWork39APITestState(t, work39ModeOrgOptional, true, false)
	beforeRequests := state.fixture.requests.Load()
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeNotSupported {
		t.Fatalf("required organization resource plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	if got := state.fixture.requests.Load(); got != beforeRequests {
		t.Fatalf("required resource plan reached Marketplace SDK: before=%d after=%d", beforeRequests, got)
	}
	assertWork39ProtectedFilesUnchanged(t, state.protected)

	generic := newAPI(t, session.DependencyLifecycle{}, nil)
	genericStatus, genericBody := postWork39(t, generic, "/api/resource-plan", `{"rowId":"opaque"}`)
	if genericStatus != http.StatusBadRequest || decodeWork39ErrorCode(t, genericBody) != codeNotSupported {
		t.Fatalf("generic session resource plan status/code = %d/%s", genericStatus, decodeWork39ErrorCode(t, genericBody))
	}
}

// spec: session-interactions/guarded-submission
func TestResourceActionRequestRequiresClosedBodyAndExplicitBoolean(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	planID := strings.Repeat("a", sha256.Size*2)
	bodies := []string{
		`{"rowId":"` + state.rowID + `","planId":"` + planID + `"}`,
		`{"rowId":"` + state.rowID + `","planId":"` + planID + `","acceptRisk":null}`,
		`{"rowId":"` + state.rowID + `","planId":"` + planID + `","acceptRisk":"false"}`,
		`{"rowId":"` + state.rowID + `","planId":"` + planID + `","acceptRisk":false,"scope":"user"}`,
		`{"rowId":"` + state.rowID + `","rowId":"` + state.rowID + `","planId":"` + planID + `","acceptRisk":false}`,
		`{"rowId":"` + state.rowID + `","planId":"` + strings.ToUpper(planID) + `","acceptRisk":false}`,
	}
	beforeRequests := state.fixture.requests.Load()
	for _, body := range bodies {
		status, response := postWork39(t, state.api, "/api/resource-actions", body)
		if status != http.StatusBadRequest || decodeWork39ErrorCode(t, response) != codeInvalidRequest {
			t.Fatalf("invalid resource-action body status/code = %d/%s", status, decodeWork39ErrorCode(t, response))
		}
	}
	status, response := postWork39(t, state.api, "/api/resource-actions?confirm=true", work39ActionRequestBody(state.rowID, planID, false))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, response) != codeInvalidRequest {
		t.Fatalf("resource-action query status/code = %d/%s", status, decodeWork39ErrorCode(t, response))
	}
	if got := state.fixture.requests.Load(); got != beforeRequests {
		t.Fatalf("invalid resource-action bodies reached Marketplace SDK: before=%d after=%d", beforeRequests, got)
	}
	assertWork39ProtectedFilesUnchanged(t, state.protected)
}

// spec: session-interactions/guarded-submission
func TestResourcePlanUsesTheSharedMutationGate(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	held, ok := state.api.gate.Begin(session.OperationSync)
	if !ok {
		t.Fatal("could not hold the existing operation gate")
	}
	beforeRequests := state.fixture.requests.Load()
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	state.api.gate.Finish(held)
	if status != http.StatusConflict || decodeWork39ErrorCode(t, body) != codeBusy {
		t.Fatalf("plan during sync status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	if got := state.fixture.requests.Load(); got != beforeRequests {
		t.Fatalf("busy plan reached Marketplace SDK: before=%d after=%d", beforeRequests, got)
	}
	assertWork39ProtectedFilesUnchanged(t, state.protected)
}

// spec: session-interactions/guarded-submission
func TestResourceActionInstallRequiresRiskAcceptanceAndConsumesPlanOnce(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, true)
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("risk install plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	plan := decodeWork39Plan(t, body)
	if plan.Action != session.UserMarketplaceActionInstall || !plan.RequiresRiskAcceptance || !validWork39PlanID(plan.PlanID) || plan.Generation != uint64(state.generation) {
		t.Fatalf("risk install plan action/risk/id/generation = %q/%t/%t/%d", plan.Action, plan.RequiresRiskAcceptance, validWork39PlanID(plan.PlanID), plan.Generation)
	}
	beforeRequests := state.fixture.requests.Load()
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, plan.PlanID, false))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeInvalidRequest {
		t.Fatalf("risk action without acknowledgement status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	if got := state.fixture.requests.Load(); got != beforeRequests {
		t.Fatalf("risk denial reached the SDK: before=%d after=%d", beforeRequests, got)
	}
	assertWork39ProtectedFilesUnchanged(t, state.protected)

	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, plan.PlanID, true))
	if status != http.StatusAccepted {
		t.Fatalf("risk-accepted action status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	operation := work39OperationResponse(t, body)
	if operation.ID == 0 || operation.Kind != string(session.OperationMutation) {
		t.Fatalf("accepted resource install operation = %#v", operation)
	}
	final := waitForWork39Operation(t, state, operation, "success")
	if final.Stage != "final" || final.Operation != nil || final.SettledOperation == nil || final.SettledOperation.ID != operation.ID {
		t.Fatalf("resource install final snapshot operation state = stage:%q active:%#v settled:%#v", final.Stage, final.Operation, final.SettledOperation)
	}
	installed := assertWork39UserInstallCommitted(t, state, operation, final)

	afterCommit := work39ProtectedFiles(t, state.fixture)
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(rowPresentation(installed).ID, plan.PlanID, true))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeInvalidRequest {
		t.Fatalf("one-shot plan replay status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	assertWork39ProtectedFilesUnchanged(t, afterCommit)
}

func assertWork39UserInstallCommitted(t *testing.T, state *work39APITestState, operation operationRef, final snapshotDTO) session.Row {
	t.Helper()
	if final.Stage != "final" || final.Operation != nil || final.SettledOperation == nil || final.SettledOperation.ID != operation.ID {
		t.Fatalf("resource install final snapshot operation state = stage:%q active:%#v settled:%#v", final.Stage, final.Operation, final.SettledOperation)
	}
	installed := work39CurrentRow(t, state, func(row session.Row) bool {
		action, _ := session.UserMarketplaceAction(row)
		return row.Artifact.Name == "review" && action == session.UserMarketplaceActionRemove
	})
	if installed.MarketplaceDiscovery != nil || !installed.EnabledGlobal || installed.PendingVerification {
		t.Fatalf("committed Marketplace row is not a verified direct user pin: %s", work39ActionFacts(installed))
	}
	userBytes := mustWork39File(t, state.fixture.userConfigPath)
	userConfig, err := config.ParseV4(userBytes)
	if err != nil {
		t.Fatalf("parse user configuration after accepted install: %v", err)
	}
	source, sourceExists := userConfig.Sources["mp"]
	if !sourceExists || source.Kind != config.V4SourceMarketplace || source.Marketplace != state.fixture.endpoint.URL {
		t.Fatalf("user Marketplace registration after install = present:%t kind:%q", sourceExists, source.Kind)
	}
	group := userConfig.Groups["mp"]
	foundInclude := false
	for _, request := range group.Include {
		if request.Registry == "source" && request.Kind == "skill" && request.Name == "review" && request.Options.Revision == "" {
			foundInclude = true
		}
	}
	if !foundInclude {
		t.Fatalf("user config did not commit the exact Work39 include: %+v", group.Include)
	}
	userLock, err := lockfile.Decode(mustWork39File(t, state.fixture.userLockPath))
	if err != nil {
		t.Fatalf("decode user lock after accepted install: %v", err)
	}
	if userLock.Scope != lockfile.ScopeUser || len(userLock.Marketplaces) != 1 || len(userLock.Marketplaces[0].Resources) != 1 {
		t.Fatalf("user lock after accepted install = scope:%q marketplaces:%d", userLock.Scope, len(userLock.Marketplaces))
	}
	pin := userLock.Marketplaces[0].Resources[0]
	if pin.RegistryName != "source" || pin.RegistryID != "src" || pin.Kind != "skill" || pin.Name != "review" || pin.Revision != state.fixture.contentDigest || string(pin.ContentDigest) != state.fixture.contentDigest || string(pin.ArchiveDigest) != state.fixture.archiveDigest || pin.Acquisition != lockfile.MarketplaceAcquisitionMirror {
		t.Fatalf("committed user lock resource did not match the prepared exact revision: %+v", pin)
	}
	materialized, err := os.ReadFile(state.fixture.materializedPath)
	if err != nil || !bytes.Contains(materialized, []byte("# Review code")) {
		t.Fatalf("accepted install did not materialize the skill: exists=%t", err == nil)
	}
	assertWork39ProtectedFilesUnchangedExcept(t, state.protected, state.fixture.userConfigPath, state.fixture.userLockPath, state.fixture.materializedPath)
	return installed
}

// spec: session-interactions/guarded-submission
func TestResourceActionInstallWithAllowedAssessmentCommits(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("allowed install plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	plan := decodeWork39Plan(t, body)
	if plan.Action != session.UserMarketplaceActionInstall || plan.RequiresRiskAcceptance || !validWork39PlanID(plan.PlanID) {
		t.Fatalf("allowed install plan = action:%q risk:%t planID:%t", plan.Action, plan.RequiresRiskAcceptance, validWork39PlanID(plan.PlanID))
	}
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, plan.PlanID, false))
	if status != http.StatusAccepted {
		t.Fatalf("allowed install action status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	operation := work39OperationResponse(t, body)
	if operation.ID == 0 || operation.Kind != string(session.OperationMutation) {
		t.Fatalf("accepted allowed install operation = %#v", operation)
	}
	final := waitForWork39Operation(t, state, operation, "success")
	installed := assertWork39UserInstallCommitted(t, state, operation, final)
	afterCommit := work39ProtectedFiles(t, state.fixture)
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(rowPresentation(installed).ID, plan.PlanID, false))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeInvalidRequest {
		t.Fatalf("one-shot allowed plan replay status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	assertWork39ProtectedFilesUnchanged(t, afterCommit)
}

// spec: session-interactions/guarded-submission
func TestResourceActionRemoveUsesOnlyTheDirectUserPin(t *testing.T) {
	state := newWork39APITestState(t, work39ModeUserInstalled, false, false)
	beforeRequests := state.fixture.requests.Load()
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("remove plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	plan := decodeWork39Plan(t, body)
	if plan.Action != session.UserMarketplaceActionRemove || plan.RequiresRiskAcceptance || validWork39PlanID(plan.PlanID) == false {
		t.Fatalf("user removal plan = action:%q risk:%t planID:%t", plan.Action, plan.RequiresRiskAcceptance, validWork39PlanID(plan.PlanID))
	}
	if got := state.fixture.requests.Load(); got != beforeRequests {
		t.Fatalf("remove planning performed SDK requests: before=%d after=%d", beforeRequests, got)
	}
	if strings.Contains(plan.Preview, state.fixture.endpoint.URL) || strings.Contains(plan.Preview, state.fixture.home) || !strings.Contains(plan.Preview, "user") {
		t.Fatal("user removal preview was not inert or exposed an endpoint/local path")
	}
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, plan.PlanID, false))
	if status != http.StatusAccepted {
		t.Fatalf("user removal action status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	operation := work39OperationResponse(t, body)
	if operation.Kind != string(session.OperationMutation) || operation.ID == 0 {
		t.Fatalf("user removal operation = %#v", operation)
	}
	final := waitForWork39Operation(t, state, operation, "info")
	if final.Stage != "final" || final.SettledOperation == nil || final.SettledOperation.ID != operation.ID {
		t.Fatalf("user removal final state = stage:%q settled:%#v", final.Stage, final.SettledOperation)
	}
	userBytes, err := os.ReadFile(state.fixture.userConfigPath)
	if err != nil {
		t.Fatal("read user configuration after direct pin removal")
	}
	userConfig, err := config.ParseV4(userBytes)
	if err != nil {
		t.Fatalf("parse user configuration after direct pin removal: %v", err)
	}
	for _, request := range userConfig.Requests {
		if request.SourceName == "mp" && request.Kind == "skill" && request.Name == "review" {
			t.Fatal("user configuration retained the directly removed Marketplace pin")
		}
	}
	lockBytes, err := os.ReadFile(state.fixture.userLockPath)
	if err == nil {
		lock, decodeErr := lockfile.Decode(lockBytes)
		if decodeErr != nil {
			t.Fatalf("decode user lock after direct pin removal: %v", decodeErr)
		}
		for _, marketplaceLock := range lock.Marketplaces {
			for _, pin := range marketplaceLock.Resources {
				if pin.Kind == "skill" && pin.Name == "review" {
					t.Fatal("user lock retained the directly removed Marketplace resource")
				}
			}
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("read user lock after direct pin removal: %v", err)
	}
	assertWork39ProtectedFilesUnchangedExcept(t, state.protected, state.fixture.userConfigPath, state.fixture.userLockPath, state.fixture.materializedPath)
}

// spec: session-interactions/guarded-submission
// spec: session-interactions/cancel-or-stale-completion
func TestResourceActionOrganizationExclusionAndRestoreStayInTheUserRoot(t *testing.T) {
	state := newWork39APITestState(t, work39ModeOrgOptional, false, false)
	beforeRequests := state.fixture.requests.Load()
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("organization exclusion plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	plan := decodeWork39Plan(t, body)
	if plan.Action != session.UserMarketplaceActionExclude || plan.RequiresRiskAcceptance || !validWork39PlanID(plan.PlanID) {
		t.Fatalf("organization exclusion plan = action:%q risk:%t planID:%t", plan.Action, plan.RequiresRiskAcceptance, validWork39PlanID(plan.PlanID))
	}
	if got := state.fixture.requests.Load(); got != beforeRequests {
		t.Fatalf("organization exclusion plan contacted the Marketplace SDK: before=%d after=%d", beforeRequests, got)
	}
	if strings.Contains(plan.Preview, state.fixture.endpoint.URL) || strings.Contains(plan.Preview, state.fixture.home) || !strings.Contains(plan.Preview, "user") {
		t.Fatal("organization exclusion preview was not inert or exposed an endpoint/local path")
	}
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, plan.PlanID, false))
	if status != http.StatusAccepted {
		t.Fatalf("organization exclusion status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	operation := work39OperationResponse(t, body)
	final := waitForWork39Operation(t, state, operation, "info")
	if final.Stage != "final" || final.SettledOperation == nil || final.SettledOperation.ID != operation.ID {
		t.Fatalf("organization exclusion final state = stage:%q settled:%#v", final.Stage, final.SettledOperation)
	}
	excluded := work39CurrentRow(t, state, func(row session.Row) bool {
		return row.Artifact.Name == "review" && row.Artifact.Source == config.Organization && row.ExcludedByUser
	})
	selection, ok := excluded.MarketplaceSelections[config.Organization]
	if !ok || !selection.Pin.Direct || selection.Pin.RegistrySlug != "source" {
		t.Fatalf("user exclusion changed the organization direct pin: %s", work39ActionFacts(excluded))
	}
	if action, err := session.UserMarketplaceAction(excluded); err != nil || action != session.UserMarketplaceActionRestore {
		t.Fatalf("excluded organization row action = %q, %v", action, err)
	}
	userV4, err := config.ParseV4(mustWork39File(t, state.fixture.userConfigPath))
	if err != nil {
		t.Fatalf("parse user override after exclusion: %v", err)
	}
	override, ok := userV4.Sources["marketplace-override"]
	if !ok || override.Kind != config.V4SourceMarketplace || override.Marketplace != state.fixture.endpoint.URL {
		t.Fatalf("user override registration did not bind the organization endpoint: present=%t kind=%q", ok, override.Kind)
	}
	exclusions := userV4.Groups["marketplace-override"].Exclude
	if len(exclusions) != 1 || exclusions[0] != "source/skill/review" {
		t.Fatalf("user exclusion selectors = %#v", exclusions)
	}
	if _, err := os.Lstat(state.fixture.userLockPath); !os.IsNotExist(err) {
		t.Fatalf("organization exclusion created a user acquisition lock: %v", err)
	}
	assertWork39ProtectedFilesUnchangedExcept(t, state.protected, state.fixture.userConfigPath, state.fixture.materializedPath)

	restoreRowID := rowPresentation(excluded).ID
	restoreStatus, restoreBody := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(restoreRowID))
	if restoreStatus != http.StatusOK {
		t.Fatalf("organization restore plan status/code = %d/%s", restoreStatus, decodeWork39ErrorCode(t, restoreBody))
	}
	restorePlan := decodeWork39Plan(t, restoreBody)
	if restorePlan.Action != session.UserMarketplaceActionRestore {
		t.Fatalf("organization restore action = %q", restorePlan.Action)
	}
	restoreStatus, restoreBody = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(restoreRowID, restorePlan.PlanID, false))
	if restoreStatus != http.StatusAccepted {
		t.Fatalf("organization restore status/code = %d/%s", restoreStatus, decodeWork39ErrorCode(t, restoreBody))
	}
	restoreOperation := work39OperationResponse(t, restoreBody)
	restoreFinal := waitForWork39Operation(t, state, restoreOperation, "info")
	if restoreFinal.Stage != "final" || restoreFinal.SettledOperation == nil || restoreFinal.SettledOperation.ID != restoreOperation.ID {
		t.Fatalf("organization restore final state = stage:%q settled:%#v", restoreFinal.Stage, restoreFinal.SettledOperation)
	}
	restored := work39CurrentRow(t, state, func(row session.Row) bool {
		return row.Artifact.Name == "review" && row.Artifact.Source == config.Organization && !row.ExcludedByUser
	})
	selection, ok = restored.MarketplaceSelections[config.Organization]
	if !ok || !selection.Pin.Direct || selection.Pin.RegistrySlug != "source" {
		t.Fatalf("restore changed the organization direct pin: %s", work39ActionFacts(restored))
	}
	if action, err := session.UserMarketplaceAction(restored); err != nil || action != session.UserMarketplaceActionExclude {
		t.Fatalf("restored organization row action = %q, %v", action, err)
	}
	restoredV4, err := config.ParseV4(mustWork39File(t, state.fixture.userConfigPath))
	if err != nil {
		t.Fatalf("parse user override after restore: %v", err)
	}
	if len(restoredV4.Groups["marketplace-override"].Exclude) != 0 {
		t.Fatalf("restoration retained user exclusions: %#v", restoredV4.Groups["marketplace-override"].Exclude)
	}
	if _, err := os.Lstat(state.fixture.userLockPath); !os.IsNotExist(err) {
		t.Fatalf("organization restore created a user acquisition lock: %v", err)
	}
	assertWork39ProtectedFilesUnchangedExcept(t, state.protected, state.fixture.userConfigPath, state.fixture.materializedPath)
}

func mustWork39File(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Work39 fixture file: %v", err)
	}
	return data
}

// spec: session-interactions/cancel-or-stale-completion
func TestResourceActionPlansAreOverwrittenAndGenerationBound(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	prepare := func() work39PlanResponse {
		status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
		if status != http.StatusOK {
			t.Fatalf("plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
		}
		return decodeWork39Plan(t, body)
	}
	first := prepare()
	second := prepare()
	if first.PlanID == second.PlanID || second.Generation != uint64(state.generation) {
		t.Fatalf("replacement plans reused an ID or generation: first:%t secondGeneration:%d", first.PlanID == second.PlanID, second.Generation)
	}
	status, body := postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, first.PlanID, false))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeInvalidRequest {
		t.Fatalf("overwritten plan confirmation status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody("not-the-current-row", second.PlanID, false))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeNotReady {
		t.Fatalf("mismatched row confirmation status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, strings.Repeat("0", sha256.Size*2), false))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeInvalidRequest {
		t.Fatalf("nonexistent plan nonce status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	status, body = postWork39(t, state.api, "/api/snapshot", "")
	if status != http.StatusOK {
		t.Fatalf("current-row refresh status = %d", status)
	}
	var refreshed snapshotDTO
	if err := json.Unmarshal(body, &refreshed); err != nil || refreshed.Stage != "final" {
		t.Fatalf("current-row refresh final stage = %q, err=%v", refreshed.Stage, err)
	}
	state.api.mu.Lock()
	newGeneration := state.api.currentG
	state.api.mu.Unlock()
	if newGeneration == state.generation {
		t.Fatal("authoritative snapshot refresh did not advance the generation")
	}
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, second.PlanID, false))
	if status != http.StatusBadRequest || decodeWork39ErrorCode(t, body) != codeNotReady {
		t.Fatalf("stale generation confirmation status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	if _, _, active := state.api.gate.Active(); active {
		t.Fatal("rejected stale confirmation retained the mutation gate")
	}
	assertWork39ProtectedFilesUnchanged(t, state.protected)
}

// spec: session-interactions/cancel-or-stale-completion
func TestResourcePlanRequestCancellationAndAPICloseCancelPreparation(t *testing.T) {
	for _, test := range []struct {
		name     string
		closeAPI bool
	}{
		{name: "request cancelled"},
		{name: "api closed", closeAPI: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newWork39APITestState(t, work39ModeAvailable, false, false)
			state.fixture.holdRegistry.Store(true)
			requestCtx, cancelRequest := context.WithCancel(context.Background())
			defer cancelRequest()
			type response struct {
				status int
				body   []byte
			}
			result := make(chan response, 1)
			request := authedRequest(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID)).WithContext(requestCtx)
			go func() {
				recorder := httptest.NewRecorder()
				state.api.Handler().ServeHTTP(recorder, request)
				result <- response{status: recorder.Code, body: bytes.Clone(recorder.Body.Bytes())}
			}()
			select {
			case <-state.fixture.registryStarted:
			case <-time.After(10 * time.Second):
				t.Fatal("SDK prepare did not reach the held registry lookup")
			}
			if test.closeAPI {
				closeDone := make(chan struct{})
				go func() {
					state.api.Close()
					close(closeDone)
				}()
				select {
				case <-closeDone:
				case <-time.After(10 * time.Second):
					t.Fatal("API.Close did not cancel the held SDK prepare")
				}
			} else {
				cancelRequest()
			}
			var got response
			select {
			case got = <-result:
			case <-time.After(10 * time.Second):
				t.Fatal("cancelled SDK prepare handler did not return")
			}
			if test.closeAPI && (got.status != http.StatusServiceUnavailable || decodeWork39ErrorCode(t, got.body) != codeClosed) {
				t.Fatalf("closed API prepare response status/code = %d/%s", got.status, decodeWork39ErrorCode(t, got.body))
			}
			if bytes.Contains(got.body, []byte(`"planId"`)) {
				t.Fatal("cancelled SDK preparation delivered a plan")
			}
			state.api.mu.Lock()
			planPresent := state.api.resourceActionPlan != nil
			generation := state.api.currentG
			state.api.mu.Unlock()
			if planPresent || generation != state.generation {
				t.Fatalf("cancelled prepare stored a plan or changed generation: plan:%t generation:%d", planPresent, generation)
			}
			if _, _, active := state.api.gate.Active(); active {
				t.Fatal("cancelled prepare retained the guarded operation")
			}
			assertWork39ProtectedFilesUnchanged(t, state.protected)
		})
	}
}

// spec: session-interactions/cancel-or-stale-completion
func TestResourcePlanCloseDiscardsStoredPlan(t *testing.T) {
	state := newWork39APITestState(t, work39ModeAvailable, false, false)
	status, body := postWork39(t, state.api, "/api/resource-plan", work39PlanRequestBody(state.rowID))
	if status != http.StatusOK {
		t.Fatalf("resource plan status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	plan := decodeWork39Plan(t, body)
	state.api.Close()
	state.api.mu.Lock()
	planPresent := state.api.resourceActionPlan != nil
	state.api.mu.Unlock()
	if planPresent {
		t.Fatal("API.Close retained the prepared Resource plan")
	}
	status, body = postWork39(t, state.api, "/api/resource-actions", work39ActionRequestBody(state.rowID, plan.PlanID, false))
	if status != http.StatusServiceUnavailable || decodeWork39ErrorCode(t, body) != codeClosed {
		t.Fatalf("confirm after API.Close status/code = %d/%s", status, decodeWork39ErrorCode(t, body))
	}
	assertWork39ProtectedFilesUnchanged(t, state.protected)
}
