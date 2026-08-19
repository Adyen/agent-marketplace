package telemetry

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// Purge removes all queued telemetry records and retained diagnostic payloads.
// It does not remove the installation ID or change configuration. The returned
// counts are best-effort snapshots taken before removal.
func Purge(installationIDPath string) (queueCount int, diagnosticCount int, err error) {
	if installationIDPath == "" {
		return 0, 0, fmt.Errorf("empty installation id path")
	}

	stateRoot := filepath.Dir(installationIDPath)

	queueRoot := queueRootFromInstallationIDPath(installationIDPath)
	q := NewQueue(queueRoot)
	queueCount = q.Count()
	if queueRoot != "" {
		if rerr := os.RemoveAll(queueRoot); rerr != nil {
			slog.Error("failed to purge queue", "path", queueRoot, "error", rerr)
			return queueCount, diagnosticCount, fmt.Errorf("purge queue: %w", rerr)
		}
	}

	diagRoot := filepath.Join(stateRoot, "diagnostic")
	diagnosticCount = countFiles(diagRoot)
	if diagRoot != "" {
		if rerr := os.RemoveAll(diagRoot); rerr != nil {
			slog.Error("failed to purge diagnostic payloads", "path", diagRoot, "error", rerr)
			return queueCount, diagnosticCount, fmt.Errorf("purge diagnostic payloads: %w", rerr)
		}
	}

	slog.Info("telemetry purge complete", "queue_count", queueCount, "diagnostic_count", diagnosticCount)
	return queueCount, diagnosticCount, nil
}

func countFiles(root string) int {
	var count int
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		count++
		return nil
	})
	return count
}
