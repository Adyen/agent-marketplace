package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type work40DirectoryMode string

const (
	work40RootOwned         work40DirectoryMode = "root-owned"
	work40SourceIncluded    work40DirectoryMode = "source-included"
	work40SelectionIncluded work40DirectoryMode = "selection-included"
)

type work40ProtectedFile struct {
	path   string
	exists bool
	mode   os.FileMode
	data   []byte
}

type work40DirectoryFixture struct {
	home                  string
	repo                  string
	mode                  work40DirectoryMode
	userConfigPath        string
	userConfigBytes       []byte
	userFragmentPath      string
	userLockPath          string
	orgConfigPath         string
	orgLockPath           string
	registryDir           string
	reviewSourcePath      string
	nextSourcePath        string
	reviewMaterialized    string
	nextMaterialized      string
	unrelatedMaterialized string
	protected             []work40ProtectedFile
}

func work40IdentityTestRow(url string, kind config.V4SourceKind) session.Row {
	entry := config.RegistryEntry{URL: url, V4Kind: kind}
	ref, path := "main", "skills"
	if entry.IsLocal() {
		ref, path = "", ""
	}
	return session.Row{
		Artifact: resolver.ResolvedArtifact{
			Entry:        catalog.Entry{Kind: ir.KindSkill, Name: "review", SourceHarness: "devin"},
			RegistryURL:  url,
			RegistryPath: path,
			Ref:          ref,
			V4Kind:       kind,
			Source:       config.UserPersonal,
		},
		RegistryURL:   url,
		RegistryPath:  path,
		RegistryRef:   ref,
		HarnessSource: "devin",
	}
}

// spec: session-interactions/guarded-submission
func TestWork40UserRowIdentityPreservesScopeAndRegistryCoordinates(t *testing.T) {
	userAPI := &API{userOnly: true}
	available := work40IdentityTestRow(t.TempDir(), config.V4SourceDirectory)
	availableDTO := userAPI.resourceRowPresentation(available)
	if availableDTO.Scope != "available" || availableDTO.ResourceAction != session.UserMarketplaceActionInstall {
		t.Fatalf("available directory projection = scope %q, action %q", availableDTO.Scope, availableDTO.ResourceAction)
	}

	selected := available
	selected.EnabledGlobal = true
	selectedDTO := userAPI.resourceRowPresentation(selected)
	if selectedDTO.Scope != "user" || selectedDTO.ResourceAction != session.UserMarketplaceActionRemove || selectedDTO.ID != availableDTO.ID {
		t.Fatalf("selected directory projection changed scope/action/ID: %q/%q/%t", selectedDTO.Scope, selectedDTO.ResourceAction, selectedDTO.ID == availableDTO.ID)
	}
	disabled := selected
	disabled.EnabledGlobal = false
	disabledDTO := userAPI.resourceRowPresentation(disabled)
	if disabledDTO.Scope != "available" || disabledDTO.ResourceAction != session.UserMarketplaceActionInstall || disabledDTO.ID != availableDTO.ID {
		t.Fatalf("disabled directory projection changed scope/action/ID: %q/%q/%t", disabledDTO.Scope, disabledDTO.ResourceAction, disabledDTO.ID == availableDTO.ID)
	}

	genericAvailable, genericSelected := rowPresentation(available), rowPresentation(selected)
	if genericAvailable.Scope != "available" || genericSelected.Scope != "user" || genericAvailable.ID == genericSelected.ID {
		t.Fatal("generic projection no longer preserves scope-sensitive row identity")
	}

	git := work40IdentityTestRow("https://example.test/repo.git", config.V4SourceGit)
	git.RegistryRef, git.Artifact.Ref = "main", "main"
	git.RegistryPath, git.Artifact.RegistryPath = "skills", "skills"
	gitID := userAPI.resourceRowPresentation(git).ID
	for _, variant := range []struct {
		name string
		edit func(*session.Row)
	}{
		{name: "origin", edit: func(row *session.Row) {
			row.RegistryURL, row.Artifact.RegistryURL = "https://example.test/other.git", "https://example.test/other.git"
		}},
		{name: "ref", edit: func(row *session.Row) { row.RegistryRef, row.Artifact.Ref = "release", "release" }},
		{name: "path", edit: func(row *session.Row) { row.RegistryPath, row.Artifact.RegistryPath = "other", "other" }},
		{name: "kind", edit: func(row *session.Row) { row.Artifact.Kind = ir.KindCommand }},
		{name: "name", edit: func(row *session.Row) { row.Artifact.Name = "next" }},
	} {
		t.Run(variant.name, func(t *testing.T) {
			candidate := git
			variant.edit(&candidate)
			if userAPI.resourceRowPresentation(candidate).ID == gitID {
				t.Fatal("distinct registry coordinates shared a user row ID")
			}
		})
	}

	marketplace := work40IdentityTestRow("https://example.test/marketplace.git", config.V4SourceGit)
	marketplace.Artifact.IsMarketplace = true
	marketplaceDTO := userAPI.resourceRowPresentation(marketplace)
	if marketplaceDTO.ID != rowPresentation(marketplace).ID {
		t.Fatal("user-only Marketplace projection changed its original ID")
	}
	marketplaceSelected := marketplace
	marketplaceSelected.EnabledGlobal = true
	marketplaceSelectedDTO := userAPI.resourceRowPresentation(marketplaceSelected)
	if marketplaceSelectedDTO.ID != rowPresentation(marketplaceSelected).ID || marketplaceSelectedDTO.ID == marketplaceDTO.ID {
		t.Fatal("Marketplace user/generic identity no longer matches its original scope-sensitive behavior")
	}
}

