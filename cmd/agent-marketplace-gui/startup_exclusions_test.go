package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/session"
	startupfixture "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/startup"
)

func assertWork50HostRows(t *testing.T, rows []startupHostRow, scope string, checked, pending bool) {
	t.Helper()
	if len(rows) != 2 {
		t.Fatal("host snapshot did not contain both Directory rows")
	}
	ids, names := make(map[string]bool, 2), make(map[string]bool, 2)
	for _, row := range rows {
		if row.ID == "" {
			t.Fatal("Directory row identity was empty")
		}
		if ids[row.ID] {
			t.Fatal("Directory row identities were duplicated")
		}
		if row.Kind != "skill" {
			t.Fatal("Directory row kind was not skill")
		}
		if row.Registry != "local" {
			t.Fatal("Directory row registry was not local")
		}
		if row.Scope != scope {
			t.Fatal("Directory row scope did not match the expected source layer")
		}
		if row.Checked != checked {
			t.Fatal("Directory row selection state did not match the expected stage")
		}
		if row.PendingVerification != pending {
			t.Fatal("Directory row verification state did not match the expected stage")
		}
		if checked && row.ResourceAction != "none" {
			t.Fatal("pending configured Directory row exposed a resource action")
		}
		ids[row.ID] = true
		names[row.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Fatal("host snapshot did not contain both fixture skill names")
	}
}

// spec: session-startup/excluded-only-configuration-publishes-no-provisional-rows
func TestGuiHostExcludedOnlyDirectorySelectionsHaveNoProvisionalRows(t *testing.T) {
	for _, tc := range []struct {
		name       string
		exclusions string
		excluded   bool
	}{{name: "control"}, {name: "excluded", exclusions: ", exclude: [skill/alpha, skill/beta]", excluded: true}} {
		t.Run(tc.name, func(t *testing.T) {
			paths := startupHostFixture(t, func(source string) string {
				return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\nresources:\n  local: {all: true%s}\n", source, tc.exclusions)
			})
			before := captureProtectedHostPaths(t, paths)
			app, err := NewApp(context.Background(), session.DependencyLifecycle{})
			if err != nil {
				t.Fatal("create GUI host for Directory exclusion fixture")
			}
			t.Cleanup(app.close)
			stages := readHostStartupStages(t, app)
			assertHostStartupStageOrder(t, stages)
			if stages[3].Payload.Drift != "ready" {
				t.Fatal("valid Directory exclusion fixture did not finish ready")
			}
			if tc.excluded {
				if len(stages[0].Payload.Rows) != 0 || len(stages[1].Payload.Rows) != 0 {
					t.Fatal("same-layer excluded selections appeared in provisional or config stages")
				}
				assertWork50HostRows(t, stages[2].Payload.Rows, "available", false, false)
				assertWork50HostRows(t, stages[3].Payload.Rows, "available", false, false)
			} else {
				assertWork50HostRows(t, stages[0].Payload.Rows, "user", true, true)
				assertWork50HostRows(t, stages[1].Payload.Rows, "user", true, true)
			}
			app.close()
			assertProtectedHostPathsUnchanged(t, paths, before)
		})
	}
}

type work50HostRow struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Kind                string   `json:"kind"`
	Registry            string   `json:"registry"`
	Scope               string   `json:"scope"`
	Required            bool     `json:"required"`
	Checked             bool     `json:"checked"`
	PendingVerification bool     `json:"pendingVerification"`
	HarnessOverride     bool     `json:"harnessOverride"`
	Harnesses           []string `json:"harnesses"`
	Status              string   `json:"status"`
	Group               string   `json:"group"`
	ResourceAction      string   `json:"resourceAction"`
}

type work50HostGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type work50HostEvent struct {
	Type       string `json:"type"`
	Sequence   uint64 `json:"sequence"`
	Generation uint64 `json:"generation"`
	Payload    struct {
		Stage       string                  `json:"stage"`
		Drift       string                  `json:"drift"`
		Rows        []work50HostRow         `json:"rows"`
		Groups      []work50HostGroup       `json:"groups"`
		Diagnostics []startupHostDiagnostic `json:"diagnostics"`
	} `json:"payload"`
}

