package main

import (
	"bufio"
	"bytes"
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

	"github.com/Adyen/agent-marketplace/internal/session"
	startupfixture "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/startup"
)

type work52ActionPlan struct {
	RowID  string `json:"rowId"`
	PlanID string `json:"planId"`
	Action string `json:"action"`
}

type work52Operation struct {
	Operation struct {
		ID   uint64 `json:"id"`
		Kind string `json:"kind"`
	} `json:"operation"`
}

type work52SettledEvent struct {
	Type    string `json:"type"`
	Payload struct {
		Stage            string          `json:"stage"`
		Rows             []work50HostRow `json:"rows"`
		SettledOperation *struct {
			ID uint64 `json:"id"`
		} `json:"settledOperation"`
	} `json:"payload"`
}

type work52LayoutKey struct {
	ID, Name, Kind, Registry, Scope string
	HarnessOverride                 bool
	Harnesses                       string
}

func work52LayoutKeys(rows []work50HostRow) []work52LayoutKey {
	keys := make([]work52LayoutKey, len(rows))
	for i, row := range rows {
		keys[i] = work52LayoutKey{row.ID, row.Name, row.Kind, row.Registry, row.Scope, row.HarnessOverride, strings.Join(row.Harnesses, "\x00")}
	}
	return keys
}

func work52RowNamed(t *testing.T, rows []work50HostRow, name string) work50HostRow {
	t.Helper()
	var found []work50HostRow
	for _, row := range rows {
		if row.Name == name {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatal("single-view fixture did not contain exactly one row with the expected name")
	}
	return found[0]
}

func work52WaitSettled(t *testing.T, app *App, operationID uint64) []work50HostRow {
	t.Helper()
	server := httptest.NewServer(app.handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal("create authenticated single-view event request")
	}
	request.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal("open authenticated single-view event stream")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatal("single-view event stream returned unexpected status")
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event work52SettledEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			t.Fatal("decode single-view host snapshot event")
		}
		if event.Type == "snapshot" && event.Payload.Stage == "final" && event.Payload.SettledOperation != nil && event.Payload.SettledOperation.ID == operationID {
			return event.Payload.Rows
		}
	}
	t.Fatal("single-view operation did not settle with an authoritative final snapshot")
	return nil
}

