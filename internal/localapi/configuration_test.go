package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func postRaw(t *testing.T, api *API, path, body string) (int, map[string]any) {
	t.Helper()
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var decoded map[string]any
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("response body is not JSON: %q", data)
		}
	}
	return resp.StatusCode, decoded
}

func configurationField(t *testing.T, decoded map[string]any, key string) []string {
	t.Helper()
	raw, ok := decoded[key].([]any)
	if !ok {
		t.Fatalf("configuration.%s missing or not an array in %v", key, decoded)
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("configuration.%s entry %v is not a string", key, item)
		}
		values = append(values, s)
	}
	return values
}

// spec: gui-configuration/save-user-harness-targets-without-losing-unknown-identifiers
func TestUserConfigurationProjection(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	source := t.TempDir()
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin, future-harness]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true, harnesses: [devin]}\n")
	writeFile(t, filepath.Join(home, ".agents", "fragment.yaml"), "harnesses: [devin]\n")
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	config.SetTestOrganizationConfigPath(t, orgPath)

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("configuration status = %d, body = %v", status, decoded)
	}
	if got := configurationField(t, decoded, "userTargets"); strings.Join(got, ",") != "devin,future-harness" {
		t.Fatalf("userTargets = %v", got)
	}
	if got := configurationField(t, decoded, "organizationTargets"); strings.Join(got, ",") != "claude" {
		t.Fatalf("organizationTargets = %v", got)
	}
	supported := configurationField(t, decoded, "supportedTargets")
	if len(supported) == 0 {
		t.Fatal("supportedTargets is empty")
	}
	foundDevin, foundFuture := false, false
	for _, id := range supported {
		foundDevin = foundDevin || id == "devin"
		foundFuture = foundFuture || id == "future-harness"
	}
	if !foundDevin || foundFuture {
		t.Fatalf("supportedTargets = %v, want canonical IDs without unknown entries", supported)
	}
	if decoded["rootPresent"] != true || decoded["editable"] != true {
		t.Fatalf("rootPresent/editable = %v/%v", decoded["rootPresent"], decoded["editable"])
	}
	managed := configurationField(t, decoded, "managedOutsideGUI")
	wantLabels := map[string]bool{"included files": false, "per-source configuration": false}
	for _, label := range managed {
		if _, known := wantLabels[label]; known {
			wantLabels[label] = true
		}
		if strings.Contains(label, "narrowing") {
			t.Fatalf("managedOutsideGUI mislabels ingest/narrowing detail: %v", managed)
		}
	}
	for label, found := range wantLabels {
		if !found {
			t.Fatalf("managedOutsideGUI missing %q: %v", label, managed)
		}
	}
	body, _ := json.Marshal(decoded)
	for _, forbidden := range []string{home, api.Credential(), "apiVersion"} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("configuration response leaks %q: %s", forbidden, body)
		}
	}
}

type configurationSourceView struct {
	Kind          string `json:"kind"`
	Location      string `json:"location"`
	Ref           string `json:"ref"`
	Path          string `json:"path"`
	SourceHarness string `json:"sourceHarness"`
}

func configurationSources(t *testing.T, decoded map[string]any, key string) []configurationSourceView {
	t.Helper()
	data, err := json.Marshal(decoded[key])
	if err != nil {
		t.Fatal(err)
	}
	var views []configurationSourceView
	if err := json.Unmarshal(data, &views); err != nil {
		t.Fatalf("configuration.%s is not a source array: %v", key, err)
	}
	return views
}

