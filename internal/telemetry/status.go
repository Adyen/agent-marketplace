package telemetry

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"time"
)

// StatusInfo is a telemetry control surface snapshot. It exposes effective
// policy, identity, durable state, and the last delivery failure without
// sending any telemetry.
type StatusInfo struct {
	State            TelemetryState
	InstallationID   string
	QueueCount       int
	QueueSize        int64
	QueueOldestAge   time.Duration
	QueueOldestAgeOK bool
	DiagnosticCount  int
	LastFailure      DeliveryFailure
	LastFailureError error
}

// GetStatusInfo resolves the effective telemetry policy and gathers local
// durable state without creating an installation ID and without sending data.
func GetStatusInfo(ctx context.Context) (StatusInfo, error) {
	var info StatusInfo

	state, err := LoadAndResolve(ctx)
	if err != nil {
		return info, fmt.Errorf("resolve telemetry config: %w", err)
	}
	info.State = state

	var readErr error
	info.InstallationID, readErr = ReadInstallationID(state.InstallationIDPath)
	if readErr != nil {
		slog.Warn("telemetry status: failed to read installation id", "path", state.InstallationIDPath, "error", readErr)
	}

	queueRoot := state.QueueRoot()
	q := NewQueue(queueRoot)
	info.QueueCount = q.Count()
	info.QueueSize = q.Size()
	info.QueueOldestAge, info.QueueOldestAgeOK = q.OldestAge()

	stateRoot := state.StateRoot()
	info.DiagnosticCount = countDiagnosticPayloads(stateRoot)

	info.LastFailure, info.LastFailureError = ReadLastDeliveryFailure(filepath.Join(stateRoot, "last-delivery-failure"))

	slog.Info("telemetry status gathered",
		"effective_level", state.EffectiveLevel,
		"level_source", state.EffectiveLevelSource,
		"primary_host", state.PrimaryEndpointHostname,
		"primary_source", state.PrimaryEndpointSource,
		"community_enabled", state.CommunityReportingEnabled,
		"community_source", state.CommunityReportingSource,
		"installation_id_set", info.InstallationID != "",
		"queue_count", info.QueueCount,
	)

	return info, nil
}

// countDiagnosticPayloads returns the number of retained diagnostic payload
// files under the diagnostic subdirectory of stateRoot.
func countDiagnosticPayloads(stateRoot string) int {
	diagRoot := filepath.Join(stateRoot, "diagnostic")
	var count int
	_ = filepath.WalkDir(diagRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		count++
		return nil
	})
	return count
}