// spec: session-startup/provisional-rows-retain-single-view-position-and-guarded-actions
func TestGuiHostSingleViewProvisionalContinuityAndGuardedActions(t *testing.T) {
	t.Run("user-toggle-and-sync", func(t *testing.T) {
		paths := startupHostFixture(t, func(source string) string {
			return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin, claude]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\nresources:\n  local: {all: true, harnesses: [devin]}\n", source)
		})
		userConfig := filepath.Join(os.Getenv("HOME"), ".agents", "config.yaml")
		others := slices.DeleteFunc(slices.Clone(paths), func(path string) bool { return path == userConfig })
		before := captureProtectedHostPaths(t, others)
		userBefore, err := os.ReadFile(userConfig)
		if err != nil {
			t.Fatal("read user-personal config before toggle")
		}
		app, err := NewApp(context.Background(), session.DependencyLifecycle{})
		if err != nil {
			t.Fatal("create user-scope host GUI for single-view continuity")
		}
		t.Cleanup(app.close)
		stages := readWork50HostStages(t, app)
		if len(stages) != 4 {
			t.Fatal("single-view fixture did not deliver four startup stages")
		}
		for i, stage := range stages {
			if stage.Generation != stages[0].Generation || stage.Payload.Stage != []string{"provisional", "config", "inventory", "final"}[i] {
				t.Fatal("single-view fixture changed startup order or generation")
			}
			for _, row := range stage.Payload.Rows {
				if !row.HarnessOverride || !reflect.DeepEqual(row.Harnesses, []string{"devin"}) {
					t.Fatal("single-view row lost its harness-override badge")
				}
			}
		}
		want := work50HostGroupedLayout(stages[0].Payload.Rows, stages[0].Payload.Groups)
		for i := 1; i < 3; i++ {
			if !reflect.DeepEqual(work50HostGroupedLayout(stages[i].Payload.Rows, stages[i].Payload.Groups), want) {
				t.Fatal("provisional single-view layout differs from the verified inventory layout")
			}
			if !reflect.DeepEqual(stages[i].Payload.Groups, stages[0].Payload.Groups) {
				t.Fatal("provisional single-view group metadata differs from the verified inventory layout")
			}
		}
		for _, stage := range stages[:2] {
			for _, row := range stage.Payload.Rows {
				if row.PendingVerification && row.ResourceAction != "none" {
					t.Fatal("unverified provisional row exposed a resource action")
				}
			}
		}
		if !reflect.DeepEqual(work52LayoutKeys(stages[0].Payload.Rows), work52LayoutKeys(stages[3].Payload.Rows)) {
			t.Fatal("provisional single-view identities differ from the final assessment")
		}
		alpha := work52RowNamed(t, stages[3].Payload.Rows, "alpha")
		beta := work52RowNamed(t, stages[3].Payload.Rows, "beta")
		for _, row := range []work50HostRow{alpha, beta} {
			if row.Group != "pending" || row.Status != "enabled (pending sync)" {
				t.Fatal("final assessment did not mark selected rows pending sync")
			}
		}
		status, body := requestGUIAPI(t, app, http.MethodPost, "/api/resource-plan", `{"rowId":"`+alpha.ID+`"}`, true)
		if status != http.StatusOK {
			t.Fatal("eligible user-personal row did not produce a resource plan")
		}
		var plan work52ActionPlan
		if err := json.Unmarshal(body, &plan); err != nil || plan.Action != "remove" || plan.PlanID == "" {
			t.Fatal("eligible enabled user-personal row did not offer removal")
		}
		status, body = requestGUIAPI(t, app, http.MethodPost, "/api/resource-actions", `{"rowId":"`+alpha.ID+`","planId":"`+plan.PlanID+`","acceptRisk":false}`, true)
		if status != http.StatusAccepted {
			t.Fatal("eligible resource plan was not accepted for execution")
		}
		var accepted work52Operation
		if err := json.Unmarshal(body, &accepted); err != nil {
			t.Fatal("decode accepted resource action operation")
		}
		settled := work52WaitSettled(t, app, accepted.Operation.ID)
		if work52RowNamed(t, settled, "alpha").Checked || !work52RowNamed(t, settled, "beta").Checked {
			t.Fatal("toggle did not change only the chosen user-personal selection")
		}
		if work52RowNamed(t, settled, "beta").ID != beta.ID {
			t.Fatal("toggle changed the identity of an unrelated row")
		}
		userAfter, err := os.ReadFile(userConfig)
		if err != nil || bytes.Equal(userBefore, userAfter) {
			t.Fatal("toggle did not write the user-personal config")
		}
		assertProtectedHostPathsUnchanged(t, others, before)

		status, body = requestGUIAPI(t, app, http.MethodPost, "/api/actions", `{"action":"sync"}`, true)
		if status != http.StatusAccepted {
			t.Fatal("sync was not accepted after the authoritative reload")
		}
		var syncOperation work52Operation
		if err := json.Unmarshal(body, &syncOperation); err != nil || syncOperation.Operation.Kind != "sync" {
			t.Fatal("sync request did not return a sync operation")
		}
		work52WaitSettled(t, app, syncOperation.Operation.ID)
	})

	t.Run("required-pins-refuse-mutation", func(t *testing.T) {
		work50HostRun(t, true, func(f *startupfixture.Fixture) []byte {
			return f.OrganizationConfig(true)
		}, func(f *startupfixture.Fixture, _ string) string {
			return string(f.UserConfig(false))
		}, func(app *App) []work50HostEvent {
			stages := readWork50HostStages(t, app)
			if len(stages) != 4 {
				t.Fatal("required-pin fixture did not deliver four startup stages")
			}
			required := 0
			for _, row := range stages[3].Payload.Rows {
				if !row.Required {
					continue
				}
				required++
				if row.ResourceAction != "none" {
					t.Fatal("verified required row exposed a resource action")
				}
				status, _ := requestGUIAPI(t, app, http.MethodPost, "/api/resource-plan", `{"rowId":"`+row.ID+`"}`, true)
				if status != http.StatusBadRequest {
					t.Fatal("mutation plan against a required row was not refused")
				}
			}
			if required != 2 {
				t.Fatal("required-pin fixture did not expose both verified required rows")
			}
			return stages
		})
	})
}
