package localapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func awaitFinalSnapshot(t *testing.T, events <-chan sessionEvent) snapshotDTO {
	t.Helper()
	for {
		event := nextEvent(t, events)
		if event.Type != "snapshot" {
			continue
		}
		snapshot := event.snapshotPayload(t)
		if snapshot.Stage == "final" {
			return snapshot
		}
	}
}

func postMigrate(t *testing.T, api *API) (int, string) {
	t.Helper()
	status, decoded := postRaw(t, api, "/api/actions", `{"action":"migrate"}`)
	code, _ := decoded["code"].(string)
	return status, code
}

func writeLegacyUserRoot(t *testing.T, home, body string) string {
	t.Helper()
	path := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, path, body)
	return path
}

func newLegacyUserAPI(t *testing.T, rootBody string, lifecycle session.DependencyLifecycle) (*API, string, string) {
	t.Helper()
	home, repo := newUserAPIFixture(t, false)
	writeLegacyUserRoot(t, home, rootBody)
	api, err := NewUser(context.Background(), home, lifecycle, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	return api, home, repo
}

func legacyUserRootBody(apiVersion, source string) string {
	return "apiVersion: " + apiVersion + "\nharnesses: [devin]\nregistries:\n  - url: " + source + "\n    default: true\n"
}

// spec: session-interactions/legacy-user-configuration-is-offered-for-migration
func TestUserSnapshotReportsPendingChangesAndMigrationOffer(t *testing.T) {
	for _, apiVersion := range []string{"agent-marketplace/v2", "agent-marketplace/v3"} {
		t.Run(apiVersion, func(t *testing.T) {
			source := t.TempDir()
			writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
			api, _, _ := newLegacyUserAPI(t, legacyUserRootBody(apiVersion, source), session.DependencyLifecycle{})

			events := streamEvents(t, api)
			final := awaitFinalSnapshot(t, events)
			if !final.MigrationOffered {
				t.Fatal("final snapshot did not offer the legacy user migration")
			}
			if final.PendingChanges < 1 {
				t.Fatalf("final snapshot pendingChanges = %d, want >= 1 for an unsynced resource", final.PendingChanges)
			}
		})
	}
}

// spec: session-interactions/legacy-user-configuration-is-offered-for-migration
func TestUserSnapshotOmitsMigrationOfferForV4Root(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})

	events := streamEvents(t, api)
	final := awaitFinalSnapshot(t, events)
	if final.MigrationOffered {
		t.Fatal("v4 user root must not be offered for migration")
	}
}

// spec: session-interactions/accepting-the-offer-migrates-the-user-personal-root
func TestUserMigrationMigratesOnlyUserRoot(t *testing.T) {
	for _, apiVersion := range []string{"agent-marketplace/v2", "agent-marketplace/v3"} {
		t.Run(apiVersion, func(t *testing.T) {
			source := t.TempDir()
			writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
			api, home, repo := newLegacyUserAPI(t, legacyUserRootBody(apiVersion, source), session.DependencyLifecycle{})

			orgPath := filepath.Join(t.TempDir(), "org.yaml")
			writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
			config.SetTestOrganizationConfigPath(t, orgPath)

			userConfig := filepath.Join(home, ".agents", "config.yaml")
			repoConfig := filepath.Join(repo, ".agents", "config.yaml")
			repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
			repoBefore, _ := os.ReadFile(repoConfig)
			localBefore, _ := os.ReadFile(repoLocal)
			orgBefore, _ := os.ReadFile(orgPath)

			events := streamEvents(t, api)
			if final := awaitFinalSnapshot(t, events); !final.MigrationOffered {
				t.Fatal("legacy root was not offered for migration")
			}

			if status, _ := postMigrate(t, api); status != http.StatusAccepted {
				t.Fatalf("migrate status = %d, want 202", status)
			}
			notification := awaitNotification(t, events)
			data, err := json.Marshal(notification.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "migration complete") {
				t.Fatalf("migration notification = %s, want completion", data)
			}

			final := postSnapshot(t, api)
			if final.MigrationOffered {
				t.Fatal("post-migration snapshot still offers migration")
			}
			var solo *rowDTO
			for i := range final.Rows {
				if final.Rows[i].Name == "solo" {
					solo = &final.Rows[i]
				}
			}
			if solo == nil {
				t.Fatal("post-migration snapshot lost the solo resource row")
			}
			if solo.Scope != "user" || !solo.Checked {
				t.Fatalf("solo row scope=%q checked=%v, want user/true", solo.Scope, solo.Checked)
			}
			if len(solo.Harnesses) != 1 || solo.Harnesses[0] != "devin" {
				t.Fatalf("solo row harnesses = %v, want [devin]", solo.Harnesses)
			}

			migrated, err := os.ReadFile(userConfig)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(migrated), "agent-marketplace/v4") {
				t.Fatalf("user root not migrated to v4:\n%s", migrated)
			}
			if !strings.Contains(string(migrated), source) {
				t.Fatalf("migrated user root lost the declared source:\n%s", migrated)
			}
			if after, _ := os.ReadFile(repoConfig); string(after) != string(repoBefore) {
				t.Fatal("repo-committed config changed during user migration")
			}
			if after, _ := os.ReadFile(repoLocal); string(after) != string(localBefore) {
				t.Fatal("personal-local config changed during user migration")
			}
			if after, _ := os.ReadFile(orgPath); string(after) != string(orgBefore) {
				t.Fatal("organization config changed during user migration")
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "devin")); !os.IsNotExist(err) {
				t.Fatal("migration materialized target output without a sync")
			}
		})
	}
}

