package localapi

import (
	"testing"

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

func TestUserSnapshotReportsPendingChanges(t *testing.T) {
	api, _, _ := newUserAPI(t, session.DependencyLifecycle{})

	events := streamEvents(t, api)
	final := awaitFinalSnapshot(t, events)
	if final.PendingChanges < 1 {
		t.Fatalf("final snapshot pendingChanges = %d, want >= 1 for an unsynced resource", final.PendingChanges)
	}
}
