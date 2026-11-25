package localapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func saveSettings(t *testing.T, api *API, body string) (int, map[string]any) {
	t.Helper()
	return postRaw(t, api, "/api/configuration/actions", body)
}

func configurationPayload(t *testing.T, api *API) configurationDTO {
	t.Helper()
	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("configuration status = %d, body = %v", status, decoded)
	}
	data, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var dto configurationDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		t.Fatal(err)
	}
	return dto
}

// spec: gui-configuration/settings-read-reports-the-source-of-each-field
func TestConfigurationSettingsProjectionReportsSources(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nbackup:\n  max_days: 7\n")
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nupdate_check:\n  enabled: false\n")
	config.SetTestOrganizationConfigPath(t, orgPath)

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	settings := configurationPayload(t, api).Settings
	if settings.Backup.MaxDays.Source != "user" || settings.Backup.MaxDays.Value != float64(7) {
		t.Fatalf("backup.maxDays = %+v, want user 7", settings.Backup.MaxDays)
	}
	if settings.Backup.Enabled.Source != "default" || settings.Backup.Enabled.Value != true {
		t.Fatalf("backup.enabled = %+v, want default true", settings.Backup.Enabled)
	}
	if settings.UpdateCheck.Enabled.Source != "organization" || settings.UpdateCheck.Enabled.Locked {
		t.Fatalf("updateCheck.enabled = %+v, want unlocked organization default", settings.UpdateCheck.Enabled)
	}
	if settings.Telemetry.Level.Locked {
		t.Fatalf("telemetry.level locked without an organization level: %+v", settings.Telemetry.Level)
	}
}

// spec: gui-configuration/save-settings-writes-the-changed-keys
// spec: gui-configuration/save-refuses-an-invalid-value-without-writing
func TestConfigurationSaveSettingsWritesAndRefusesInvalid(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	before, _ := os.ReadFile(userConfig)
	status, body := saveSettings(t, api, `{"action":"save-settings","backup":{"maxDays":-1}}`)
	if status != http.StatusBadRequest || body["code"] != codeInvalidSetting {
		t.Fatalf("invalid save status = %d body = %v", status, body)
	}
	if after, _ := os.ReadFile(userConfig); string(after) != string(before) {
		t.Fatalf("refused save changed the file:\n%s", after)
	}

	status, body = saveSettings(t, api, `{"action":"save-settings","backup":{"maxDays":5},"logging":{"level":"warn"}}`)
	if status != http.StatusAccepted {
		t.Fatalf("save-settings status = %d body = %v", status, body)
	}
	for {
		event := nextEvent(t, events)
		if event.Type == "notification" {
			if got := notificationSeverity(t, event); got != "info" {
				t.Fatalf("notification severity = %q", got)
			}
			break
		}
	}
	data, _ := os.ReadFile(userConfig)
	content := string(data)
	for _, want := range []string{"max_days: 5", "level: warn", "apiVersion: agent-marketplace/v4"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q in:\n%s", want, content)
		}
	}
}

// spec: gui-configuration/organization-telemetry-level-locks-its-field
// spec: gui-configuration/save-refuses-a-locked-telemetry-level
func TestConfigurationSaveSettingsRefusesLockedTelemetry(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry:\n  level: off\n")
	config.SetTestOrganizationConfigPath(t, orgPath)

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	if !configurationPayload(t, api).Settings.Telemetry.Level.Locked {
		t.Fatal("organization telemetry level should lock the telemetry field")
	}
	before, _ := os.ReadFile(userConfig)
	status, body := saveSettings(t, api, `{"action":"save-settings","telemetry":{"level":"basic"}}`)
	if status != http.StatusConflict || body["code"] != codeSettingLocked {
		t.Fatalf("locked save status = %d body = %v", status, body)
	}
	if after, _ := os.ReadFile(userConfig); string(after) != string(before) {
		t.Fatalf("locked save changed the file:\n%s", after)
	}
}
