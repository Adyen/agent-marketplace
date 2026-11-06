package localapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cachefs "github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/session"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type installedSkillFilesResponseDTO struct {
	RowID              string              `json:"rowId"`
	Generation         uint64              `json:"generation"`
	Files              []inspectionFileDTO `json:"files"`
	PrimaryFileID      string              `json:"primaryFileId"`
	SelectedFileID     string              `json:"selectedFileId"`
	Content            string              `json:"content"`
	Truncated          bool                `json:"truncated"`
	UnsupportedMessage string              `json:"unsupportedMessage"`
}

type work37InstalledFixture struct {
	home             string
	endpoint         config.MarketplaceEntry
	requests         atomic.Int64
	resource         *ir.Skill
	support          map[string][]byte
	contentDigest    string
	archiveDigest    string
	archivePath      string
	markerPath       string
	revisionPath     string
	stagePath        string
	orgConfigPath    string
	cacheRoot        string
	cacheEntries     []string
	protected        []discoveryFixtureFile
	baselineRequests int64
}

func setupWork37InstalledFixture(t *testing.T, home string, bootstrap map[string]string, withSupport bool) *work37InstalledFixture {
	t.Helper()
	resource := ir.NewSkill("review", "Review code", "# Installed review\n", []string{"user"}, nil, ir.SourceRef{})
	support := map[string][]byte{}
	canonicalSupport := map[string]hccartifact.SupportFile{}
	if withSupport {
		resource.SupportFiles = []string{"notes.md", "scripts/helper.sh"}
		support["notes.md"] = []byte("# Supporting review\n")
		support["scripts/helper.sh"] = []byte("printf \"review\"\n")
		canonicalSupport["notes.md"] = hccartifact.SupportFile{Data: support["notes.md"]}
		canonicalSupport["scripts/helper.sh"] = hccartifact.SupportFile{Data: support["scripts/helper.sh"], Executable: true}
	}
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatalf("FromIR installed fixture: %v", err)
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
	canonical.Support = canonicalSupport
	if !withSupport {
		canonical.Assets = map[string][]byte{"logo.png": []byte("\x89PNG\r\n\x1a\nfake-image")}
	}
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode installed fixture: %v", err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	coordinate := store.ResourceCoordinate{Marketplace: store.MarketplaceID("mp"), Registry: store.RegistryID("src"), Kind: "skill", Name: "review"}
	content := store.RevisionDigest(contentDigest)
	archive := store.ArchiveDigest(archiveDigest)
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               coordinate,
		ContentDigest:            content,
		AnalyzerVersion:          "gui-test",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	revision := store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: content, ArchiveDigest: archive},
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: coordinate, ContentDigest: content, Commit: strings.Repeat("a", 40),
			ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}
	fixture := &work37InstalledFixture{home: home, resource: resource, support: support, contentDigest: contentDigest, archiveDigest: archiveDigest, cacheRoot: cachefs.CacheDir()}
	var server *httptest.Server
	revisionPath := "/api/v1/resources/src/skill/review/revisions/" + contentDigest
	artifactPath := "/api/v1/artifacts/sha256/" + strings.TrimPrefix(archiveDigest, "sha256:")
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.requests.Add(1)
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", server.URL))
		case revisionPath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(revision)
		case artifactPath:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	fixture.endpoint = config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Alias: "mp"}

	cache := marketplace.DefaultCache()
	fixture.archivePath, err = cache.WriteArtifact(archiveDigest, raw)
	if err != nil {
		t.Fatalf("seed cached artifact: %v", err)
	}
	if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("mark cached artifact verified: %v", err)
	}
	fixture.markerPath = filepath.Join(filepath.Dir(fixture.archivePath), "verified.marker")
	fixture.revisionPath, err = cache.WriteResourceRevision(server.URL, "src", "skill", "review", contentDigest, revision)
	if err != nil {
		t.Fatalf("seed cached resource revision: %v", err)
	}
	fixture.stagePath = cache.MirrorResourceDir(archiveDigest)
	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint: fixture.endpoint, Items: []mpapi.ResourceListItem{}, Total: 0, ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatalf("seed fresh empty Work37 discovery page: %v", err)
	}

	fixture.orgConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	organization := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n"
	writeFile(t, fixture.orgConfigPath, organization)
	config.SetTestOrganizationConfigPath(t, fixture.orgConfigPath)
	if err := config.ValidateOrganizationTrustUnit(fixture.orgConfigPath, true, true); err != nil {
		t.Fatalf("validate trusted Work37 organization config: %v", err)
	}
	userConfig := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: " + server.URL + "}\nresources:\n  mp:\n    include:\n      - source/skill/review: {revision: " + contentDigest + "}\n"
	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userConfigPath, userConfig)
	lockPaths, err := config.ResolveConfigLockPathsTolerateMissing(userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	lockData, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL:        server.URL,
			InstanceID: "mp",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{
				Name: "source", RegistryID: "src",
			}},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName:  "source",
				RegistryID:    "src",
				Kind:          "skill",
				Name:          "review",
				Revision:      contentDigest,
				ContentDigest: lockfile.Digest(contentDigest),
				ArchiveDigest: lockfile.Digest(archiveDigest),
				Acquisition:   lockfile.MarketplaceAcquisitionMirror,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, lockPaths.CanonicalLock, string(lockData))
	if bootstrap != nil {
		bootstrap["orgConfig"] = fixture.orgConfigPath
		bootstrap["marketplaceEndpoint"] = server.URL
	}
	return fixture
}