// spec: gui-configuration/organization-sources-are-read-only
func TestUserConfigurationSourceProjection(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	userDir := t.TempDir()
	fragDir := t.TempDir()
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	userBytes := []byte("apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nsources:\n  ddir: {directory: " + userDir + ", source_harness: windsurf}\n  zgit: {git: https://example.com/repo.git, ref: main, path: sub/dir, source_harness: claude}\n  mmp: {marketplace: https://market.example}\nresources:\n  ddir: {all: true}\n  zgit: {all: true}\n  mmp: {include: [main/collection/onboarding]}\n")
	fragBytes := []byte("sources:\n  ffrag: {directory: " + fragDir + "}\nresources:\n  ffrag: {all: true}\n")
	writeFile(t, userConfig, string(userBytes))
	writeFile(t, filepath.Join(home, ".agents", "fragment.yaml"), string(fragBytes))
	orgDir := t.TempDir()
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	orgBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  orggit: {git: \"https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.com/org.git?token=hint#frag\", ref: main, path: nested, source_harness: devin}\n  orgdir: {directory: " + orgDir + "}\n  orgmp: {marketplace: https://org-mp.example}\n")
	writeFile(t, orgPath, string(orgBytes))
	config.SetTestOrganizationConfigPath(t, orgPath)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("configuration status = %d, body = %v", status, decoded)
	}
	wantUser := []configurationSourceView{
		{Kind: "directory", Location: userDir, SourceHarness: "windsurf"},
		{Kind: "directory", Location: fragDir},
		{Kind: "marketplace", Location: "https://market.example"},
		{Kind: "git", Location: "https://example.com/repo.git", Ref: "main", Path: "sub/dir", SourceHarness: "claude"},
	}
	if got := configurationSources(t, decoded, "userSources"); !reflect.DeepEqual(got, wantUser) {
		t.Fatalf("userSources = %#v, want %#v", got, wantUser)
	}
	wantOrg := []configurationSourceView{
		{Kind: "directory", Location: orgDir},
		{Kind: "git", Location: "https://***@example.com/org.git?***", Ref: "main", Path: "nested", SourceHarness: "devin"},
		{Kind: "marketplace", Location: "https://org-mp.example"},
	}
	if got := configurationSources(t, decoded, "organizationSources"); !reflect.DeepEqual(got, wantOrg) {
		t.Fatalf("organizationSources = %#v, want %#v", got, wantOrg)
	}
	body, _ := json.Marshal(decoded)
	for _, secret := range []string{"YOUR_USER_HERE", "YOUR_TOKEN_HERE", "token=hint"} {
		if bytes.Contains(body, []byte(secret)) {
			t.Fatalf("configuration response leaks %q: %s", secret, body)
		}
	}

	status, again := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK || !reflect.DeepEqual(again, decoded) {
		t.Fatalf("repeat metadata status = %d equal=%v", status, reflect.DeepEqual(again, decoded))
	}

	if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, userBytes) {
		t.Fatal("user config changed")
	}
	if after, _ := os.ReadFile(filepath.Join(home, ".agents", "fragment.yaml")); !bytes.Equal(after, fragBytes) {
		t.Fatal("included fragment changed")
	}
	if after, _ := os.ReadFile(orgPath); !bytes.Equal(after, orgBytes) {
		t.Fatal("organization config changed")
	}
	if after, _ := os.ReadFile(repoConfig); !bytes.Equal(after, repoBefore) {
		t.Fatal("repo-committed config changed")
	}
	if after, _ := os.ReadFile(repoLocal); !bytes.Equal(after, localBefore) {
		t.Fatal("personal-local config changed")
	}
	entries, err := os.ReadDir(filepath.Join(home, ".agents"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "config.yaml" && entry.Name() != "fragment.yaml" {
			t.Fatalf("unexpected state file %q under .agents", entry.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".devin")); !os.IsNotExist(err) {
		t.Fatal("metadata read materialized native output")
	}
}

func TestConfiguredSourceRowSourceIDMatchesSnapshotRows(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	dirSource := t.TempDir()
	writeFile(t, filepath.Join(dirSource, "dir-skill", "SKILL.md"), "---\nname: dir-skill\ndescription: Dir\n---\n# Dir\n")
	base := t.TempDir()
	work := filepath.Join(base, "work")
	bare := filepath.Join(base, "repo.git")
	sourceGit(t, "", "init", "--initial-branch=main", work)
	writeFile(t, filepath.Join(work, "git-skill", "SKILL.md"), "---\nname: git-skill\ndescription: Git\n---\n# Git\n")
	sourceGit(t, work, "add", ".")
	sourceGit(t, work, "commit", "-m", "seed")
	sourceGit(t, "", "clone", "--bare", work, bare)
	gitLocation := "file://" + filepath.ToSlash(bare)
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local: {directory: "+dirSource+"}\n  remote: {git: "+gitLocation+", ref: main}\nresources:\n  local: {all: true}\n  remote: {all: true}\n")
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("configuration status = %d, body = %v", status, decoded)
	}
	data, err := json.Marshal(decoded["userSources"])
	if err != nil {
		t.Fatal(err)
	}
	var configured []struct {
		Kind        string `json:"kind"`
		RowSourceID string `json:"rowSourceId"`
	}
	if err := json.Unmarshal(data, &configured); err != nil {
		t.Fatal(err)
	}
	rowSourceByName := map[string]string{}
	deadline := time.Now().Add(60 * time.Second)
	synced := false
	for {
		snapshot := postSnapshot(t, api)
		clear(rowSourceByName)
		for _, row := range snapshot.Rows {
			rowSourceByName[row.Name] = row.Source.ID
		}
		_, hasDir := rowSourceByName["dir-skill"]
		_, hasGit := rowSourceByName["git-skill"]
		if snapshot.Stage == "final" && snapshot.Operation == nil {
			if hasDir && hasGit {
				break
			}
			if !synced {
				status, body := postRaw(t, api, "/api/actions", `{"action":"sync"}`)
				switch status {
				case http.StatusAccepted:
					synced = true
				case http.StatusBadRequest:
				default:
					t.Fatalf("sync status = %d, body = %v", status, body)
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("rows after sync = %v", rowSourceByName)
		}
		time.Sleep(50 * time.Millisecond)
	}
	wantByKind := map[string]string{"directory": rowSourceByName["dir-skill"], "git": rowSourceByName["git-skill"]}
	if len(configured) != len(wantByKind) {
		t.Fatalf("userSources = %+v, want one per kind", configured)
	}
	for _, source := range configured {
		want, ok := wantByKind[source.Kind]
		if !ok {
			t.Fatalf("unexpected source kind %q", source.Kind)
		}
		if source.RowSourceID != want {
			t.Fatalf("%s rowSourceId = %q, rows from that source carry %q", source.Kind, source.RowSourceID, want)
		}
	}
	if wantByKind["directory"] == "" || wantByKind["git"] == "" {
		t.Fatalf("snapshot rows by name = %v, want dir-skill and git-skill", rowSourceByName)
	}
}

func configurationFixtureStatus(t *testing.T, api *API, path, body string) int {
	t.Helper()
	status, _ := postRaw(t, api, path, body)
	return status
}

// spec: gui-shell/launch-directory-does-not-change-the-gui-workspace
func TestConfigurationRequiresUserMode(t *testing.T) {
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	if status := configurationFixtureStatus(t, api, "/api/configuration", ""); status != http.StatusBadRequest {
		t.Fatalf("workspace-mode configuration status = %d, want 400", status)
	}
	if status := configurationFixtureStatus(t, api, "/api/configuration/actions",
		`{"action":"save-targets","targets":["devin"]}`); status != http.StatusBadRequest {
		t.Fatalf("workspace-mode configuration action status = %d, want 400", status)
	}
}

func TestConfigurationEndpointRequestControls(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)

	send := func(method, path, token, origin, body string) int {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, server.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("X-Agent-Marketplace-Token", token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	token := api.Credential()
	for _, path := range []string{"/api/configuration", "/api/configuration/actions"} {
		if status := send(http.MethodGet, path, token, "", ""); status != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status = %d, want 405", path, status)
		}
		if status := send(http.MethodPost, path, "", "", ""); status != http.StatusUnauthorized {
			t.Fatalf("no-token %s status = %d, want 401", path, status)
		}
		if status := send(http.MethodPost, path, "wrong", "", ""); status != http.StatusUnauthorized {
			t.Fatalf("wrong-token %s status = %d, want 401", path, status)
		}
		if status := send(http.MethodPost, path, token, "https://evil.example", ""); status != http.StatusForbidden {
			t.Fatalf("bad-origin %s status = %d, want 403", path, status)
		}
		if status := send(http.MethodPost, path+"?scope=organization", token, "", ""); status != http.StatusBadRequest {
			t.Fatalf("query %s status = %d, want 400", path, status)
		}
	}
	if status := send(http.MethodPost, "/api/configuration", token, "", "{}"); status != http.StatusBadRequest {
		t.Fatalf("non-empty metadata body status = %d, want 400", status)
	}
	if status := send(http.MethodPost, "/api/configuration", token, guiOrigin, ""); status != http.StatusOK {
		t.Fatalf("wails origin metadata status = %d, want 200", status)
	}
}

func TestConfigurationProjectionRootStates(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("absent root status = %d", status)
	}
	if decoded["rootPresent"] != false || decoded["editable"] != false {
		t.Fatalf("absent root: rootPresent/editable = %v/%v", decoded["rootPresent"], decoded["editable"])
	}

	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: [broken\n")
	status, decoded = postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("invalid root status = %d", status)
	}
	if decoded["rootPresent"] != true || decoded["editable"] != false {
		t.Fatalf("invalid root: rootPresent/editable = %v/%v", decoded["rootPresent"], decoded["editable"])
	}
}

// spec: gui-configuration/save-user-harness-targets-without-losing-unknown-identifiers
func TestConfigurationRejectsNonRegularRoots(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, path string)
	}{
		{name: "dangling-symlink", plant: func(t *testing.T, path string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(t.TempDir(), "missing.yaml"), path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory", plant: func(t *testing.T, path string) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := newUserAPIFixture(t, false)
			userConfig := filepath.Join(home, ".agents", "config.yaml")
			tc.plant(t, userConfig)

			api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(api.Close)

			status, decoded := postRaw(t, api, "/api/configuration", "")
			if status != http.StatusOK {
				t.Fatalf("%s root metadata status = %d", tc.name, status)
			}
			if decoded["rootPresent"] != true || decoded["editable"] != false {
				t.Fatalf("%s root: rootPresent/editable = %v/%v", tc.name, decoded["rootPresent"], decoded["editable"])
			}

			events := streamEvents(t, api)
			for {
				event := nextEvent(t, events)
				if event.Type != "snapshot" {
					continue
				}
				if stage := event.snapshotStage(); stage == "config" || stage == "final" {
					break
				}
			}
			status, _ = saveTargets(t, api, `["devin"]`)
			if status != http.StatusBadRequest {
				t.Fatalf("%s root save-targets status = %d, want 400", tc.name, status)
			}
			info, err := os.Lstat(userConfig)
			if err != nil {
				t.Fatalf("%s root vanished: %v", tc.name, err)
			}
			switch tc.name {
			case "dangling-symlink":
				if info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("dangling symlink root was replaced by a regular file")
				}
			case "directory":
				if !info.IsDir() {
					t.Fatal("directory root was replaced by a regular file")
				}
			}
		})
	}
}