func writeWork40File(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal("create private Work40 fixture parent")
	}
	if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
		t.Fatal("write private Work40 fixture file")
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal("set private Work40 fixture mode")
	}
}

func setupWork40DirectoryFixture(t *testing.T, home, repo string, bootstrap map[string]string, mode work40DirectoryMode) *work40DirectoryFixture {
	t.Helper()
	if mode != work40RootOwned && mode != work40SourceIncluded && mode != work40SelectionIncluded {
		t.Fatal("unsupported Work40 directory fixture mode")
	}
	registryDir := t.TempDir()
	reviewBody := "---\nname: review\ndescription: Review code\n---\n# Review code\n"
	nextBody := "---\nname: next\ndescription: Next code\n---\n# Next code\n"
	reviewSourcePath := filepath.Join(registryDir, "review", "SKILL.md")
	nextSourcePath := filepath.Join(registryDir, "next", "SKILL.md")
	writeWork40File(t, reviewSourcePath, reviewBody)
	writeWork40File(t, nextSourcePath, nextBody)

	userConfigPath := filepath.Join(home, ".agents", "config.yaml")
	userFragmentPath := filepath.Join(home, ".agents", "fragment.yaml")
	var userConfig, userFragment string
	switch mode {
	case work40RootOwned:
		userConfig = "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nsources:\n  local: {directory: " + filepath.ToSlash(registryDir) + "}\nresources:\n  local:\n    all: true\n    exclude: [skill/review]\n"
		userFragment = "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n"
	case work40SourceIncluded:
		userConfig = "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nresources:\n  local: {all: true}\n"
		userFragment = "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + filepath.ToSlash(registryDir) + "}\n"
	case work40SelectionIncluded:
		userConfig = "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nsources:\n  local: {directory: " + filepath.ToSlash(registryDir) + "}\n"
		userFragment = "apiVersion: agent-marketplace/v4\nresources:\n  local: {include: [skill/review]}\n"
	}
	writeWork40File(t, userConfigPath, userConfig)
	writeWork40File(t, userFragmentPath, userFragment)

	orgConfigPath := filepath.Join(t.TempDir(), "config.yaml")
	writeWork40File(t, orgConfigPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
	config.SetTestOrganizationConfigPath(t, orgConfigPath)
	if err := config.ValidateOrganizationTrustUnit(orgConfigPath, true, true); err != nil {
		t.Fatal("validate private Work40 organization fixture")
	}
	userLockPaths, err := config.ResolveConfigLockPathsTolerateMissing(userConfigPath)
	if err != nil {
		t.Fatal("resolve private Work40 user lock path")
	}
	orgLockPaths, err := config.ResolveConfigLockPathsTolerateMissing(orgConfigPath)
	if err != nil {
		t.Fatalf("resolve private Work40 organization lock path: %v", err)
	}
	fixture := &work40DirectoryFixture{
		home: home, repo: repo, mode: mode,
		userConfigPath: userConfigPath, userConfigBytes: []byte(userConfig), userFragmentPath: userFragmentPath, userLockPath: userLockPaths.CanonicalLock,
		orgConfigPath: orgConfigPath, orgLockPath: orgLockPaths.CanonicalLock,
		registryDir: registryDir, reviewSourcePath: reviewSourcePath, nextSourcePath: nextSourcePath,
		reviewMaterialized:    filepath.Join(home, ".config", "devin", "skills", "review", "SKILL.md"),
		nextMaterialized:      filepath.Join(home, ".config", "devin", "skills", "next", "SKILL.md"),
		unrelatedMaterialized: filepath.Join(home, ".config", "devin", "skills", "work40-protected", "SKILL.md"),
	}
	writeWork40File(t, fixture.unrelatedMaterialized, "work40 protected materialized bytes\n")
	fixture.protected = captureWork40ProtectedFiles(t, fixture)
	if bootstrap != nil {
		bootstrap["orgConfig"] = orgConfigPath
		bootstrap["work40Mode"] = string(mode)
		bootstrap["work40UserConfig"] = userConfigPath
		bootstrap["work40UserFragment"] = userFragmentPath
		bootstrap["work40UserLock"] = fixture.userLockPath
		bootstrap["work40OrgConfig"] = orgConfigPath
		bootstrap["work40OrgLock"] = fixture.orgLockPath
		bootstrap["work40Directory"] = registryDir
		bootstrap["work40ReviewSource"] = reviewSourcePath
		bootstrap["work40NextSource"] = nextSourcePath
		bootstrap["work40ReviewMaterialized"] = fixture.reviewMaterialized
		bootstrap["work40NextMaterialized"] = fixture.nextMaterialized
		bootstrap["work40UnrelatedMaterialized"] = fixture.unrelatedMaterialized
	}
	return fixture
}