type work37APITestState struct {
	api             *API
	fixture         *work37InstalledFixture
	row             session.Row
	rowID           string
	generation      session.RequestGeneration
	protected       []discoveryFixtureFile
	sdkRequestCount int64
}

func newWork37APITestState(t *testing.T, withSupport bool) *work37APITestState {
	t.Helper()
	home, _ := newUserAPIFixture(t, true)
	fixture := setupWork37InstalledFixture(t, home, nil, withSupport)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	events := streamEvents(t, api)
	waitForInspectionFinal(t, api, events)

	api.mu.Lock()
	rowID := ""
	var snapshotRow rowDTO
	for _, row := range api.current.Rows {
		if row.Name == "review" && !row.Discovery {
			rowID = row.ID
			snapshotRow = row
			break
		}
	}
	generation := api.currentG
	api.mu.Unlock()
	if rowID == "" {
		t.Fatal("NewUser pipeline did not publish the installed Marketplace skill")
	}
	if !snapshotRow.InstalledFiles || snapshotRow.Inspectable {
		t.Fatalf("installed row capabilities = files:%t SDK:%t", snapshotRow.InstalledFiles, snapshotRow.Inspectable)
	}
	row, capturedGeneration, code := api.visibleResourceRow(rowID, session.MarketplaceInstalledSkillFiles)
	if code != "" || capturedGeneration != generation || !session.MarketplaceInstalledSkillFiles(row) {
		t.Fatalf("NewUser installed row lookup = generation %d/%d, code %q, row %+v", capturedGeneration, generation, code, row)
	}
	missingPathRow := row
	missingPathRow.Artifact.Path = ""
	if !rowPresentation(missingPathRow).InstalledFiles {
		t.Fatal("missing installed source path hid the installed-files capability")
	}
	if row.Artifact.Path == "" || row.Artifact.Resource == nil || row.MarketplaceDiscovery != nil || row.PendingVerification {
		t.Fatalf("installed row did not come from the normal resolver pipeline: %+v", row.Artifact)
	}
	if withSupport && row.Artifact.Path != fixture.stagePath {
		t.Fatalf("resolver staged support path = %q, want %q", row.Artifact.Path, fixture.stagePath)
	}
	if !withSupport && row.Artifact.Path != fixture.archivePath {
		t.Fatalf("resolver archive path = %q, want %q", row.Artifact.Path, fixture.archivePath)
	}
	return &work37APITestState{
		api:             api,
		fixture:         fixture,
		row:             row,
		rowID:           rowID,
		generation:      capturedGeneration,
		protected:       captureWork37InstalledProtected(t, home, fixture, row),
		sdkRequestCount: fixture.requests.Load(),
	}
}