func readWork50HostStages(t *testing.T, app *App) []work50HostEvent {
	t.Helper()
	server := httptest.NewServer(app.handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal("create authenticated mixed-pin event request")
	}
	request.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal("open authenticated mixed-pin event stream")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatal("mixed-pin event stream returned unexpected status")
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	stages := make([]work50HostEvent, 0, 4)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event work50HostEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			t.Fatal("decode mixed-pin host snapshot event")
		}
		if event.Type != "snapshot" {
			continue
		}
		stages = append(stages, event)
		if event.Payload.Stage == "final" {
			return stages
		}
	}
	if scanner.Err() != nil {
		t.Fatal("read mixed-pin host snapshot stream")
	}
	t.Fatal("mixed-pin host stream ended before final")
	return nil
}

func work50HostStages(t *testing.T, required bool, userConfig func(fixture *startupfixture.Fixture, source string) string) []work50HostEvent {
	t.Helper()
	return work50HostRun(t, required, func(f *startupfixture.Fixture) []byte { return f.OrganizationConfig(required) }, userConfig, func(app *App) []work50HostEvent {
		return readWork50HostStages(t, app)
	})
}

func work50HostRun(t *testing.T, required bool, orgConfig func(fixture *startupfixture.Fixture) []byte, userConfig func(fixture *startupfixture.Fixture, source string) string, body func(app *App) []work50HostEvent) []work50HostEvent {
	t.Helper()
	var fixture *startupfixture.Fixture
	paths := startupHostFixture(t, func(source string) string {
		fixture = startupfixture.New(t, marketplace.DefaultCache())
		return userConfig(fixture, source)
	})
	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "config.yaml")
	if err := os.WriteFile(orgPath, orgConfig(fixture), 0o600); err != nil {
		t.Fatal("write owned mixed-pin organization config")
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	orgLockPath := config.Organization.LockPath(orgPath)
	if err := os.WriteFile(orgLockPath, fixture.OrganizationLock(t), 0o600); err != nil {
		t.Fatal("write owned mixed-pin organization lock")
	}
	paths = append(paths, orgDir, orgPath, orgLockPath)
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create host GUI for mixed organization pins")
	}
	t.Cleanup(app.close)
	stages := body(app)
	app.close()
	fixture.Server.Close()
	assertProtectedHostPathsUnchanged(t, paths, before)
	return stages
}

type work50HostIdentity struct {
	ID, Name, Kind, Registry, Scope, Group string
	Required, HarnessOverride              bool
	Harnesses                              string
}

func work50HostIdentities(rows []work50HostRow) []work50HostIdentity {
	identities := make([]work50HostIdentity, len(rows))
	for i, row := range rows {
		harnesses := ""
		if row.HarnessOverride {
			harnesses = strings.Join(row.Harnesses, "\x00")
		}
		identities[i] = work50HostIdentity{row.ID, row.Name, row.Kind, row.Registry, row.Scope, row.Group, row.Required, row.HarnessOverride, harnesses}
	}
	return identities
}

func work50HostGroupedLayout(rows []work50HostRow, groups []work50HostGroup) [][]work50HostIdentity {
	layout := make([][]work50HostIdentity, len(groups))
	for i, group := range groups {
		var members []work50HostRow
		for _, row := range rows {
			if row.Group == group.ID {
				members = append(members, row)
			}
		}
		slices.SortFunc(members, func(a, b work50HostRow) int {
			return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Kind, b.Kind), strings.Compare(a.Registry, b.Registry), strings.Compare(a.Scope, b.Scope), strings.Compare(a.ID, b.ID))
		})
		layout[i] = work50HostIdentities(members)
	}
	return layout
}

