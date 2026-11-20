package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: claude-enterprise-delivery/a-withheld-version-is-visible-to-the-operator
// spec: claude-enterprise-delivery/status-reports-a-failure-by-code-without-content
func TestAdminClaudeDeliveryStatusListsSanitizedStateBehindAdminAuth(t *testing.T) {
	s, handler := adminHTTPFixture(t)
	state := s.meta.(store.ClaudeDeliveryStateStore)
	now := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	if err := s.meta.PutMarketplace(context.Background(), store.Marketplace{
		ID: "other-marketplace", Name: "Other", CanonicalURL: "https://other.marketplace.test",
	}); err != nil {
		t.Fatalf("register foreign marketplace: %v", err)
	}
	rows := []store.ClaudeDeliveryPlugin{
		{
			Coordinate: store.ResourceCoordinate{Marketplace: "admin-test", Registry: "registry", Kind: "skill", Name: "guide"},
			PluginName: "am-registry-skill-guide", PluginID: "plugin_guide",
			TargetMarketplaceID: "marketplace_1", PublishedVersionID: "version_2",
			LatestVersionID: "version_2", ServedVersionID: "version_1",
			ContentScanStatus: "processing", Reach: "remote", InstallationPreference: "available",
			State:            store.ClaudeDeliveryStatePublished,
			LastErrorMessage: "sanitized failure", CreatedAt: now, UpdatedAt: now,
		},
		{
			Coordinate: store.ResourceCoordinate{Marketplace: "admin-test", Registry: "registry", Kind: "command", Name: "retired"},
			PluginName: "am-registry-command-retired", PluginID: "plugin_retired",
			TargetMarketplaceID: "marketplace_1", PublishedVersionID: "version_old",
			ServedVersionID: "version_old", ContentScanStatus: "completed", Reach: "remote",
			InstallationPreference: "not_available", State: store.ClaudeDeliveryStatePublished,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			Coordinate: store.ResourceCoordinate{Marketplace: "other-marketplace", Registry: "registry", Kind: "skill", Name: "foreign"},
			PluginName: "am-registry-skill-foreign", PluginID: "plugin_foreign",
			TargetMarketplaceID: "marketplace_1", PublishedVersionID: "version_foreign",
			LatestVersionID: "version_foreign", ServedVersionID: "version_foreign",
			ContentScanStatus: "completed", Reach: "organization", InstallationPreference: "available",
			State: store.ClaudeDeliveryStatePublished, CreatedAt: now, UpdatedAt: now,
		},
		{
			Coordinate: store.ResourceCoordinate{Marketplace: "admin-test", Registry: "registry", Kind: "mcpserver", Name: "failed"},
			PluginName: "am-registry-mcpserver-failed", PluginID: "plugin_failed",
			TargetMarketplaceID: "marketplace_1", State: store.ClaudeDeliveryStatePublished,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, row := range rows {
		if err := state.UpsertClaudeDeliveryPublished(context.Background(), row); err != nil {
			t.Fatalf("seed delivery state: %v", err)
		}
		if row.Coordinate.Name == "retired" {
			if err := state.MarkClaudeDeliveryRetired(context.Background(), row.Coordinate, now); err != nil {
				t.Fatalf("retire delivery state: %v", err)
			}
		}
		if row.Coordinate.Name == "failed" {
			if err := state.RecordClaudeDeliveryFailure(context.Background(), row.Coordinate,
				row.PluginName, row.TargetMarketplaceID, "upload.failed", now); err != nil {
				t.Fatalf("record delivery failure: %v", err)
			}
		}
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/admin/claude-delivery", nil)
	unauthorized.RemoteAddr = "127.0.0.1:4567"
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status request = %d, want 401: %s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	request := adminAuthRequest(http.MethodGet)
	request.URL.Path = "/api/v1/admin/claude-delivery"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("authenticated delivery status = %d %s", response.Code, response.Body.String())
	}
	type statusEntry struct {
		Resource               string `json:"resource"`
		PluginName             string `json:"plugin_name"`
		PluginID               string `json:"plugin_id"`
		State                  string `json:"state"`
		PublishedVersionID     string `json:"published_version_id"`
		ServedVersionID        string `json:"served_version_id"`
		ContentScanStatus      string `json:"content_scan_status"`
		Reach                  string `json:"reach"`
		InstallationPreference string `json:"installation_preference"`
		LastFailureCode        string `json:"last_failure_code"`
		StatusMessage          string `json:"status_message"`
	}
	var body struct {
		Plugins []statusEntry `json:"plugins"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Plugins) != 3 {
		t.Fatalf("delivery response has %d plugins, want 3: %s", len(body.Plugins), response.Body.String())
	}
	if strings.Contains(response.Body.String(), "plugin_foreign") {
		t.Fatalf("delivery status included a plugin from another marketplace: %s", response.Body.String())
	}
	var withheld, withdrawn, failed *statusEntry
	for i := range body.Plugins {
		entry := &body.Plugins[i]
		switch entry.Resource {
		case "registry/skill/guide":
			withheld = entry
		case "registry/command/retired":
			withdrawn = entry
		case "registry/mcpserver/failed":
			failed = entry
		}
	}
	if withheld == nil || withheld.Resource != "registry/skill/guide" ||
		withheld.PluginName != "am-registry-skill-guide" || withheld.PluginID != "plugin_guide" ||
		withheld.State != "published" || withheld.PublishedVersionID != "version_2" ||
		withheld.ServedVersionID != "version_1" || withheld.ContentScanStatus != "processing" ||
		withheld.Reach != "remote" || withheld.InstallationPreference != "available" ||
		!strings.Contains(withheld.StatusMessage, "awaiting its content scan") {
		t.Fatalf("withheld delivery status = %+v", withheld)
	}
	if withdrawn == nil || withdrawn.Resource != "registry/command/retired" || withdrawn.State != "retired" ||
		!strings.Contains(withdrawn.StatusMessage, "installed copies") ||
		!strings.Contains(withdrawn.StatusMessage, "version history") {
		t.Fatalf("withdrawn delivery status = %+v", withdrawn)
	}
	if failed == nil || failed.State != "failed" || failed.LastFailureCode != "upload.failed" {
		t.Fatalf("failed delivery status = %+v", failed)
	}
	for _, forbidden := range []string{"sanitized failure", "sk-ant-secret", "resource-body", "hook-command", "mcp-secret"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("delivery status exposed %q: %s", forbidden, response.Body.String())
		}
	}

	write := adminAuthRequest(http.MethodPost)
	write.URL.Path = "/api/v1/admin/claude-delivery"
	writeResponse := httptest.NewRecorder()
	handler.ServeHTTP(writeResponse, write)
	if writeResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delivery status accepted a write: %d %s", writeResponse.Code, writeResponse.Body.String())
	}
}