func decodeInstalledSkillFilesResponse(t *testing.T, recorder *httptest.ResponseRecorder) installedSkillFilesResponseDTO {
	t.Helper()
	var response installedSkillFilesResponseDTO
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode installed skill files response: %v", err)
	}
	return response
}

func expectedInstalledSkillFileID(rowID string, generation uint64, file session.InstalledSkillFile) string {
	identity, _ := json.Marshal([4]any{rowID, generation, file.Path, file.Identity})
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:])
}

type work37APISnapshot struct {
	payload    []byte
	generation uint64
	sequence   uint64
	requests   session.RequestState
	operation  session.OperationID
	kind       session.OperationKind
	active     bool
}

func captureWork37APISnapshot(t *testing.T, api *API) work37APISnapshot {
	t.Helper()
	payload, generation, sequence, requests := inspectionAPIState(t, api)
	operation, kind, active := api.gate.Active()
	return work37APISnapshot{payload: payload, generation: generation, sequence: sequence, requests: requests, operation: operation, kind: kind, active: active}
}

func assertWork37APIUnchanged(t *testing.T, state *work37APITestState, before work37APISnapshot) {
	t.Helper()
	after := captureWork37APISnapshot(t, state.api)
	if !bytes.Equal(after.payload, before.payload) || after.generation != before.generation || after.sequence != before.sequence || after.requests != before.requests {
		t.Fatalf("installed-file browsing changed core API state: generation=%d/%d sequence=%d/%d requests=%+v/%+v", after.generation, before.generation, after.sequence, before.sequence, after.requests, before.requests)
	}
	if after.operation != before.operation || after.kind != before.kind || after.active != before.active {
		t.Fatalf("installed-file browsing changed operation gate: before=%d/%q/%t after=%d/%q/%t", before.operation, before.kind, before.active, after.operation, after.kind, after.active)
	}
	assertInspectionFilesUnchanged(t, state.protected)
	assertWork37CacheTreeUnchanged(t, state.fixture)
	if got := state.fixture.requests.Load(); got != state.sdkRequestCount {
		t.Fatalf("installed-file browsing made SDK requests: before=%d after=%d", state.sdkRequestCount, got)
	}
}

