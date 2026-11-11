package materialize

import (
	"os"
	"path/filepath"
	"testing"
)

// spec: claude-native-configuration/legacy-cleanup-preserves-unmarked-hard-linked-json
func TestLegacyPrunePreservesUnmarkedHardlinks(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "check"}[dryRun], func(t *testing.T) {
			root := t.TempDir()
			manual := filepath.Join(root, "manual.json")
			claudeDirectoryWrite(t, manual, `{"handwritten":true}`)
			if err := os.Link(manual, filepath.Join(root, "other.json")); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(manual)
			if err != nil {
				t.Fatal(err)
			}
			good := filepath.Join(root, "z-owned.json")
			claudeDirectoryWrite(t, good, pruneWalkGood)
			boundary := &destinationBoundary{machineScope: true, homeDir: root, roots: []string{root}}
			report, diags := pruneUncoveredManagedMergeFiles(root, nil, dryRun, nil, nil, boundary)
			if len(diags) != 0 || len(report.Written) != 1 {
				t.Fatalf("legacy unmarked file blocked cleanup: %+v, %+v", report, diags)
			}
			after, err := os.Stat(manual)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("unmarked file metadata changed: %v", err)
			}
			if string(claudeDirectoryRead(t, manual)) != `{"handwritten":true}` {
				t.Fatal("unmarked file bytes changed")
			}
			want := pruneWalkGoodPruned
			if dryRun {
				want = pruneWalkGood
			}
			if string(claudeDirectoryRead(t, good)) != want {
				t.Fatal("legacy sibling cleanup changed")
			}
		})
	}
}