func assertWork50HostMixedRows(t *testing.T, stage int, rows []work50HostRow, required, excludeReview, pending bool) {
	t.Helper()
	if len(rows) != 2 {
		t.Fatalf("mixed-pin host stage %d: expected two organization skills", stage)
	}
	ids := make(map[string]bool, len(rows))
	for i, wantName := range []string{"audit", "review"} {
		row := rows[i]
		if row.ID == "" || ids[row.ID] {
			t.Fatalf("mixed-pin host stage %d row %d: identity condition failed", stage, i)
		}
		ids[row.ID] = true
		if row.Name != wantName || row.Kind != "skill" || row.Scope != "organization" {
			t.Fatalf("mixed-pin host stage %d row %d: organization coordinate condition failed", stage, i)
		}
		if row.Required != required {
			t.Fatalf("mixed-pin host stage %d row %d: required-state condition failed", stage, i)
		}
		wantChecked := i == 0 || required || !excludeReview
		if row.Checked != wantChecked {
			t.Fatalf("mixed-pin host stage %d row %d: checked-state condition failed", stage, i)
		}
		if row.PendingVerification != pending {
			t.Fatalf("mixed-pin host stage %d row %d: pending-state condition failed", stage, i)
		}
		wantAction := "none"
		if !pending && !required {
			wantAction = "exclude"
			if i == 1 && excludeReview {
				wantAction = "restore"
			}
		}
		if row.ResourceAction != wantAction {
			t.Fatalf("mixed-pin host stage %d row %d: resource-action condition failed", stage, i)
		}
		wantGroup := "enabled"
		if required {
			wantGroup = "required"
		} else if i == 1 && excludeReview {
			wantGroup = "available"
		}
		if row.Group != wantGroup {
			t.Fatalf("mixed-pin host stage %d row %d: status-group condition failed", stage, i)
		}
		if pending && !strings.Contains(row.Status, "verification pending") {
			t.Fatalf("mixed-pin host stage %d row %d: pending-status condition failed", stage, i)
		}
		if i == 1 && excludeReview && !required && !strings.Contains(row.Status, "excluded") {
			t.Fatalf("mixed-pin host stage %d row %d: exclusion-status condition failed", stage, i)
		}
	}
}

// spec: session-startup/excluded-configured-selections-render-unchecked-in-their-verified-position
func TestGuiHostExcludedOrganizationPinRetainsVerifiedPosition(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		excludeReview, required bool
	}{
		{name: "optional-visible"},
		{name: "optional-user-exclusion", excludeReview: true},
		{name: "required-user-exclusion-immunity", excludeReview: true, required: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stages := work50HostStages(t, tc.required, func(f *startupfixture.Fixture, _ string) string {
				return string(f.UserConfig(tc.excludeReview))
			})
			if len(stages) != 4 {
				t.Fatal("mixed-pin host did not deliver four startup stages")
			}
			generation := stages[0].Generation
			if generation == 0 {
				t.Fatal("mixed-pin host generation was zero")
			}
			for i, stage := range stages {
				if stage.Type != "snapshot" || stage.Generation != generation || stage.Payload.Stage != []string{"provisional", "config", "inventory", "final"}[i] {
					t.Fatal("mixed-pin host stage order or generation changed")
				}
				if i > 0 && stage.Sequence <= stages[i-1].Sequence {
					t.Fatal("mixed-pin host event sequence was not increasing")
				}
			}
			for i := 0; i < 3; i++ {
				assertWork50HostMixedRows(t, i, stages[i].Payload.Rows, tc.required, tc.excludeReview, i < 2)
				for _, row := range stages[i].Payload.Rows {
					if !slices.ContainsFunc(stages[i].Payload.Groups, func(group work50HostGroup) bool { return group.ID == row.Group }) {
						t.Fatal("mixed-pin row group was absent from host group metadata")
					}
				}
				if i > 0 {
					if !reflect.DeepEqual(work50HostIdentities(stages[0].Payload.Rows), work50HostIdentities(stages[i].Payload.Rows)) {
						t.Fatalf("mixed-pin host visible row metadata changed at stage %d", i)
					}
					if !reflect.DeepEqual(stages[0].Payload.Groups, stages[i].Payload.Groups) {
						t.Fatalf("mixed-pin host group metadata changed at stage %d", i)
					}
				}
			}
		})
	}
}