// spec: session-navigation/installed-skill-definition-and-supporting-files-remain-browsable
func TestUserAPIInstalledSkillFilesReturnsVerifiedPrimaryAndSupportingFilesReadOnly(t *testing.T) {
	state := newWork37APITestState(t, true)
	before := captureWork37APISnapshot(t, state.api)

	primary := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", `{"rowId":"`+state.rowID+`"}`, state.api.Credential(), "")
	if primary.Code != http.StatusOK || primary.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("primary installed files status/cache-control = %d/%q body=%s", primary.Code, primary.Header().Get("Cache-Control"), primary.Body.String())
	}
	var rawPayload map[string]json.RawMessage
	if err := json.Unmarshal(primary.Body.Bytes(), &rawPayload); err != nil {
		t.Fatalf("decode raw installed files payload: %v", err)
	}
	for _, forbidden := range []string{"revision", "manifestDigest"} {
		if _, ok := rawPayload[forbidden]; ok {
			t.Fatalf("installed payload fabricated remote provenance field %q", forbidden)
		}
	}
	primaryResponse := decodeInstalledSkillFilesResponse(t, primary)
	if primaryResponse.RowID != state.rowID || primaryResponse.Generation != uint64(state.generation) || primaryResponse.PrimaryFileID == "" || primaryResponse.SelectedFileID != primaryResponse.PrimaryFileID || primaryResponse.Truncated || primaryResponse.UnsupportedMessage != "" {
		t.Fatalf("primary installed files response = %+v", primaryResponse)
	}
	if !strings.Contains(primaryResponse.Content, "# Installed review") || strings.Contains(primaryResponse.Content, `"Body"`) || strings.Contains(primaryResponse.Content, `"SupportFiles"`) {
		t.Fatalf("installed primary preview is not inert Factory Markdown: %q", primaryResponse.Content)
	}
	files := make(map[string]inspectionFileDTO, len(primaryResponse.Files))
	for _, file := range primaryResponse.Files {
		if len(file.ID) != 64 || strings.Trim(file.ID, "0123456789abcdef") != "" {
			t.Fatalf("installed file ID is not opaque lower-case SHA256: %#v", file)
		}
		if strings.Contains(file.Path, ".cache") || strings.Contains(file.Path, "sha256_") || strings.Contains(file.Path, "artifact.tar.gz") || strings.Contains(file.Path, "verified.marker") {
			t.Fatalf("installed file listing exposed cache implementation path %q", file.Path)
		}
		files[file.Path] = file
	}
	currentBundle, err := session.OpenInstalledSkillBundle(state.row)
	if err != nil {
		t.Fatalf("open normal-resolver installed bundle for ID oracle: %v", err)
	}
	defer func() { _ = currentBundle.Close() }()
	for _, member := range currentBundle.Files() {
		if got := files[member.Path].ID; got != expectedInstalledSkillFileID(state.rowID, uint64(state.generation), member) {
			t.Fatalf("installed file ID for %q = %q, want generation/path/source-bound identity", member.Path, got)
		}
	}
	for _, path := range []string{"SKILL.md", "notes.md", "scripts/helper.sh"} {
		if _, ok := files[path]; !ok {
			t.Fatalf("installed file listing missing %q: %#v", path, primaryResponse.Files)
		}
	}
	if len(files) != 3 {
		t.Fatalf("staged installed file listing = %#v, want primary and two declared members", primaryResponse.Files)
	}
	if files["scripts/helper.sh"].InspectionClass != "text" {
		t.Fatalf("script file class = %q, want inert text", files["scripts/helper.sh"].InspectionClass)
	}

	for path, want := range state.fixture.support {
		file := files[path]
		response := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", `{"rowId":"`+state.rowID+`","fileId":"`+file.ID+`"}`, state.api.Credential(), "")
		if response.Code != http.StatusOK {
			t.Fatalf("installed member %q status = %d body=%s", path, response.Code, response.Body.String())
		}
		payload := decodeInstalledSkillFilesResponse(t, response)
		if payload.RowID != state.rowID || payload.Generation != uint64(state.generation) || payload.SelectedFileID != file.ID || payload.PrimaryFileID != primaryResponse.PrimaryFileID || payload.Content != string(want) || payload.Truncated || payload.UnsupportedMessage != "" {
			t.Fatalf("installed member %q response = %+v", path, payload)
		}
	}

	assertWork37APIUnchanged(t, state, before)
}

func TestUserAPIInstalledSkillFilesReturnsArchiveRasterMetadataWithoutBytes(t *testing.T) {
	state := newWork37APITestState(t, false)
	before := captureWork37APISnapshot(t, state.api)
	primary := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", `{"rowId":"`+state.rowID+`"}`, state.api.Credential(), "")
	if primary.Code != http.StatusOK {
		t.Fatalf("archive installed files status = %d body=%s", primary.Code, primary.Body.String())
	}
	payload := decodeInstalledSkillFilesResponse(t, primary)
	var image inspectionFileDTO
	for _, file := range payload.Files {
		if file.Path == "assets/logo.png" {
			image = file
		}
	}
	if image.ID == "" || image.InspectionClass != "image" {
		t.Fatalf("archive raster metadata missing: %#v", payload.Files)
	}
	response := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", `{"rowId":"`+state.rowID+`","fileId":"`+image.ID+`"}`, state.api.Credential(), "")
	if response.Code != http.StatusOK {
		t.Fatalf("raster metadata request status = %d body=%s", response.Code, response.Body.String())
	}
	selected := decodeInstalledSkillFilesResponse(t, response)
	if selected.SelectedFileID != image.ID || selected.Content != "" || !strings.Contains(selected.UnsupportedMessage, "image/png") {
		t.Fatalf("raster response included bytes or omitted static metadata: %+v", selected)
	}
	assertWork37APIUnchanged(t, state, before)
}

