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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func newFirstRunAPI(t *testing.T, home string, lifecycles ...session.DependencyLifecycle) *API {
	t.Helper()
	lifecycle := session.DependencyLifecycle{}
	if len(lifecycles) > 0 {
		lifecycle = lifecycles[0]
	}
	api, err := NewUser(context.Background(), home, lifecycle, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	return api
}

func initializeRequest(t *testing.T, api *API, body string) (int, map[string]any) {
	t.Helper()
	return postRaw(t, api, "/api/configuration/actions", body)
}

func firstRunHome(t *testing.T) (home, userConfig string) {
	t.Helper()
	home, _ = newUserAPIFixture(t, false)
	if err := os.MkdirAll(filepath.Join(home, ".devin"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(home, ".agents", "config.yaml")
}

func assertNoFirstRunResources(t *testing.T, home string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(home, ".devin", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".factory", "skills"),
		filepath.Join(home, ".codex", "agents"),
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(os.Getenv("CODEX_HOME"), "agents"),
		filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "devin"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("first-run source mutation created native output %s (err=%v)", path, err)
		}
	}
}

// spec: gui-first-run/absent-user-personal-root-starts-first-run
func TestFirstRunProjectionStates(t *testing.T) {
	for _, tc := range []struct {
		name         string
		plant        func(t *testing.T, path string)
		wantFirstRun bool
		wantCanInit  bool
	}{
		{name: "absent", plant: func(t *testing.T, path string) {}, wantFirstRun: true, wantCanInit: true},
		{name: "valid", plant: func(t *testing.T, path string) {
			writeFile(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
		}},
		{name: "malformed", plant: func(t *testing.T, path string) {
			writeFile(t, path, "apiVersion: [broken\n")
		}},
		{name: "directory", plant: func(t *testing.T, path string) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "dangling-symlink", plant: func(t *testing.T, path string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(t.TempDir(), "missing.yaml"), path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := newUserAPIFixture(t, false)
			userConfig := filepath.Join(home, ".agents", "config.yaml")
			tc.plant(t, userConfig)
			api := newFirstRunAPI(t, home)

			status, decoded := postRaw(t, api, "/api/configuration", "")
			if status != http.StatusOK {
				t.Fatalf("metadata status = %d, body = %v", status, decoded)
			}
			if decoded["firstRun"] != tc.wantFirstRun {
				t.Fatalf("firstRun = %v, want %v", decoded["firstRun"], tc.wantFirstRun)
			}
			if decoded["canInitialize"] != tc.wantCanInit {
				t.Fatalf("canInitialize = %v, want %v", decoded["canInitialize"], tc.wantCanInit)
			}
			for _, key := range []string{"detectedTargets", "consentRequired", "telemetryDestination", "retentionPolicyURL", "detectionUnavailable"} {
				if _, present := decoded[key]; !present {
					t.Fatalf("projection missing %q in %v", key, decoded)
				}
			}
			if decoded["rootPresent"] != !tc.wantFirstRun {
				t.Fatalf("rootPresent = %v", decoded["rootPresent"])
			}
			if tc.wantFirstRun {
				return
			}
			beforeInfo, _ := os.Lstat(userConfig)
			before, _ := os.ReadFile(userConfig)
			status, decoded = initializeRequest(t, api, `{"action":"initialize","targets":["devin"],"consent":false}`)
			if status != http.StatusBadRequest || decoded["code"] != codeNotReady {
				t.Fatalf("initialize over existing root = %d/%v, want 400/%s", status, decoded["code"], codeNotReady)
			}
			afterInfo, _ := os.Lstat(userConfig)
			if beforeInfo != nil && afterInfo != nil && beforeInfo.Mode()&os.ModeSymlink == 0 {
				if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, before) {
					t.Fatalf("existing root bytes changed: %q", after)
				}
			}
			if afterInfo != nil && beforeInfo != nil && (afterInfo.Mode()&os.ModeSymlink) != (beforeInfo.Mode()&os.ModeSymlink) {
				t.Fatal("existing root file type changed")
			}
		})
	}
}

// spec: gui-first-run/detected-harness-targets-are-preselected-and-editable
func TestFirstRunDetectedTargets(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	for _, dir := range []string{".factory", ".claude"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	api := newFirstRunAPI(t, home)

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("metadata status = %d", status)
	}
	detected := configurationField(t, decoded, "detectedTargets")
	supported := configurationField(t, decoded, "supportedTargets")
	for _, want := range []string{"claude", "factory"} {
		found := false
		for _, id := range detected {
			found = found || id == want
		}
		if !found {
			t.Fatalf("detectedTargets %v missing %q (supported %v)", detected, want, supported)
		}
	}
	supportedIndex := map[string]int{}
	for i, id := range supported {
		supportedIndex[id] = i
	}
	last := -1
	for _, id := range detected {
		index, known := supportedIndex[id]
		if !known {
			t.Fatalf("detectedTargets contains unsupported %q", id)
		}
		if index <= last {
			t.Fatalf("detectedTargets %v not in supported order %v", detected, supported)
		}
		last = index
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("metadata read wrote the user root")
	}
}

// spec: gui-first-run/organization-telemetry-level-skips-consent
// spec: gui-first-run/unmanaged-first-run-presents-init-equivalent-consent
func TestFirstRunConsentCondition(t *testing.T) {
	for _, tc := range []struct {
		name          string
		org           string
		wantConsent   bool
		wantDest      string
		wantRetention string
	}{
		{name: "absent", wantConsent: true, wantDest: "not configured", wantRetention: "not configured"},
		{name: "level-off", org: "apiVersion: agent-marketplace/v4\ntelemetry: {level: off}\n"},
		{name: "level-basic", org: "apiVersion: agent-marketplace/v4\ntelemetry: {level: basic}\n"},
		{name: "level-detailed", org: "apiVersion: agent-marketplace/v4\ntelemetry: {level: detailed}\n"},
		{name: "endpoint-only", org: "apiVersion: agent-marketplace/v4\ntelemetry: {endpoint: https://org.example.com, retention_policy: https://org.example.com/retention}\n",
			wantConsent: true, wantDest: "org.example.com", wantRetention: "https://org.example.com/retention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := newUserAPIFixture(t, false)
			if tc.org != "" {
				orgPath := filepath.Join(t.TempDir(), "org.yaml")
				writeFile(t, orgPath, tc.org)
				config.SetTestOrganizationConfigPath(t, orgPath)
			}
			api := newFirstRunAPI(t, home)

			status, decoded := postRaw(t, api, "/api/configuration", "")
			if status != http.StatusOK {
				t.Fatalf("metadata status = %d", status)
			}
			if decoded["consentRequired"] != tc.wantConsent {
				t.Fatalf("consentRequired = %v, want %v", decoded["consentRequired"], tc.wantConsent)
			}
			if tc.wantConsent {
				if decoded["telemetryDestination"] != tc.wantDest {
					t.Fatalf("telemetryDestination = %v, want %q", decoded["telemetryDestination"], tc.wantDest)
				}
				if decoded["retentionPolicyURL"] != tc.wantRetention {
					t.Fatalf("retentionPolicyURL = %v, want %q", decoded["retentionPolicyURL"], tc.wantRetention)
				}
			}
			body, _ := json.Marshal(decoded)
			for _, forbidden := range []string{home, api.Credential(), "apiVersion"} {
				if bytes.Contains(body, []byte(forbidden)) {
					t.Fatalf("configuration response leaks %q: %s", forbidden, body)
				}
			}
		})
	}
}

