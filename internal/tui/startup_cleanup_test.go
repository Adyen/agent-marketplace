package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Init starts its snapshot worker eagerly. Join it before fixture loaders and
// HOME are restored, without running unrelated commands such as release checks.
func joinStartupSnapshot(t *testing.T, initCmd tea.Cmd) {
	t.Helper()
	cmds := batchCmds(initCmd)
	if len(cmds) == 0 {
		t.Fatal("Init returned no startup commands")
	}
	t.Cleanup(func() {
		msg := cmds[0]()
		if _, closed := msg.(snapshotStreamClosedMsg); closed {
			return
		}
		snapshot, ok := msg.(snapshotLoadedMsg)
		if !ok || snapshot.stages == nil {
			t.Fatalf("startup command returned %T without a snapshot stream", msg)
		}
		for range snapshot.stages {
		}
	})
}

func TestStartupSnapshotJoinsBeforeFixtureCleanup(t *testing.T) {
	for _, tc := range []struct {
		name     string
		consumed int
	}{
		{"unconsumed", 0},
		{"first stage consumed", 1},
		{"all stages consumed", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stages := make(chan snapshotLoadedMsg)
			finished := make(chan struct{})
			go func() {
				defer close(stages)
				stages <- snapshotLoadedMsg{stages: stages}
				stages <- snapshotLoadedMsg{stages: stages}
				close(finished)
			}()
			t.Cleanup(func() {
				for range stages {
				}
			})
			t.Run("fixture", func(t *testing.T) {
				t.Cleanup(func() {
					select {
					case <-finished:
					default:
						t.Error("fixture cleanup ran before the startup worker finished")
					}
				})
				cmd := tea.Batch(waitSnapshotStages(stages), func() tea.Msg {
					t.Error("snapshot cleanup executed an unrelated startup command")
					return nil
				})
				joinStartupSnapshot(t, cmd)
				for range tc.consumed {
					batchCmds(cmd)[0]()
				}
			})
		})
	}
}