func TestUserAPIInstalledSkillFilesRejectsStrictAuthorityWithoutSDKTraffic(t *testing.T) {
	state := newWork37APITestState(t, true)
	before := captureWork37APISnapshot(t, state.api)
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
		`{"rowId":"row","path":"/tmp/skill/SKILL.md"}`,
		`{"rowId":"row","resource":"untrusted"}`,
	}
	for _, body := range invalid {
		recorder := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", body, state.api.Credential(), "")
		if recorder.Code != http.StatusBadRequest || responseErrorCode(t, recorder) != codeInvalidRequest {
			t.Fatalf("malformed installed-file request = %d/%s", recorder.Code, responseErrorCode(t, recorder))
		}
	}
	for _, request := range []struct {
		method string
		path   string
		token  string
		origin string
		status int
	}{
		{method: http.MethodPost, path: "/api/installed-skill-files?fileId=ignored", token: state.api.Credential(), status: http.StatusBadRequest},
		{method: http.MethodGet, path: "/api/installed-skill-files", token: state.api.Credential(), status: http.StatusMethodNotAllowed},
		{method: http.MethodPost, path: "/api/installed-skill-files", token: state.api.Credential(), origin: "https://invalid.example", status: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/installed-skill-files", status: http.StatusUnauthorized},
	} {
		recorder := serveDiscoveryRequest(t, state.api, request.method, request.path, `{"rowId":"`+state.rowID+`"}`, request.token, request.origin)
		if recorder.Code != request.status {
			t.Fatalf("installed-file boundary request status = %d, want %d", recorder.Code, request.status)
		}
	}
	unknownRow := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", `{"rowId":"unknown-row"}`, state.api.Credential(), "")
	if unknownRow.Code != http.StatusBadRequest || responseErrorCode(t, unknownRow) != codeNotReady {
		t.Fatalf("unknown installed row = %d/%s, want 400/%s", unknownRow.Code, responseErrorCode(t, unknownRow), codeNotReady)
	}
	primary := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", `{"rowId":"`+state.rowID+`"}`, state.api.Credential(), "")
	if primary.Code != http.StatusOK {
		t.Fatalf("installed file ID control status = %d body=%s", primary.Code, primary.Body.String())
	}
	listed := decodeInstalledSkillFilesResponse(t, primary)
	knownIDs := make(map[string]struct{}, len(listed.Files))
	for _, file := range listed.Files {
		knownIDs[file.ID] = struct{}{}
	}
	unknownID := strings.Repeat("0", 64)
	if _, exists := knownIDs[unknownID]; exists {
		unknownID = strings.Repeat("1", 64)
	}
	unknownFile := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", `{"rowId":"`+state.rowID+`","fileId":"`+unknownID+`"}`, state.api.Credential(), "")
	if unknownFile.Code != http.StatusBadRequest || responseErrorCode(t, unknownFile) != codeInvalidRequest {
		t.Fatalf("unknown installed file = %d/%s, want 400/%s", unknownFile.Code, responseErrorCode(t, unknownFile), codeInvalidRequest)
	}
	generic := newAPI(t, session.DependencyLifecycle{}, nil)
	unsupported := serveDiscoveryRequest(t, generic, http.MethodPost, "/api/installed-skill-files", `{"rowId":"row"}`, generic.Credential(), "")
	if unsupported.Code != http.StatusBadRequest || responseErrorCode(t, unsupported) != codeNotSupported {
		t.Fatalf("generic-session installed-file route = %d/%s, want 400/%s", unsupported.Code, responseErrorCode(t, unsupported), codeNotSupported)
	}
	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledRequest := httptest.NewRequest(http.MethodPost, "/api/installed-skill-files", strings.NewReader(`{"rowId":"`+state.rowID+`"}`)).WithContext(cancelledContext)
	cancelledRequest.Header.Set(tokenHeader, state.api.Credential())
	cancelled := httptest.NewRecorder()
	state.api.Handler().ServeHTTP(cancelled, cancelledRequest)
	if cancelled.Body.Len() != 0 {
		t.Fatalf("cancelled installed-file request delivered a response: %s", cancelled.Body.String())
	}
	assertWork37APIUnchanged(t, state, before)
}