func work40ProtectedPaths(fixture *work40DirectoryFixture) []string {
	return []string{
		fixture.userConfigPath, fixture.userFragmentPath, fixture.userLockPath,
		fixture.orgConfigPath, fixture.orgLockPath,
		fixture.reviewSourcePath, fixture.nextSourcePath,
		fixture.reviewMaterialized, fixture.nextMaterialized, fixture.unrelatedMaterialized,
		filepath.Join(fixture.home, ".agents", "config.local.yaml"),
		filepath.Join(fixture.repo, ".agents", "config.yaml"),
		filepath.Join(fixture.repo, ".agents", "config.local.yaml"),
	}
}

func captureWork40ProtectedFiles(t *testing.T, fixture *work40DirectoryFixture) []work40ProtectedFile {
	t.Helper()
	paths := work40ProtectedPaths(fixture)
	files := make([]work40ProtectedFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			files = append(files, work40ProtectedFile{path: path})
			continue
		}
		if err != nil {
			t.Fatal("capture protected Work40 file")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("read protected Work40 file")
		}
		files = append(files, work40ProtectedFile{path: path, exists: true, mode: info.Mode().Perm(), data: data})
	}
	return files
}

func assertWork40ProtectedFilesUnchanged(t *testing.T, before []work40ProtectedFile, exceptions ...string) {
	t.Helper()
	ignored := make(map[string]struct{}, len(exceptions))
	for _, path := range exceptions {
		ignored[path] = struct{}{}
	}
	for _, previous := range before {
		if _, skip := ignored[previous.path]; skip {
			continue
		}
		info, err := os.Lstat(previous.path)
		if os.IsNotExist(err) {
			if previous.exists {
				t.Fatalf("protected Work40 file %s disappeared", filepath.Base(previous.path))
			}
			continue
		}
		if err != nil {
			t.Fatalf("stat protected Work40 file %s", filepath.Base(previous.path))
		}
		if !previous.exists || info.Mode().Perm() != previous.mode {
			t.Fatalf("protected Work40 file %s existence/mode changed", filepath.Base(previous.path))
		}
		after, err := os.ReadFile(previous.path)
		if err != nil || !bytes.Equal(previous.data, after) {
			t.Fatalf("protected Work40 file %s bytes changed", filepath.Base(previous.path))
		}
	}
}