// spec: session-interactions/accepting-the-offer-migrates-the-user-personal-root
func TestUserMigrationRefusesStalePreflight(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
	api, home, _ := newLegacyUserAPI(t, legacyUserRootBody("agent-marketplace/v2", source), session.DependencyLifecycle{})

	events := streamEvents(t, api)
	if final := awaitFinalSnapshot(t, events); !final.MigrationOffered {
		t.Fatal("legacy root was not offered for migration")
	}

	userConfig := filepath.Join(home, ".agents", "config.yaml")
	replacement := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n"
	writeFile(t, userConfig, replacement)

	status, code := postMigrate(t, api)
	if status != http.StatusBadRequest || code != codeNotReady {
		t.Fatalf("stale migrate status/code = %d/%q, want 400/%q", status, code, codeNotReady)
	}
	if after, _ := os.ReadFile(userConfig); string(after) != replacement {
		t.Fatal("refused migration changed the externally replaced root")
	}
}

// spec: session-interactions/guarded-submission
func TestUserMigrationRefusesNonLegacyMissingAndForeignPayloads(t *testing.T) {
	api, home, _ := newUserAPI(t, session.DependencyLifecycle{})
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	before, _ := os.ReadFile(userConfig)

	events := streamEvents(t, api)
	awaitFinalSnapshot(t, events)

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{"v4 root migrate", `{"action":"migrate"}`, codeNotReady},
		{"migrate with caller path", `{"action":"migrate","path":"/tmp/x"}`, codeInvalidRequest},
		{"migrate with caller scope", `{"action":"migrate","scope":"user"}`, codeInvalidRequest},
		{"duplicate action", `{"action":"migrate","action":"migrate"}`, codeInvalidRequest},
		{"unknown action", `{"action":"editor"}`, codeInvalidRequest},
		{"malformed body", `{"action":`, codeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, decoded := postRaw(t, api, "/api/actions", tc.body)
			code, _ := decoded["code"].(string)
			if status != http.StatusBadRequest || code != tc.code {
				t.Fatalf("status/code = %d/%q, want 400/%q", status, code, tc.code)
			}
		})
	}
	if after, _ := os.ReadFile(userConfig); string(after) != string(before) {
		t.Fatal("refused actions changed user configuration")
	}
}

// spec: session-interactions/guarded-submission
func TestUserMigrationRefusesMissingRoot(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	awaitFinalSnapshot(t, events)

	status, code := postMigrate(t, api)
	if status != http.StatusBadRequest || code != codeNotReady {
		t.Fatalf("missing-root migrate status/code = %d/%q, want 400/%q", status, code, codeNotReady)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("user root appeared after refused migration")
	}
}