func TestUserAPIInstalledSkillFilesMissingOrCorruptContentIsStatic(t *testing.T) {
	for _, tc := range []struct {
		name        string
		withSupport bool
		mutate      func(t *testing.T, state *work37APITestState)
	}{
		{name: "missing declared staged member", withSupport: true, mutate: func(t *testing.T, state *work37APITestState) {
			if err := os.Remove(filepath.Join(state.row.Artifact.Path, "notes.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "corrupt verified archive", mutate: func(t *testing.T, state *work37APITestState) {
			if err := os.WriteFile(state.row.Artifact.Path, []byte("corrupt cached artifact"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newWork37APITestState(t, tc.withSupport)
			tc.mutate(t, state)
			state.protected = captureWork37InstalledProtected(t, state.fixture.home, state.fixture, state.row)
			state.sdkRequestCount = state.fixture.requests.Load()
			before := captureWork37APISnapshot(t, state.api)
			response := serveDiscoveryRequest(t, state.api, http.MethodPost, "/api/installed-skill-files", `{"rowId":"`+state.rowID+`"}`, state.api.Credential(), "")
			if response.Code != http.StatusBadGateway || responseErrorCode(t, response) != "localapi.resource_files_unavailable" {
				t.Fatalf("unavailable installed content = %d/%s", response.Code, responseErrorCode(t, response))
			}
			var payload struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Message != "Resource files are unavailable; refresh this resource and retry." {
				t.Fatalf("unavailable message = %q", payload.Message)
			}
			for _, private := range []string{state.fixture.endpoint.URL, state.row.Artifact.Path, "artifact.tar.gz", "verified.marker", ".cache", "sha256_"} {
				if strings.Contains(response.Body.String(), private) {
					t.Fatalf("unavailable response leaked implementation detail %q: %s", private, response.Body.String())
				}
			}
			assertWork37APIUnchanged(t, state, before)
		})
	}
}

func TestUserAPIInstalledSkillFilesSupersededGenerationIsNotDelivered(t *testing.T) {
	state := newWork37APITestState(t, true)
	response := installedSkillFilesPayloadDTO{
		RowID: state.rowID, Generation: uint64(state.generation), Content: "stale installed content",
	}
	state.api.mu.Lock()
	nextGeneration := state.api.beginSnapshotLocked()
	state.api.mu.Unlock()
	if state.api.deliverSnapshot(nextGeneration, session.Snapshot{
		Generation: nextGeneration, Stage: session.SnapshotStageFinal, DriftAssessment: session.DriftReady,
		Rows: []session.Row{}, MergedCfg: &config.Config{},
	}) == nil {
		t.Fatal("new snapshot did not replace installed row A with row set B")
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/installed-skill-files", strings.NewReader(`{"rowId":"`+state.rowID+`"}`))
	state.api.deliverInstalledSkillFiles(recorder, request, state.rowID, state.generation, response)
	if recorder.Code != http.StatusBadRequest || responseErrorCode(t, recorder) != codeNotReady || strings.Contains(recorder.Body.String(), "stale installed content") {
		t.Fatalf("superseded installed payload was delivered: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestWork37CacheEntryOracleRejectsNewFile(t *testing.T) {
	state := newWork37APITestState(t, true)
	baselineEntries, _ := captureWork37Cache(t, state.fixture.cacheRoot)
	if !cacheEntriesEqual(baselineEntries, state.fixture.cacheEntries) {
		t.Fatal("fresh cache capture did not match the NewUser baseline")
	}
	unchangedEntries, _ := captureWork37Cache(t, state.fixture.cacheRoot)
	if !cacheEntriesEqual(baselineEntries, unchangedEntries) {
		t.Fatal("cache entry oracle rejected an unchanged positive control")
	}
	positiveControl := filepath.Join(state.fixture.cacheRoot, "work37-positive-new-file")
	if _, err := os.Lstat(positiveControl); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache positive-control path already exists: %v", err)
	}
	if err := os.WriteFile(positiveControl, []byte("work37 positive cache-entry control\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changedEntries, _ := captureWork37Cache(t, state.fixture.cacheRoot)
	if cacheEntriesEqual(unchangedEntries, changedEntries) {
		t.Fatal("cache entry oracle accepted a new fixture-owned file")
	}
	if len(changedEntries) != len(unchangedEntries)+1 {
		t.Fatalf("cache positive control added unexpected entries: before=%d after=%d", len(unchangedEntries), len(changedEntries))
	}
	assertInspectionFilesUnchanged(t, state.protected)
}

func captureWork37InstalledProtected(t *testing.T, home string, fixture *work37InstalledFixture, row session.Row) []discoveryFixtureFile {
	t.Helper()
	if !filepath.IsAbs(fixture.cacheRoot) {
		t.Fatalf("Work37 cache root is not absolute: %q", fixture.cacheRoot)
	}
	cacheRelative, err := filepath.Rel(home, fixture.cacheRoot)
	if err != nil || cacheRelative == ".." || strings.HasPrefix(cacheRelative, ".."+string(filepath.Separator)) || filepath.IsAbs(cacheRelative) {
		t.Fatalf("Work37 cache root escapes fixture home: root=%q relative=%q err=%v", fixture.cacheRoot, cacheRelative, err)
	}
	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	userLocks, err := config.ResolveConfigLockPathsTolerateMissing(userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	orgLocks, err := config.ResolveConfigLockPathsTolerateMissing(fixture.orgConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		userConfigPath,
		fixture.orgConfigPath,
		userLocks.CanonicalLock,
		orgLocks.CanonicalLock,
		filepath.Join(home, ".devin", "skills", "review", "SKILL.md"),
		fixture.archivePath,
		fixture.markerPath,
		fixture.revisionPath,
	}
	if row.Artifact.Path == fixture.stagePath {
		paths = append(paths, filepath.Join(fixture.stagePath, "SKILL.md"))
		for path := range fixture.support {
			paths = append(paths, filepath.Join(fixture.stagePath, filepath.FromSlash(path)))
		}
	}
	protected := make([]discoveryFixtureFile, 0, len(paths))
	for _, path := range paths {
		protected = append(protected, captureDiscoveryFixtureFile(t, path))
	}
	cacheEntries, cacheFiles := captureWork37Cache(t, fixture.cacheRoot)
	if len(cacheEntries) == 0 || len(cacheFiles) == 0 {
		t.Fatal("Work37 cache baseline did not contain seeded entries and files")
	}
	fixture.cacheEntries = cacheEntries
	protected = append(protected, cacheFiles...)
	return protected
}

func captureWork37Cache(t *testing.T, root string) ([]string, []discoveryFixtureFile) {
	t.Helper()
	var entries []string
	var files []discoveryFixtureFile
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return nil
		}
		relativePath = filepath.ToSlash(relativePath)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entries = append(entries, "symlink:"+relativePath)
		case info.IsDir():
			entries = append(entries, "directory:"+relativePath)
		case info.Mode().IsRegular():
			entries = append(entries, "file:"+relativePath)
			files = append(files, captureDiscoveryFixtureFile(t, path))
		default:
			entries = append(entries, "special:"+relativePath)
		}
		return nil
	}); err != nil {
		t.Fatalf("capture Work37 cache entry paths: %v", err)
	}
	sort.Strings(entries)
	return entries, files
}

func cacheEntriesEqual(before, after []string) bool {
	return slices.Equal(before, after)
}

func assertWork37CacheTreeUnchanged(t *testing.T, fixture *work37InstalledFixture) {
	t.Helper()
	entries, _ := captureWork37Cache(t, fixture.cacheRoot)
	if !cacheEntriesEqual(fixture.cacheEntries, entries) {
		t.Fatalf("Work37 cache entry set changed: before=%d after=%d", len(fixture.cacheEntries), len(entries))
	}
}