func assertWork40UserSelection(t *testing.T, fixture *work40DirectoryFixture, enabled bool) []byte {
	t.Helper()
	data, err := os.ReadFile(fixture.userConfigPath)
	if err != nil {
		t.Fatal("read Work40 user root")
	}
	if !strings.Contains(string(data), "include: fragment.yaml") {
		t.Fatal("Work40 user root lost its include")
	}
	parsed, err := config.ParseV4Config(data, fixture.userConfigPath, true)
	if err != nil {
		t.Fatal("parse Work40 user root")
	}
	source, ok := parsed.Sources["local"]
	if !ok || source.Kind != config.V4SourceDirectory || source.URL != fixture.registryDir {
		t.Fatal("Work40 directory source identity changed")
	}
	group, ok := parsed.Groups["local"]
	if !ok || group.All == nil || !*group.All {
		t.Fatal("Work40 all-resource selector changed")
	}
	included, excluded := false, false
	for _, request := range group.Include {
		included = included || request.Locator == "skill/review"
	}
	for _, locator := range group.Exclude {
		excluded = excluded || locator == "skill/review"
	}
	if enabled {
		if !included || excluded {
			t.Fatal("Work40 install did not include review and clear its exclusion")
		}
	} else if included || !excluded {
		t.Fatal("Work40 removal did not exclude review")
	}
	return data
}

type work40APITestState struct {
	api        *API
	events     <-chan sessionEvent
	fixture    *work40DirectoryFixture
	rowID      string
	row        session.Row
	generation session.RequestGeneration
}

func newWork40APITestState(t *testing.T, mode work40DirectoryMode) *work40APITestState {
	t.Helper()
	home, repo := newUserAPIFixture(t, false)
	fixture := setupWork40DirectoryFixture(t, home, repo, nil, mode)
	api, err := NewUser(context.Background(), home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal("create private Work40 Local API")
	}
	t.Cleanup(api.Close)
	events := streamEvents(t, api)
	_, row, generation := waitForWork39ResourceRow(t, api, events, func(row session.Row) bool {
		return row.Artifact.Name == "review" && row.RegistryURL == fixture.registryDir && !row.Artifact.IsMarketplace && !row.PendingVerification
	})
	api.mu.Lock()
	rowID := api.resourceRowPresentation(row).ID
	api.mu.Unlock()
	if rowID == "" || generation == 0 {
		t.Fatal("NewUser did not publish the final Work40 directory row")
	}
	if !row.EnabledGlobal && mode != work40RootOwned {
		t.Fatal("included-owner Work40 fixture did not enable the review row")
	}
	if row.EnabledGlobal && mode == work40RootOwned {
		t.Fatal("root-owned Work40 fixture unexpectedly enabled the excluded review row")
	}
	if action, err := session.UserResourceAction(row); err != nil || action != map[bool]string{true: session.UserMarketplaceActionRemove, false: session.UserMarketplaceActionInstall}[row.EnabledGlobal] {
		t.Fatalf("NewUser Work40 row action = %q, %v", action, err)
	}
	next := work40CurrentRowByName(t, api, "next")
	nextAction, err := session.UserResourceAction(next)
	if err != nil {
		t.Fatalf("NewUser Work40 next row action: %v", err)
	}
	if mode == work40SelectionIncluded {
		if next.EnabledGlobal || nextAction != session.UserMarketplaceActionInstall {
			t.Fatal("selector-included Work40 fixture did not leave next available")
		}
	} else if !next.EnabledGlobal || nextAction != session.UserMarketplaceActionRemove {
		t.Fatal("Work40 all-selected fixture did not publish next as enabled")
	}
	assertWork40ProtectedFilesUnchanged(t, fixture.protected)
	return &work40APITestState{api: api, events: events, fixture: fixture, rowID: rowID, row: row, generation: generation}
}

func work40CurrentRowByName(t *testing.T, api *API, name string) session.Row {
	t.Helper()
	api.mu.Lock()
	defer api.mu.Unlock()
	rows := session.MergeDiscoveryRows(api.authoritativeRows, api.provisionalRows, api.discovery.rows)
	for _, row := range rows {
		if row.Artifact.Name == name && !row.PendingVerification {
			return row
		}
	}
	t.Fatalf("authoritative Work40 row was absent: %s", name)
	return session.Row{}
}

func postWork40JSON(t *testing.T, api *API, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal("encode Work40 request")
	}
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, authedRequest(t, api, path, string(data)))
	return recorder
}

func decodeWork40Plan(t *testing.T, body []byte) resourceActionPlanResponse {
	t.Helper()
	var plan resourceActionPlanResponse
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatal("decode Work40 plan")
	}
	return plan
}

