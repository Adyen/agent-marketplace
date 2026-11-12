package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/claudedelivery"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type claudeDeliveryStatus struct {
	Resource               string                    `json:"resource"`
	PluginName             string                    `json:"plugin_name"`
	PluginID               string                    `json:"plugin_id"`
	State                  store.ClaudeDeliveryState `json:"state"`
	PublishedVersionID     string                    `json:"published_version_id"`
	ServedVersionID        string                    `json:"served_version_id"`
	ContentScanStatus      string                    `json:"content_scan_status"`
	Reach                  string                    `json:"reach"`
	InstallationPreference string                    `json:"installation_preference"`
	LastFailureCode        string                    `json:"last_failure_code"`
	StatusMessage          string                    `json:"status_message,omitempty"`
}

func (s *Server) handleAdminClaudeDelivery(w http.ResponseWriter, r *http.Request) {
	state, ok := s.meta.(store.ClaudeDeliveryStateStore)
	if !ok {
		writeAdminServiceError(w, store.ErrTopology)
		return
	}
	rows, err := state.ListClaudeDeliveryPlugins(r.Context())
	if err != nil {
		writeAdminServiceError(w, err)
		return
	}
	plugins := make([]claudeDeliveryStatus, 0, len(rows))
	for _, row := range rows {
		if row.Coordinate.Marketplace != s.mpID {
			continue
		}
		plugins = append(plugins, claudeDeliveryStatus{
			Resource:   fmt.Sprintf("%s/%s/%s", row.Coordinate.Registry, row.Coordinate.Kind, row.Coordinate.Name),
			PluginName: row.PluginName, PluginID: row.PluginID, State: row.State,
			PublishedVersionID: row.PublishedVersionID, ServedVersionID: row.ServedVersionID,
			ContentScanStatus: row.ContentScanStatus, Reach: row.Reach,
			InstallationPreference: row.InstallationPreference, LastFailureCode: row.LastErrorCode,
			StatusMessage: claudedelivery.StatusMessage(row),
		})
	}
	slices.SortFunc(plugins, func(a, b claudeDeliveryStatus) int {
		return strings.Compare(a.Resource, b.Resource)
	})
	writeAdminJSON(w, r, http.StatusOK, struct {
		Plugins []claudeDeliveryStatus `json:"plugins"`
	}{plugins})
}