func saveTargets(t *testing.T, api *API, targets string) (int, map[string]any) {
	t.Helper()
	return postRaw(t, api, "/api/configuration/actions", `{"action":"save-targets","targets":`+targets+`}`)
}

func awaitNotification(t *testing.T, events <-chan sessionEvent) sessionEvent {
	t.Helper()
	for {
		event := nextEvent(t, events)
		if event.Type == "notification" {
			return event
		}
	}
}

func notificationSeverity(t *testing.T, event sessionEvent) string {
	t.Helper()
	data, err := json.Marshal(event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var payload notificationPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Severity
}

// spec: gui-configuration/organization-harness-targets-are-read-only
func TestConfigurationSaveTargetsMatchesSharedWriter(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
	fragmentBytes := []byte("sources:\n  local: {directory: " + source + "}\nresources:\n  local: {all: true, harnesses: [devin]}\n")
	fragmentPath := filepath.Join(home, ".agents", "fragment.yaml")
	writeFile(t, fragmentPath, string(fragmentBytes))
	root := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin, future-harness]\n"
	writeFile(t, userConfig, root)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	orgBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	writeFile(t, orgPath, string(orgBytes))
	config.SetTestOrganizationConfigPath(t, orgPath)

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	status, decoded := saveTargets(t, api, `["claude","future-harness"]`)
	if status != http.StatusAccepted {
		t.Fatalf("save-targets status = %d, body = %v", status, decoded)
	}
	operation, _ := decoded["operation"].(map[string]any)
	if operation["kind"] != "targets" {
		t.Fatalf("operation kind = %v, want targets", operation["kind"])
	}
	var announced bool
	for {
		event := nextEvent(t, events)
		if event.Type == "snapshot" && event.Operation != nil && event.Operation.Kind == "targets" {
			announced = true
		}
		if event.Type == "notification" {
			if got := notificationSeverity(t, event); got != "info" {
				t.Fatalf("notification severity = %q", got)
			}
			break
		}
	}
	if !announced {
		t.Fatal("no snapshot event announced the targets operation")
	}

	guiBytes, err := os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(guiBytes, []byte("future-harness")) {
		t.Fatalf("unknown identifier lost: %s", guiBytes)
	}

	oracleHome := t.TempDir()
	oraclePath := filepath.Join(oracleHome, ".agents", "config.yaml")
	writeFile(t, oraclePath, root)
	writeFile(t, filepath.Join(oracleHome, ".agents", "fragment.yaml"), string(fragmentBytes))
	if err := config.SetHarnessesInConfig(oraclePath, []string{"claude", "future-harness"}); err != nil {
		t.Fatalf("shared writer oracle failed: %v", err)
	}
	oracleBytes, err := os.ReadFile(oraclePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(guiBytes, oracleBytes) {
		t.Fatalf("GUI save bytes differ from shared writer:\nGUI:\n%s\nwriter:\n%s", guiBytes, oracleBytes)
	}

	if after, _ := os.ReadFile(repoConfig); !bytes.Equal(after, repoBefore) {
		t.Fatal("repo-committed config changed")
	}
	if after, _ := os.ReadFile(repoLocal); !bytes.Equal(after, localBefore) {
		t.Fatal("personal-local config changed")
	}
	if after, err := os.ReadFile(orgPath); err != nil || !bytes.Equal(after, orgBytes) {
		t.Fatal("organization config changed")
	}
	if after, err := os.ReadFile(fragmentPath); err != nil || !bytes.Equal(after, fragmentBytes) {
		t.Fatal("included fragment changed")
	}
	if _, err := os.Stat(filepath.Join(home, ".future-harness")); !os.IsNotExist(err) {
		t.Fatal("unsupported target materialized output")
	}
}

// spec: session-interactions/empty-selection
func TestConfigurationActionRejectsInvalidRequests(t *testing.T) {
	var syncCalls, updateCalls atomic.Int32
	home, _ := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin, future-harness]\n")
	before, _ := os.ReadFile(userConfig)
	api, err := NewUser(context.Background(), home, countingLifecycle(&syncCalls, &updateCalls), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	bodies := []string{
		`{"action":"save-targets","targets":[]}`,
		`{"action":"save-targets","targets":["brand-new-unknown"]}`,
		`{"action":"save-targets","targets":["devin",""]}`,
		`{"action":"save-targets","targets":["devin"," "]}`,
		`{"action":"save-targets","targets":"devin"}`,
		`{"action":"save-targets","targets":["devin"],"extra":1}`,
		`{"action":"save-targets","targets":["devin"],"scope":"organization"}`,
		`{"action":"save-targets","targets":["devin"],"path":"/tmp/evil.yaml"}`,
		`{"action":"Save-Targets","targets":["devin"]}`,
		`{"action":"save-targets","targets":["devin"],"action":"save-targets"}`,
		`{"action":"save-targets","targets":["devin"]} trailing`,
		`{"action":"save-targets"}`,
		`{"targets":["devin"]}`,
		`{}`,
		``,
	}
	for _, body := range bodies {
		status, _ := postRaw(t, api, "/api/configuration/actions", body)
		if status != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want 400", body, status)
		}
	}
	if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, before) {
		t.Fatalf("rejected bodies changed the root: %s", after)
	}
	if got := syncCalls.Load() + updateCalls.Load(); got != 0 {
		t.Fatalf("lifecycle invoked %d times", got)
	}
}