func commitWork40Action(t *testing.T, state *work40APITestState, plan resourceActionPlanResponse) operationRef {
	t.Helper()
	recorder := postWork40JSON(t, state.api, "/api/resource-actions", map[string]any{
		"rowId": state.rowID, "planId": plan.PlanID, "acceptRisk": false,
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("Work40 action status = %d", recorder.Code)
	}
	var response struct {
		Operation operationRef `json:"operation"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Operation.ID == 0 || response.Operation.Kind != string(session.OperationMutation) {
		t.Fatal("Work40 action did not return an accepted mutation")
	}
	return response.Operation
}

func waitForWork40Mutation(t *testing.T, state *work40APITestState, operation operationRef, severity string) (snapshotDTO, notificationPayload) {
	t.Helper()
	var final *snapshotDTO
	for {
		event := nextEvent(t, state.events)
		if event.Type == "snapshot" {
			data, err := json.Marshal(event.Payload)
			if err != nil {
				t.Fatal("encode Work40 final snapshot")
			}
			var snapshot snapshotDTO
			if err := json.Unmarshal(data, &snapshot); err != nil {
				t.Fatal("decode Work40 final snapshot")
			}
			if snapshot.Stage == "final" && snapshot.SettledOperation != nil && snapshot.SettledOperation.ID == operation.ID {
				final = &snapshot
			}
		}
		if event.Type != "notification" || event.Operation == nil || event.Operation.ID != operation.ID {
			continue
		}
		data, err := json.Marshal(event.Payload)
		if err != nil {
			t.Fatal("encode Work40 mutation notification")
		}
		var notification notificationPayload
		if err := json.Unmarshal(data, &notification); err != nil {
			t.Fatal("decode Work40 mutation notification")
		}
		if final == nil {
			t.Fatal("Work40 mutation notification preceded its final snapshot")
		}
		if notification.Severity != severity {
			t.Fatalf("Work40 mutation severity = %q, want %q", notification.Severity, severity)
		}
		return *final, notification
	}
}

func work40CurrentDTO(t *testing.T, api *API, rowID string) rowDTO {
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
	t.Fatal("Work40 row was absent from the authoritative API snapshot")
	return rowDTO{}
}

func work40HasOwnerDiagnostic(snapshot snapshotDTO, fixture *work40DirectoryFixture, mode work40DirectoryMode) bool {
	message := `source "local" is declared in an included fragment`
	if mode == work40SelectionIncluded {
		message = `resource "skill/review" is requested by an included fragment`
	}
	expected := &config.V4ValidationError{Code: "config.edit.include-owned", Path: fixture.userConfigPath, Msg: message}
	expectedCode := pipeline.IssueDedupKey(expected)
	expectedMessage := session.SanitizeReturnedError(expected).Error()
	for _, diagnostic := range snapshot.Diagnostics {
		if diagnostic.Category == string(session.DiagnosticMutation) && diagnostic.Severity == "error" && diagnostic.Code == expectedCode && diagnostic.Message == expectedMessage {
			return true
		}
	}
	return false
}

func work40UserRootMode(t *testing.T, fixture *work40DirectoryFixture, before []work40ProtectedFile) {
	t.Helper()
	for _, file := range before {
		if file.path != fixture.userConfigPath {
			continue
		}
		info, err := os.Stat(fixture.userConfigPath)
		if err != nil || info.Mode().Perm() != file.mode {
			t.Fatal("Work40 user root mode changed")
		}
		return
	}
	t.Fatal("Work40 baseline did not include the user root")
}

// spec: session-interactions/guarded-submission
func TestLocalDirectoryResourceActionEditsOnlyTheUserSelector(t *testing.T) {
	state := newWork40APITestState(t, work40RootOwned)
	state.api.mu.Lock()
	generation, sequence := state.api.currentG, state.api.sequence
	state.api.mu.Unlock()
	planRecorder := postWork40JSON(t, state.api, "/api/resource-plan", map[string]string{"rowId": state.rowID})
	if planRecorder.Code != http.StatusOK {
		t.Fatalf("static Work40 plan status = %d", planRecorder.Code)
	}
	plan := decodeWork40Plan(t, planRecorder.Body.Bytes())
	if plan.RowID != state.rowID || plan.Generation != uint64(state.generation) || plan.Action != session.UserMarketplaceActionInstall || plan.RequiresRiskAcceptance || plan.Preview != "Resource: skill/review\nLayer: user" {
		t.Fatalf("static Work40 plan did not preserve the current user row contract")
	}
	state.api.mu.Lock()
	planStored := state.api.resourceActionPlan != nil && state.api.resourceActionPlan.prepared == nil
	generationAfterPlan, sequenceAfterPlan := state.api.currentG, state.api.sequence
	state.api.mu.Unlock()
	if !planStored || generationAfterPlan != generation || sequenceAfterPlan != sequence {
		t.Fatal("static Work40 preparation invoked SDK work or advanced session state")
	}
	assertWork40ProtectedFilesUnchanged(t, state.fixture.protected)

	installOperation := commitWork40Action(t, state, plan)
	installedSnapshot, _ := waitForWork40Mutation(t, state, installOperation, "info")
	if installedSnapshot.Stage != "final" {
		t.Fatal("Work40 install did not settle with an authoritative final snapshot")
	}
	installedDTO := work40CurrentDTO(t, state.api, state.rowID)
	if !installedDTO.Checked || installedDTO.PendingVerification || installedDTO.ResourceAction != session.UserMarketplaceActionRemove {
		t.Fatal("Work40 install did not enable the current user row with Remove capability")
	}
	next := work40CurrentRowByName(t, state.api, "next")
	if !next.EnabledGlobal {
		t.Fatal("Work40 install changed the independent next selector")
	}
	installedBytes := assertWork40UserSelection(t, state.fixture, true)
	if bytes.Equal(installedBytes, state.fixture.userConfigBytes) {
		t.Fatal("Work40 install did not change the user selection file")
	}
	work40UserRootMode(t, state.fixture, state.fixture.protected)
	assertWork40ProtectedFilesUnchanged(t, state.fixture.protected, state.fixture.userConfigPath)

	removeRecorder := postWork40JSON(t, state.api, "/api/resource-plan", map[string]string{"rowId": state.rowID})
	if removeRecorder.Code != http.StatusOK {
		t.Fatalf("Work40 remove plan status = %d", removeRecorder.Code)
	}
	removePlan := decodeWork40Plan(t, removeRecorder.Body.Bytes())
	if removePlan.Action != session.UserMarketplaceActionRemove || removePlan.RequiresRiskAcceptance {
		t.Fatal("Work40 remove plan did not remain static and risk-free")
	}
	removeOperation := commitWork40Action(t, state, removePlan)
	removedSnapshot, _ := waitForWork40Mutation(t, state, removeOperation, "info")
	if removedSnapshot.Stage != "final" {
		t.Fatal("Work40 removal did not settle with an authoritative final snapshot")
	}
	removedDTO := work40CurrentDTO(t, state.api, state.rowID)
	if removedDTO.Checked || removedDTO.PendingVerification || removedDTO.ResourceAction != session.UserMarketplaceActionInstall {
		t.Fatal("Work40 removal did not leave the current row disabled with Install capability")
	}
	next = work40CurrentRowByName(t, state.api, "next")
	if !next.EnabledGlobal {
		t.Fatal("Work40 removal changed the independent next selector")
	}
	removedBytes := assertWork40UserSelection(t, state.fixture, false)
	if bytes.Equal(removedBytes, installedBytes) {
		t.Fatal("Work40 removal did not update the user selector")
	}
	work40UserRootMode(t, state.fixture, state.fixture.protected)
	assertWork40ProtectedFilesUnchanged(t, state.fixture.protected, state.fixture.userConfigPath)
}

// spec: session-interactions/guarded-submission
func TestLocalDirectoryResourceActionRejectsIncludedOwners(t *testing.T) {
	for _, mode := range []work40DirectoryMode{work40SourceIncluded, work40SelectionIncluded} {
		t.Run(string(mode), func(t *testing.T) {
			state := newWork40APITestState(t, mode)
			planRecorder := postWork40JSON(t, state.api, "/api/resource-plan", map[string]string{"rowId": state.rowID})
			if planRecorder.Code != http.StatusOK {
				t.Fatalf("static included-owner plan status = %d", planRecorder.Code)
			}
			plan := decodeWork40Plan(t, planRecorder.Body.Bytes())
			if plan.Action != session.UserMarketplaceActionRemove || plan.RequiresRiskAcceptance {
				t.Fatal("included-owner row did not receive a static Remove plan")
			}
			assertWork40ProtectedFilesUnchanged(t, state.fixture.protected)
			operation := commitWork40Action(t, state, plan)
			final, _ := waitForWork40Mutation(t, state, operation, "error")
			if !work40HasOwnerDiagnostic(final, state.fixture, mode) {
				t.Fatal("included-owner refusal did not expose config.edit.include-owned")
			}
			row := work40CurrentDTO(t, state.api, state.rowID)
			if !row.Checked || row.ResourceAction != session.UserMarketplaceActionRemove {
				t.Fatal("included-owner refusal changed effective selection or capability")
			}
			assertWork40ProtectedFilesUnchanged(t, state.fixture.protected)
		})
	}
}