// spec: session-interactions/guarded-submission
func TestUserMigrationRejectsBadCredentialAndOrigin(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
	api, home, _ := newLegacyUserAPI(t, legacyUserRootBody("agent-marketplace/v2", source), session.DependencyLifecycle{})
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	before, _ := os.ReadFile(userConfig)

	events := streamEvents(t, api)
	if final := awaitFinalSnapshot(t, events); !final.MigrationOffered {
		t.Fatal("legacy root was not offered for migration")
	}

	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	send := func(token, origin string) int {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/actions", strings.NewReader(`{"action":"migrate"}`))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("X-Agent-Marketplace-Token", token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}
	if status := send("", ""); status != http.StatusUnauthorized {
		t.Fatalf("migrate without credential status = %d, want 401", status)
	}
	if status := send(strings.Repeat("11", 32), ""); status != http.StatusUnauthorized {
		t.Fatalf("migrate with bad credential status = %d, want 401", status)
	}
	if status := send(api.Credential(), "https://evil.example"); status != http.StatusForbidden {
		t.Fatalf("migrate with foreign origin status = %d, want 403", status)
	}
	if after, _ := os.ReadFile(userConfig); string(after) != string(before) {
		t.Fatal("rejected migrate caller changed user configuration")
	}
}

// spec: session-interactions/accepting-the-offer-migrates-the-user-personal-root
func TestUserMigrationAcceptedWithoutSupportedTargets(t *testing.T) {
	api, home, _ := newLegacyUserAPI(t, "apiVersion: agent-marketplace/v2\nharnesses: []\n", session.DependencyLifecycle{})

	events := streamEvents(t, api)
	if final := awaitFinalSnapshot(t, events); !final.MigrationOffered {
		t.Fatal("legacy root was not offered for migration")
	}
	if status := postAction(t, api, "sync"); status != http.StatusBadRequest {
		t.Fatalf("sync without targets status = %d, want 400", status)
	}

	if status, _ := postMigrate(t, api); status != http.StatusAccepted {
		t.Fatalf("migrate without targets status = %d, want 202", status)
	}
	awaitNotification(t, events)

	migrated, err := os.ReadFile(filepath.Join(home, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migrated), "agent-marketplace/v4") {
		t.Fatalf("user root not migrated to v4:\n%s", migrated)
	}
}

// spec: session-interactions/guarded-submission
func TestUserMigrationBusySerializerMakesNoWrites(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
	release := make(chan struct{})
	api, home, _ := newLegacyUserAPI(t, legacyUserRootBody("agent-marketplace/v2", source), session.DependencyLifecycle{
		Sync: func(ctx context.Context, ws pipeline.Workspace) ([]error, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil, nil
		},
	})
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	before, _ := os.ReadFile(userConfig)

	events := streamEvents(t, api)
	awaitFinalSnapshot(t, events)

	if status := postAction(t, api, "sync"); status != http.StatusAccepted {
		t.Fatalf("sync status = %d, want 202", status)
	}
	status, code := postMigrate(t, api)
	if status != http.StatusConflict || code != codeBusy {
		t.Fatalf("busy migrate status/code = %d/%q, want 409/%q", status, code, codeBusy)
	}
	if after, _ := os.ReadFile(userConfig); string(after) != string(before) {
		t.Fatal("busy-refused migration changed user configuration")
	}
	close(release)
	awaitNotification(t, events)
}

// spec: session-interactions/guarded-submission
func TestUserMigrationStaysBusyUntilFinalAssessment(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
	api, home, _ := newLegacyUserAPI(t, legacyUserRootBody("agent-marketplace/v2", source), session.DependencyLifecycle{})
	userConfig := filepath.Join(home, ".agents", "config.yaml")

	events := streamEvents(t, api)
	if final := awaitFinalSnapshot(t, events); !final.MigrationOffered {
		t.Fatal("legacy root was not offered for migration")
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)
	realDiscover := session.DiscoverCatalog
	session.DiscoverCatalog = func(dir, sourceHarness string) (catalog.LoadedCatalog, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		return realDiscover(dir, sourceHarness)
	}
	t.Cleanup(func() { session.DiscoverCatalog = realDiscover })

	if status, _ := postMigrate(t, api); status != http.StatusAccepted {
		t.Fatalf("migrate status = %d, want 202", status)
	}
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("post-migration assessment never reached catalog discovery")
	}
	if status := postAction(t, api, "sync"); status != http.StatusConflict {
		t.Fatalf("sync during held post-migration assessment status = %d, want 409", status)
	}
	releaseOnce()
	notification := awaitNotification(t, events)
	data, err := json.Marshal(notification.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "migration complete") {
		t.Fatalf("migration notification = %s, want completion", data)
	}
	migrated, err := os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migrated), "agent-marketplace/v4") {
		t.Fatalf("user root not migrated to v4:\n%s", migrated)
	}
	if final := postSnapshot(t, api); final.MigrationOffered {
		t.Fatal("post-migration snapshot still offers migration")
	}
}

// spec: session-interactions/accepting-the-offer-migrates-the-user-personal-root
func TestWorkspaceModeRejectsMigration(t *testing.T) {
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	status, code := postMigrate(t, api)
	if status != http.StatusBadRequest || code != codeNotSupported {
		t.Fatalf("workspace-mode migrate status/code = %d/%q, want 400/%q", status, code, codeNotSupported)
	}
}