// spec: gui-first-run/organization-telemetry-level-skips-consent
func TestFirstRunOrgInvalidBlocksInitialize(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, "apiVersion: [broken\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	api := newFirstRunAPI(t, home)

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("metadata status = %d", status)
	}
	if decoded["firstRun"] != true || decoded["canInitialize"] != false {
		t.Fatalf("invalid org: firstRun/canInitialize = %v/%v", decoded["firstRun"], decoded["canInitialize"])
	}
	status, decoded = initializeRequest(t, api, `{"action":"initialize","targets":["devin"]}`)
	if status != http.StatusBadRequest || decoded["code"] != codeNotReady {
		t.Fatalf("initialize under invalid org = %d/%v, want 400/%s", status, decoded["code"], codeNotReady)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("rejected initialize wrote the root")
	}
}

// spec: gui-first-run/organization-telemetry-level-skips-consent
func TestFirstRunInitializeRejectsConsentWhenManaged(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\ntelemetry: {level: off}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	api := newFirstRunAPI(t, home)

	for _, body := range []string{
		`{"action":"initialize","targets":["devin"],"consent":false}`,
		`{"action":"initialize","targets":["devin"],"consent":true}`,
	} {
		status, decoded := initializeRequest(t, api, body)
		if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
			t.Fatalf("managed-consent initialize %q = %d/%v, want 400/%s", body, status, decoded["code"], codeInvalidRequest)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("managed-consent rejection wrote the root")
	}
}

// spec: session-interactions/guarded-submission
// spec: session-interactions/empty-selection
func TestFirstRunInitializeRejectsInvalidRequests(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)
	api := newFirstRunAPI(t, home)

	bodies := []string{
		`{"action":"initialize"}`,
		`{"action":"initialize","targets":[]}`,
		`{"action":"initialize","targets":["devin"]}`,
		`{"action":"initialize","targets":["devin"],"consent":null}`,
		`{"action":"initialize","targets":["devin"],"consent":"yes"}`,
		`{"action":"initialize","targets":["devin"],"consent":1}`,
		`{"action":"initialize","targets":["devin","unknown-new"],"consent":true}`,
		`{"action":"initialize","targets":["devin",""]}`,
		`{"action":"initialize","targets":["devin"],"consent":true,"extra":1}`,
		`{"action":"initialize","targets":["devin"],"consent":true,"consent":false}`,
		`{"action":"Initialize","targets":["devin"],"consent":true}`,
		`{"action":"initialize","targets":["devin"],"Consent":true}`,
		`{"action":"initialize","targets":["devin"],"consent":true} trailing`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":null}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":"source"}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":1}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":{}}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[null]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[1]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":["directory"]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[{}]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[{"kind":"directory","location":"/tmp/x","unknown":true}]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[{"kind":"directory","location":"/tmp/x","Kind":"git"}]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[{"kind":"directory","kind":"git","location":"/tmp/x"}]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[],"Sources":[]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[],"sources":[]}`,
		`{"action":"initialize","targets":["devin"],"consent":false,"sources":[],"extra":true}`,
		`{"action":"save-targets","targets":["devin"],"sources":[]}`,
		`{"action":"save-targets","targets":["devin"],"sources":null}`,
		`{"action":"save-targets","targets":["devin"],"consent":true}`,
		`{"action":"save-targets","targets":[]}`,
		`{"action":"add-source","source":{"kind":"directory","location":"/tmp/x"},"sources":[]}`,
		`{"action":"add-source","source":{"kind":"directory","location":"/tmp/x"},"sources":null}`,
		`{"action":"remove-source","id":"source-id","sources":[]}`,
		`{"action":"remove-source","id":"source-id","sources":null}`,
		`{"targets":["devin"],"consent":true}`,
		`{}`,
		``,
	}
	for _, body := range bodies {
		status, decoded := postRaw(t, api, "/api/configuration/actions", body)
		if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
			t.Fatalf("body %q = %d/%v, want 400/%s", body, status, decoded["code"], codeInvalidRequest)
		}
	}

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
	initBody := `{"action":"initialize","targets":["devin"],"consent":true}`
	if status := send(http.MethodGet, "/api/configuration/actions", api.Credential(), "", initBody); status != http.StatusMethodNotAllowed {
		t.Fatalf("GET action status = %d, want 405", status)
	}
	if status := send(http.MethodPost, "/api/configuration/actions", "", "", initBody); status != http.StatusUnauthorized {
		t.Fatalf("no-token action status = %d, want 401", status)
	}
	if status := send(http.MethodPost, "/api/configuration/actions", "wrong", "", initBody); status != http.StatusUnauthorized {
		t.Fatalf("wrong-token action status = %d, want 401", status)
	}
	if status := send(http.MethodPost, "/api/configuration/actions", api.Credential(), "https://evil.example", initBody); status != http.StatusForbidden {
		t.Fatalf("bad-origin action status = %d, want 403", status)
	}
	if status := send(http.MethodPost, "/api/configuration/actions?force=true", api.Credential(), "", initBody); status != http.StatusBadRequest {
		t.Fatalf("query action status = %d, want 400", status)
	}

	if _, err := os.Lstat(userConfig); !os.IsNotExist(err) {
		t.Fatal("rejected requests wrote the user root")
	}
	for _, path := range []string{
		filepath.Join(home, ".agents", "lock.yaml"),
		filepath.Join(repo, ".agents", "lock.yaml"),
		filepath.Join(repo, ".agents", "lock.local.yaml"),
		filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("rejected requests wrote %s (err=%v)", path, err)
		}
	}
	assertNoFirstRunResources(t, home)
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("rejected requests left entries under home: %v (err=%v)", entries, err)
	}
	if after, _ := os.ReadFile(repoConfig); !bytes.Equal(after, repoBefore) {
		t.Fatal("repo-committed config changed")
	}
	if after, _ := os.ReadFile(repoLocal); !bytes.Equal(after, localBefore) {
		t.Fatal("personal-local config changed")
	}
}