// spec: session-interactions/save-selected-harnesses
func TestConfigurationSaveTrimsTargetsAndUsesSessionHome(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin, future-harness]\n")

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	decoy := t.TempDir()
	decoyConfig := filepath.Join(decoy, ".agents", "config.yaml")
	writeFile(t, decoyConfig, "apiVersion: agent-marketplace/v4\nharnesses: [windsurf]\n")
	t.Setenv("HOME", decoy)

	status, decoded := saveTargets(t, api, `[ " claude ", "future-harness"]`)
	if status != http.StatusAccepted {
		t.Fatalf("save-targets with padded targets status = %d, body = %v", status, decoded)
	}
	event := awaitNotification(t, events)
	if got := notificationSeverity(t, event); got != "info" {
		t.Fatalf("notification severity = %q, want info (writer succeeded)", got)
	}

	data, err := os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("claude")) || !bytes.Contains(data, []byte("future-harness")) {
		t.Fatalf("session-home root not written: %s", data)
	}
	if after, _ := os.ReadFile(decoyConfig); string(after) != "apiVersion: agent-marketplace/v4\nharnesses: [windsurf]\n" {
		t.Fatalf("save followed a recomputed HOME: %s", after)
	}
}

// spec: session-interactions/guarded-submission
func TestConfigurationActionRejectsBusyAndInvalidRoot(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	userConfig := filepath.Join(home, ".agents", "config.yaml")

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	held, ok := api.gate.Begin(session.OperationSync)
	if !ok {
		t.Fatal("could not hold the gate")
	}
	status, _ := saveTargets(t, api, `["devin"]`)
	api.gate.Finish(held)
	if status != http.StatusConflict {
		t.Fatalf("busy save-targets status = %d, want 409", status)
	}

	writeFile(t, userConfig, "apiVersion: [broken\n")
	status, _ = saveTargets(t, api, `["devin"]`)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid root save-targets status = %d, want 400", status)
	}
	if data, _ := os.ReadFile(userConfig); string(data) != "apiVersion: [broken\n" {
		t.Fatal("invalid root was overwritten")
	}
}

// spec: config-layering/unsupported-only-target-set-grants-no-materialization-authority
func TestConfigurationSaveUnknownOnlyRoot(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [future-harness]\n")
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK || decoded["editable"] != true {
		t.Fatalf("unknown-only root not editable: %d %v", status, decoded)
	}
	status, decoded = saveTargets(t, api, `["future-harness"]`)
	if status != http.StatusAccepted {
		t.Fatalf("unknown-only save status = %d, body = %v", status, decoded)
	}
	event := awaitNotification(t, events)
	if got := notificationSeverity(t, event); got != "info" {
		t.Fatalf("notification severity = %q", got)
	}
	if data, _ := os.ReadFile(userConfig); !bytes.Contains(data, []byte("future-harness")) {
		t.Fatalf("unknown identifier lost: %s", data)
	}
	if _, err := os.Stat(filepath.Join(home, ".future-harness")); !os.IsNotExist(err) {
		t.Fatal("unsupported target materialized output")
	}
	if status := postAction(t, api, "sync"); status != http.StatusBadRequest {
		t.Fatalf("sync after unknown-only save status = %d, want 400", status)
	}
}
