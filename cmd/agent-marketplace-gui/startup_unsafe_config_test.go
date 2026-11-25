package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

// spec: session-startup/browse-safe-config-is-never-published-for-unsafe-configurations
func TestGuiHostUnsafeConfigurationNeverPublishesBrowseSafeConfig(t *testing.T) {
	paths := startupHostFixture(t, func(string) string { return "apiVersion: [broken\n" })
	marketURL, entered, release, closeMarket := blockedWork47Marketplace(t)
	go func() {
		select {
		case <-entered:
			release()
		case <-t.Context().Done():
		}
	}()
	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "config.yaml")
	orgConfig := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  mp: {marketplace: %q}\nresources:\n  mp: {include: [tools/skill/remote]}\n", marketURL)
	if err := os.WriteFile(orgPath, []byte(orgConfig), 0o600); err != nil {
		t.Fatal("write owned organization fixture")
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	paths = append(paths, orgDir, orgPath, config.Organization.LockPath(orgPath))
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create GUI host for unsafe user config")
	}
	var apiClose func()
	cleanup := func() {
		release()
		app.close()
		if apiClose != nil {
			apiClose()
		}
		closeMarket()
	}
	t.Cleanup(cleanup)
	stages := readHostStartupStages(t, app)
	if len(stages) != 1 || stages[0].Type != "snapshot" || stages[0].Generation == 0 ||
		stages[0].Payload.Stage != "final" || stages[0].Payload.Drift != "failed" || len(stages[0].Payload.Rows) != 0 {
		t.Fatal("unsafe user config did not produce exactly one failed final snapshot")
	}
	hasError := false
	for _, diagnostic := range stages[0].Payload.Diagnostics {
		if diagnostic.Severity == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Fatal("unsafe final snapshot lacked an error diagnostic")
	}
	server := newHostAPIServer(t, app)
	apiClose = server.Close
	t.Cleanup(cleanup)
	status, body := postHostAPI(t, server, app, "/api/snapshot", nil)
	if status != 200 {
		t.Fatal("snapshot endpoint did not return success")
	}
	var current struct {
		Stage            string            `json:"stage"`
		Drift            string            `json:"drift"`
		DiscoveryEnabled *bool             `json:"discoveryEnabled"`
		Rows             []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(body, &current); err != nil {
		t.Fatal("decode current snapshot state")
	}
	if current.Stage != "final" || current.Drift != "failed" || current.DiscoveryEnabled == nil ||
		*current.DiscoveryEnabled || len(current.Rows) != 0 {
		t.Fatal("unsafe current snapshot exposed browse-safe discovery state")
	}
	cleanup()
	assertProtectedHostPathsUnchanged(t, paths, before)
}