// spec: gui-configuration/save-user-harness-targets
func TestFirstRunInitializeRequiresUserMode(t *testing.T) {
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	status, decoded := postRaw(t, api, "/api/configuration/actions",
		`{"action":"initialize","targets":["devin"],"consent":true}`)
	if status != http.StatusBadRequest || decoded["code"] != codeNotSupported {
		t.Fatalf("workspace-mode initialize = %d/%v, want 400/%s", status, decoded["code"], codeNotSupported)
	}
}

// spec: gui-first-run/harness-targets-and-consent-match-init-output
// spec: gui-first-run/unmanaged-first-run-presents-init-equivalent-consent
func TestFirstRunInitializeWritesInitBytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		org      string
		body     string
		replay   string
		wantFile string
	}{
		{
			name:     "affirmative",
			body:     `{"action":"initialize","targets":["devin"],"consent":true}`,
			replay:   `{"action":"initialize","targets":["devin"],"consent":false}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - devin\ntelemetry:\n  level: basic\n",
		},
		{
			name:     "declined",
			body:     `{"action":"initialize","targets":["devin"],"consent":false}`,
			replay:   `{"action":"initialize","targets":["devin"],"consent":false}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - devin\n",
		},
		{
			name:     "empty source array",
			body:     `{"action":"initialize","targets":["devin"],"consent":false,"sources":[]}`,
			replay:   `{"action":"initialize","targets":["devin"],"consent":false,"sources":[]}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - devin\n",
		},
		{
			name:     "managed",
			org:      "apiVersion: agent-marketplace/v4\ntelemetry: {level: off}\n",
			body:     `{"action":"initialize","targets":["devin"]}`,
			replay:   `{"action":"initialize","targets":["devin"]}`,
			wantFile: "apiVersion: agent-marketplace/v4\nharnesses:\n  - devin\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, userConfig := firstRunHome(t)
			orgPath := filepath.Join(t.TempDir(), "org.yaml")
			var orgBytes []byte
			if tc.org != "" {
				orgBytes = []byte(tc.org)
				writeFile(t, orgPath, tc.org)
			}
			config.SetTestOrganizationConfigPath(t, orgPath)
			api := newFirstRunAPI(t, home)
			events := streamEvents(t, api)

			status, _ := postRaw(t, api, "/api/configuration", "")
			if status != http.StatusOK {
				t.Fatalf("metadata status = %d", status)
			}
			if _, err := os.Lstat(userConfig); !os.IsNotExist(err) {
				t.Fatal("metadata draft wrote the root")
			}

			status, decoded := initializeRequest(t, api, tc.body)
			if status != http.StatusAccepted {
				t.Fatalf("initialize status = %d, body = %v", status, decoded)
			}
			operation, _ := decoded["operation"].(map[string]any)
			if operation["kind"] != "mutation" {
				t.Fatalf("operation kind = %v, want mutation", operation["kind"])
			}
			var sawFinal, sawNotification bool
			for !sawNotification {
				event := nextEvent(t, events)
				if event.Type == "snapshot" && event.Operation != nil && event.Operation.Kind == "mutation" {
					sawFinal = false
				}
				if event.Type == "snapshot" && event.Operation == nil && event.snapshotStage() == "final" {
					sawFinal = true
				}
				if event.Type == "notification" {
					sawNotification = true
					if got := notificationSeverity(t, event); got != "info" {
						t.Fatalf("notification severity = %q, want info", got)
					}
				}
			}
			if !sawFinal {
				t.Fatal("no settled final snapshot before the completion notification")
			}
			if _, _, active := api.gate.Active(); active {
				t.Fatal("operation gate still held after completion")
			}

			data, err := os.ReadFile(userConfig)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.wantFile {
				t.Fatalf("root bytes = %q, want %q", data, tc.wantFile)
			}
			if tc.org != "" {
				if after, err := os.ReadFile(orgPath); err != nil || !bytes.Equal(after, orgBytes) {
					t.Fatal("organization config changed")
				}
			}
			if _, err := os.Stat(filepath.Join(home, ".devin", "skills")); !os.IsNotExist(err) {
				t.Fatal("initialize materialized generated resources")
			}

			status, _ = initializeRequest(t, api, tc.replay)
			if status != http.StatusBadRequest {
				t.Fatalf("replay initialize status = %d, want 400", status)
			}
			if after, _ := os.ReadFile(userConfig); string(after) != tc.wantFile {
				t.Fatal("replay overwrote the initialized root")
			}
		})
	}
}

// spec: session-interactions/guarded-submission
func TestFirstRunInitializeBusyAndRace(t *testing.T) {
	home, userConfig := firstRunHome(t)
	api := newFirstRunAPI(t, home)

	held, ok := api.gate.Begin(session.OperationSync)
	if !ok {
		t.Fatal("could not hold the gate")
	}
	status, decoded := initializeRequest(t, api, `{"action":"initialize","targets":["devin"],"consent":false}`)
	api.gate.Finish(held)
	if status != http.StatusConflict || decoded["code"] != codeBusy {
		t.Fatalf("busy initialize = %d/%v, want 409/%s", status, decoded["code"], codeBusy)
	}
	if _, err := os.Lstat(userConfig); !os.IsNotExist(err) {
		t.Fatal("busy initialize wrote the root")
	}

	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
	status, decoded = initializeRequest(t, api, `{"action":"initialize","targets":["devin"],"consent":true}`)
	if status != http.StatusBadRequest || decoded["code"] != codeNotReady {
		t.Fatalf("initialize over existing root = %d/%v, want 400/%s", status, decoded["code"], codeNotReady)
	}
	if data, _ := os.ReadFile(userConfig); string(data) != "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n" {
		t.Fatalf("existing root overwritten: %s", data)
	}
}

// spec: gui-first-run/detected-harness-targets-are-preselected-and-editable
func TestFirstRunDetectionUnavailable(t *testing.T) {
	home, userConfig := firstRunHome(t)
	loop := filepath.Join(home, ".factory")
	if err := os.Symlink(".factory", loop); err != nil {
		t.Fatal(err)
	}
	api := newFirstRunAPI(t, home)

	for range 2 {
		status, decoded := postRaw(t, api, "/api/configuration", "")
		if status != http.StatusOK {
			t.Fatalf("metadata status = %d", status)
		}
		if decoded["detectionUnavailable"] != true {
			t.Fatalf("detectionUnavailable = %v, want true", decoded["detectionUnavailable"])
		}
		if detected := configurationField(t, decoded, "detectedTargets"); len(detected) != 0 {
			t.Fatalf("detectedTargets = %v, want empty", detected)
		}
		body, _ := json.Marshal(decoded)
		for _, forbidden := range []string{home, loop} {
			if bytes.Contains(body, []byte(forbidden)) {
				t.Fatalf("metadata leaks underlying detection detail %q: %s", forbidden, body)
			}
		}
	}
	if diagnostics := api.diag.All(); len(diagnostics) != 0 {
		t.Fatalf("metadata read recorded diagnostics: %v", diagnostics)
	}
	if api.current != nil {
		t.Fatal("metadata read changed the current snapshot")
	}
	if _, _, active := api.gate.Active(); active {
		t.Fatal("metadata read held the operation gate")
	}
	if _, err := os.Lstat(userConfig); !os.IsNotExist(err) {
		t.Fatal("metadata read wrote the root")
	}
	if info, err := os.Lstat(loop); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("detection fixture loop changed")
	}

	events := streamEvents(t, api)
	status, decoded := initializeRequest(t, api, `{"action":"initialize","targets":["devin"],"consent":false}`)
	if status != http.StatusAccepted {
		t.Fatalf("manual selection initialize status = %d, body = %v", status, decoded)
	}
	for {
		event := nextEvent(t, events)
		if event.Type == "notification" {
			break
		}
	}
	if data, err := os.ReadFile(userConfig); err != nil || string(data) != "apiVersion: agent-marketplace/v4\nharnesses:\n  - devin\n" {
		t.Fatalf("root bytes = %q (err=%v)", data, err)
	}
}

// spec: gui-first-run/absent-user-personal-root-starts-first-run
// spec: gui-first-run/harness-targets-and-consent-match-init-output
func TestFirstRunInitializeBeforeStartupAnnouncesOperation(t *testing.T) {
	home, userConfig := firstRunHome(t)
	api := newFirstRunAPI(t, home)

	release := make(chan struct{})
	entered := make(chan struct{})
	var once, releaseOnce sync.Once
	realMaterialize := session.RunMaterialize
	session.RunMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		once.Do(func() { close(entered) })
		<-release
		return realMaterialize(ctx, root, artifacts, opts...)
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		session.RunMaterialize = realMaterialize
	})

	status, decoded := initializeRequest(t, api, `{"action":"initialize","targets":["devin"],"consent":false}`)
	if status != http.StatusAccepted {
		t.Fatalf("initialize status = %d, body = %v", status, decoded)
	}
	operation, _ := decoded["operation"].(map[string]any)
	acceptedID := operation["id"]

	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("post-initialize reload never reached the materialization dry run")
	}

	events := streamEvents(t, api)
	announced := nextEvent(t, events)
	if announced.Type != "snapshot" || announced.Operation == nil || announced.Operation.Kind != "mutation" {
		t.Fatalf("first event while blocked = %q/%+v, want an active mutation snapshot", announced.Type, announced.Operation)
	}
	if announced.Operation.ID != uint64(acceptedID.(float64)) {
		t.Fatalf("announced operation id = %d, want %v", announced.Operation.ID, acceptedID)
	}
	opGeneration := announced.Generation

	select {
	case event := <-events:
		t.Fatalf("event delivered while the operation reload was blocked: %q", event.Type)
	case <-time.After(200 * time.Millisecond):
	}

	releaseOnce.Do(func() { close(release) })
	var sawFinal bool
	for {
		event := nextEvent(t, events)
		if event.Generation != opGeneration {
			t.Fatalf("generation changed: announced %d, then %d", opGeneration, event.Generation)
		}
		if event.Type == "snapshot" && event.Operation == nil && event.snapshotStage() == "final" {
			sawFinal = true
		}
		if event.Type == "notification" {
			if !sawFinal {
				t.Fatal("completion notification arrived before the authoritative final snapshot")
			}
			break
		}
	}
	if _, _, active := api.gate.Active(); active {
		t.Fatal("operation gate still held after completion")
	}
	if data, err := os.ReadFile(userConfig); err != nil || string(data) != "apiVersion: agent-marketplace/v4\nharnesses:\n  - devin\n" {
		t.Fatalf("root bytes = %q (err=%v)", data, err)
	}
}

// spec: gui-first-run/optional-source-step-uses-the-configuration-form
func TestFirstRunInitializeAddsQueuedDirectoryAndMarketplaceSources(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	if err := os.MkdirAll(filepath.Join(home, ".devin"), 0o755); err != nil {
		t.Fatal(err)
	}
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	localBefore, err := os.ReadFile(repoLocal)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	directory := filepath.Join(home, "queued-directory")
	writeFile(t, filepath.Join(directory, "queued", "SKILL.md"), fixtureSkillBody)
	endpoint := newMarketplaceEndpointTestServer(t, "/first-run/catalog", "first-run-marketplace", "", 0)
	organizationPath := filepath.Join(t.TempDir(), "organization.yaml")
	organizationFragmentPath := filepath.Join(filepath.Dir(organizationPath), "fragment.yaml")
	organizationBytes := []byte("apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [claude]\n")
	organizationFragmentBytes := []byte("telemetry: {level: off}\n")
	writeFile(t, organizationPath, string(organizationBytes))
	writeFile(t, organizationFragmentPath, string(organizationFragmentBytes))
	config.SetTestOrganizationConfigPath(t, organizationPath)
	api := newFirstRunAPI(t, home, session.RuntimeLifecycle())
	events := streamEvents(t, api)

	body, err := json.Marshal(map[string]any{
		"action":  "initialize",
		"targets": []string{"devin"},
		"sources": []map[string]string{
			{"kind": "directory", "location": directory},
			{"kind": "marketplace", "location": endpoint.endpoint},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, decoded := initializeRequest(t, api, string(body))
	if status != http.StatusAccepted {
		t.Fatalf("initialize with optional sources status = %d, body = %v", status, decoded)
	}
	operation, ok := decoded["operation"].(map[string]any)
	if !ok || operation["kind"] != string(session.OperationMutation) {
		t.Fatalf("initialize operation = %v, want mutation", decoded["operation"])
	}
	acceptedID, ok := operation["id"].(float64)
	if !ok || acceptedID <= 0 {
		t.Fatalf("initialize operation id = %v, want positive id", operation["id"])
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
		t.Fatalf("initialize with optional sources severity = %q, want info", got)
	}
	snapshot := postSnapshot(t, api)
	if snapshot.Stage != "final" || snapshot.Operation != nil {
		t.Fatalf("settled snapshot = %#v, want final idle", snapshot)
	}
	if snapshot.SettledOperation == nil || snapshot.SettledOperation.Kind != string(session.OperationMutation) || snapshot.SettledOperation.ID != uint64(acceptedID) {
		t.Fatalf("settled operation = %#v, want accepted mutation %d", snapshot.SettledOperation, uint64(acceptedID))
	}
	metadataStatus, metadata := postRaw(t, api, "/api/configuration", "")
	if metadataStatus != http.StatusOK || metadata["rootPresent"] != true || metadata["firstRun"] != false || metadata["editable"] != true {
		t.Fatalf("post-initialize metadata = %d/%v", metadataStatus, metadata)
	}
	wantSources := map[string]bool{
		"directory|" + directory + "|||":           true,
		"marketplace|" + endpoint.endpoint + "|||": true,
	}
	if got := descSet(readUserSourceDescs(t, home)); !reflect.DeepEqual(got, wantSources) {
		t.Fatalf("effective sources = %v, want exactly %v", got, wantSources)
	}
	v4, err := config.LoadV4ConfigFromPath(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Sources) != 2 || len(v4.Groups) != 0 || len(v4.Requests) != 0 {
		t.Fatalf("optional source initialization selected Resources: sources=%#v groups=%#v requests=%#v", v4.Sources, v4.Groups, v4.Requests)
	}
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lockBytes, marketplaceEndpointLock(endpoint.endpoint, "first-run-marketplace")) {
		t.Fatalf("endpoint-only lock = %q", lockBytes)
	}
	if endpoint.requests.Load() == 0 {
		t.Fatal("queued Marketplace endpoint did not reach its local fixture")
	}
	if after, err := os.ReadFile(organizationPath); err != nil || !bytes.Equal(after, organizationBytes) {
		t.Fatalf("organization config changed: %q (err=%v)", after, err)
	}
	if after, err := os.ReadFile(organizationFragmentPath); err != nil || !bytes.Equal(after, organizationFragmentBytes) {
		t.Fatalf("organization fragment changed: %q (err=%v)", after, err)
	}
	if after, err := os.ReadFile(repoConfig); err != nil || !bytes.Equal(after, repoBefore) {
		t.Fatalf("repo config changed: %q (err=%v)", after, err)
	}
	if after, err := os.ReadFile(repoLocal); err != nil || !bytes.Equal(after, localBefore) {
		t.Fatalf("local config changed: %q (err=%v)", after, err)
	}
	assertNoFirstRunResources(t, home)
}

func TestFirstRunInitializeSourcePreflightRejectsPolicyBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name         string
		kind         string
		policy       string
		wantAllowed  bool
		relocateHome bool
	}{
		{name: "empty registry policy blocks Git", kind: "git", policy: "allowed_registries: []\n"},
		{name: "nil registry policy accepts Git preflight", kind: "git", wantAllowed: true},
		{name: "matching registry policy accepts Git preflight", kind: "git", policy: "allowed_registries: ['.*']\n", wantAllowed: true},
		{name: "empty Marketplace policy blocks endpoint", kind: "marketplace", policy: "allowed_marketplaces: []\n"},
		{name: "relocated HOME rejects queued Marketplace before init", kind: "marketplace", relocateHome: true},
		{name: "nil Marketplace policy accepts endpoint", kind: "marketplace", wantAllowed: true},
		{name: "matching Marketplace policy accepts endpoint", kind: "marketplace", policy: "allowed_marketplaces: ['.*']\n", wantAllowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, repo := newUserAPIFixture(t, false)
			if err := os.MkdirAll(filepath.Join(home, ".devin"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", t.TempDir())
			organizationPath := filepath.Join(t.TempDir(), "organization.yaml")
			organizationBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\n" + tc.policy)
			writeFile(t, organizationPath, string(organizationBytes))
			config.SetTestOrganizationConfigPath(t, organizationPath)
			userConfig := filepath.Join(home, ".agents", "config.yaml")
			userLock := filepath.Join(home, ".agents", "lock.yaml")
			repoConfig := filepath.Join(repo, ".agents", "config.yaml")
			repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
			repoBefore, err := os.ReadFile(repoConfig)
			if err != nil {
				t.Fatal(err)
			}
			localBefore, err := os.ReadFile(repoLocal)
			if err != nil {
				t.Fatal(err)
			}

			var source map[string]string
			var requests atomic.Int32
			var marketplace *marketplaceEndpointTestServer
			var gitServer *httptest.Server
			if tc.kind == "git" {
				gitServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					http.NotFound(w, r)
				}))
				t.Cleanup(gitServer.Close)
				source = map[string]string{"kind": "git", "location": gitServer.URL + "/repo.git", "ref": "main"}
			} else {
				marketplace = newMarketplaceEndpointTestServer(t, "/preflight/catalog", "preflight-marketplace", "", 0)
				source = map[string]string{"kind": "marketplace", "location": marketplace.endpoint}
			}
			api := newFirstRunAPI(t, home, session.RuntimeLifecycle())
			var decoyConfig string
			var decoyBefore []byte
			if tc.relocateHome {
				decoyHome := t.TempDir()
				decoyConfig = filepath.Join(decoyHome, ".agents", "config.yaml")
				decoyBefore = []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
				writeFile(t, decoyConfig, string(decoyBefore))
				t.Setenv("HOME", decoyHome)
			}
			body, err := json.Marshal(map[string]any{
				"action":  "initialize",
				"targets": []string{"devin"},
				"sources": []map[string]string{source},
			})
			if err != nil {
				t.Fatal(err)
			}
			status, decoded := initializeRequest(t, api, string(body))
			if tc.wantAllowed {
				if status != http.StatusAccepted {
					t.Fatalf("allowed %s initialize status = %d, body = %v; want 202", tc.kind, status, decoded)
				}
				operation, ok := decoded["operation"].(map[string]any)
				if !ok || operation["kind"] != string(session.OperationMutation) {
					t.Fatalf("accepted %s operation = %v, want mutation", tc.kind, decoded["operation"])
				}
				return
			}
			if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
				t.Fatalf("blocked %s initialize = %d/%v, want 400/%s", tc.kind, status, decoded["code"], codeInvalidRequest)
			}
			if gitServer != nil && requests.Load() != 0 {
				t.Fatalf("blocked Git source made %d HTTPS request(s) before initialization", requests.Load())
			}
			if marketplace != nil && marketplace.requests.Load() != 0 {
				t.Fatalf("blocked Marketplace source made %d request(s) before initialization", marketplace.requests.Load())
			}
			if _, err := os.Lstat(userConfig); !os.IsNotExist(err) {
				t.Fatalf("blocked source wrote user root: %v", err)
			}
			if _, err := os.Lstat(userLock); !os.IsNotExist(err) {
				t.Fatalf("blocked source wrote user lock: %v", err)
			}
			if decoyConfig != "" {
				if after, err := os.ReadFile(decoyConfig); err != nil || !bytes.Equal(after, decoyBefore) {
					t.Fatalf("relocated HOME changed decoy root: %q (err=%v)", after, err)
				}
				if _, err := os.Lstat(filepath.Join(filepath.Dir(decoyConfig), "lock.yaml")); !os.IsNotExist(err) {
					t.Fatalf("relocated HOME wrote decoy lock: %v", err)
				}
			}
			if after, err := os.ReadFile(organizationPath); err != nil || !bytes.Equal(after, organizationBytes) {
				t.Fatalf("blocked source changed organization config: %q (err=%v)", after, err)
			}
			if after, err := os.ReadFile(repoConfig); err != nil || !bytes.Equal(after, repoBefore) {
				t.Fatalf("blocked source changed repo config: %q (err=%v)", after, err)
			}
			if after, err := os.ReadFile(repoLocal); err != nil || !bytes.Equal(after, localBefore) {
				t.Fatalf("blocked source changed local config: %q (err=%v)", after, err)
			}
			if _, _, active := api.gate.Active(); active {
				t.Fatal("blocked source held the operation gate")
			}
			assertNoFirstRunResources(t, home)
			residue, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*"))
			if err != nil || len(residue) != 0 {
				t.Fatalf("blocked source left staging residue %v (err=%v)", residue, err)
			}
		})
	}
}

// spec: gui-first-run/organization-sources-skip-the-source-step
func TestFirstRunOrganizationSourcesSkipOptionalQueue(t *testing.T) {
	for _, tc := range []struct {
		name     string
		kind     string
		location string
		source   string
	}{
		{name: "directory", kind: "directory", location: "/org/directory", source: "directory: /org/directory"},
		{name: "git", kind: "git", location: "https://org.example/repo.git", source: "git: https://org.example/repo.git, ref: main"},
		{name: "marketplace", kind: "marketplace", location: "https://org.example/services/catalog", source: "marketplace: https://org.example/services/catalog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, userConfig := firstRunHome(t)
			organizationPath := filepath.Join(t.TempDir(), "organization.yaml")
			organizationBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\nsources:\n  organization: {" + tc.source + "}\n")
			writeFile(t, organizationPath, string(organizationBytes))
			config.SetTestOrganizationConfigPath(t, organizationPath)
			api := newFirstRunAPI(t, home, session.RuntimeLifecycle())

			status, metadata := postRaw(t, api, "/api/configuration", "")
			if status != http.StatusOK {
				t.Fatalf("configuration metadata status = %d, body = %v", status, metadata)
			}
			organizationSources, ok := metadata["organizationSources"].([]any)
			if !ok || len(organizationSources) != 1 {
				t.Fatalf("projected organization sources = %v, want one declared source", metadata["organizationSources"])
			}
			organizationSource, ok := organizationSources[0].(map[string]any)
			if !ok || organizationSource["kind"] != tc.kind || organizationSource["location"] != tc.location {
				t.Fatalf("organization source projection = %v, want %s %s", organizationSources[0], tc.kind, tc.location)
			}

			optionalDirectory := t.TempDir()
			queueBody, err := json.Marshal(map[string]any{
				"action":  "initialize",
				"targets": []string{"devin"},
				"sources": []map[string]string{{"kind": "directory", "location": optionalDirectory}},
			})
			if err != nil {
				t.Fatal(err)
			}
			status, decoded := initializeRequest(t, api, string(queueBody))
			if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
				t.Fatalf("initialization with organization sources = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
			}
			if _, err := os.Lstat(userConfig); !os.IsNotExist(err) {
				t.Fatalf("organization source skip refusal wrote user root: %v", err)
			}

			events := streamEvents(t, api)
			status, decoded = initializeRequest(t, api, `{"action":"initialize","targets":["devin"]}`)
			if status != http.StatusAccepted {
				t.Fatalf("source-less initialize with organization source = %d, body = %v", status, decoded)
			}
			if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
				t.Fatalf("source-less initialize notification severity = %q, want info", got)
			}
			if data, err := os.ReadFile(userConfig); err != nil || string(data) != "apiVersion: agent-marketplace/v4\nharnesses:\n  - devin\n" {
				t.Fatalf("source-less root = %q (err=%v)", data, err)
			}
			if after, err := os.ReadFile(organizationPath); err != nil || !bytes.Equal(after, organizationBytes) {
				t.Fatalf("organization source changed: %q (err=%v)", after, err)
			}
		})
	}
}

// spec: gui-first-run/optional-source-failure-keeps-initialized-configuration
func TestFirstRunInitializeOptionalSourceFailureKeepsRootAndAllowsRetry(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	if err := os.MkdirAll(filepath.Join(home, ".devin"), 0o755); err != nil {
		t.Fatal(err)
	}
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	localBefore, err := os.ReadFile(repoLocal)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	firstDirectory := filepath.Join(home, "first-directory")
	secondDirectory := filepath.Join(home, "second-directory")
	writeFile(t, filepath.Join(firstDirectory, "first", "SKILL.md"), fixtureSkillBody)
	writeFile(t, filepath.Join(secondDirectory, "second", "SKILL.md"), fixtureSkillBody)
	endpoint := newMarketplaceEndpointTestServer(t, "/optional/catalog", "optional-marketplace", "", 0)
	endpoint.failNextStatus(http.StatusInternalServerError)
	organizationPath := filepath.Join(t.TempDir(), "organization.yaml")
	organizationBytes := []byte("apiVersion: agent-marketplace/v4\ntelemetry: {level: off}\n")
	writeFile(t, organizationPath, string(organizationBytes))
	config.SetTestOrganizationConfigPath(t, organizationPath)
	api := newFirstRunAPI(t, home, session.RuntimeLifecycle())
	events := streamEvents(t, api)
	userLock := filepath.Join(home, ".agents", "lock.yaml")

	body, err := json.Marshal(map[string]any{
		"action":  "initialize",
		"targets": []string{"devin"},
		"sources": []map[string]string{
			{"kind": "directory", "location": firstDirectory},
			{"kind": "marketplace", "location": endpoint.endpoint},
			{"kind": "directory", "location": secondDirectory},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, decoded := initializeRequest(t, api, string(body))
	if status != http.StatusAccepted {
		t.Fatalf("initialize with optional source failure status = %d, body = %v", status, decoded)
	}
	operation, ok := decoded["operation"].(map[string]any)
	if !ok || operation["kind"] != string(session.OperationMutation) {
		t.Fatalf("initialize operation = %v, want mutation", decoded["operation"])
	}
	acceptedID, ok := operation["id"].(float64)
	if !ok || acceptedID <= 0 {
		t.Fatalf("initialize operation id = %v, want positive id", operation["id"])
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "error" {
		t.Fatalf("optional source failure notification severity = %q, want error", got)
	}
	diagnostics := mutationDiagnostics(t, api)
	if len(diagnostics) != 1 || diagnostics[0].Severity != "error" || diagnostics[0].Code == "" || !strings.Contains(diagnostics[0].Message, "add optional source:") {
		t.Fatalf("optional source failure diagnostics = %#v, want one coded wrapped source error", diagnostics)
	}
	final := postSnapshot(t, api)
	if final.Stage != "final" || final.Operation != nil {
		t.Fatalf("settled failure snapshot = %#v, want final idle", final)
	}
	if final.SettledOperation == nil || final.SettledOperation.Kind != string(session.OperationMutation) || final.SettledOperation.ID != uint64(acceptedID) {
		t.Fatalf("settled failure operation = %#v, want accepted mutation %d", final.SettledOperation, uint64(acceptedID))
	}
	if endpoint.requests.Load() != 1 {
		t.Fatalf("failed optional Marketplace request count = %d, want exactly one before retry", endpoint.requests.Load())
	}
	wantRoot := "apiVersion: agent-marketplace/v4\nharnesses:\n  - devin\nsources:\n  first-directory:\n    directory: " + firstDirectory + "\n"
	rootAfterFailure, err := os.ReadFile(userConfig)
	if err != nil || string(rootAfterFailure) != wantRoot {
		t.Fatalf("root after optional failure = %q (err=%v), want retained init and first source:\n%s", rootAfterFailure, err, wantRoot)
	}
	wantFailureSources := map[string]bool{"directory|" + firstDirectory + "|||": true}
	if got := descSet(readUserSourceDescs(t, home)); !reflect.DeepEqual(got, wantFailureSources) {
		t.Fatalf("effective sources after failure = %v, want only prior successful source %v", got, wantFailureSources)
	}
	if _, err := os.Lstat(userLock); !os.IsNotExist(err) {
		t.Fatalf("failed endpoint wrote a user lock: %v", err)
	}
	if status, metadata := postRaw(t, api, "/api/configuration", ""); status != http.StatusOK || metadata["rootPresent"] != true || metadata["firstRun"] != false || metadata["editable"] != true {
		t.Fatalf("retained-root metadata = %d/%v", status, metadata)
	}
	if after, err := os.ReadFile(organizationPath); err != nil || !bytes.Equal(after, organizationBytes) {
		t.Fatalf("optional source failure changed organization config: %q (err=%v)", after, err)
	}
	if after, err := os.ReadFile(repoConfig); err != nil || !bytes.Equal(after, repoBefore) {
		t.Fatalf("optional source failure changed repo config: %q (err=%v)", after, err)
	}
	if after, err := os.ReadFile(repoLocal); err != nil || !bytes.Equal(after, localBefore) {
		t.Fatalf("optional source failure changed local config: %q (err=%v)", after, err)
	}
	assertNoFirstRunResources(t, home)

	status, decoded = initializeRequest(t, api, `{"action":"initialize","targets":["devin"]}`)
	if status != http.StatusBadRequest || decoded["code"] != codeNotReady {
		t.Fatalf("reinitialization after optional source failure = %d/%v, want 400/%s", status, decoded["code"], codeNotReady)
	}
	if after, err := os.ReadFile(userConfig); err != nil || !bytes.Equal(after, rootAfterFailure) {
		t.Fatalf("reinitialization changed retained root: %q (err=%v)", after, err)
	}

	if err := marketplace.DefaultDiscoveryCache().Store(marketplace.DiscoveryOutcome{
		Endpoint:   config.MarketplaceEntry{URL: endpoint.endpoint, InstanceID: endpoint.instanceID},
		ObservedAt: time.Now(),
	}, "", "", marketplace.DiscoveryPageLimit); err != nil {
		t.Fatal(err)
	}

	retryBody := `{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(endpoint.endpoint) + `}}`
	status, decoded = sourceAction(t, api, retryBody)
	if status != http.StatusAccepted {
		t.Fatalf("ordinary Marketplace retry status = %d, body = %v", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
		t.Fatalf("ordinary Marketplace retry severity = %q, want info", got)
	}
	if endpoint.requests.Load() != 3 {
		t.Fatalf("Marketplace requests after retry = %d, want failure then registration and snapshot recovery", endpoint.requests.Load())
	}
	wantRetrySources := map[string]bool{
		"directory|" + firstDirectory + "|||":      true,
		"marketplace|" + endpoint.endpoint + "|||": true,
	}
	if got := descSet(readUserSourceDescs(t, home)); !reflect.DeepEqual(got, wantRetrySources) {
		t.Fatalf("effective sources after retry = %v, want %v", got, wantRetrySources)
	}
	if got := mutationDiagnostics(t, api); len(got) != 0 {
		t.Fatalf("successful retry retained source error diagnostics: %#v", got)
	}
	lockBytes, err := os.ReadFile(userLock)
	if err != nil || !bytes.Equal(lockBytes, marketplaceEndpointLock(endpoint.endpoint, "optional-marketplace")) {
		t.Fatalf("retry endpoint lock = %q (err=%v)", lockBytes, err)
	}
	if after, err := os.ReadFile(organizationPath); err != nil || !bytes.Equal(after, organizationBytes) {
		t.Fatalf("ordinary retry changed organization config: %q (err=%v)", after, err)
	}
	if after, err := os.ReadFile(repoConfig); err != nil || !bytes.Equal(after, repoBefore) {
		t.Fatalf("ordinary retry changed repo config: %q (err=%v)", after, err)
	}
	if after, err := os.ReadFile(repoLocal); err != nil || !bytes.Equal(after, localBefore) {
		t.Fatalf("ordinary retry changed local config: %q (err=%v)", after, err)
	}
	if after, err := os.ReadFile(userConfig); err != nil || strings.Contains(string(after), filepath.Base(secondDirectory)) {
		t.Fatalf("second queued directory was attempted: %q (err=%v)", after, err)
	}
	assertNoFirstRunResources(t, home)
}
