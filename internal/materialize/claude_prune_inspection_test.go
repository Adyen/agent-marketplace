package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: claude-native-configuration/declared-aggregate-inspection-errors-remain-visible
func TestClaudeDeclaredPruneInspectionErrors(t *testing.T) {
	for _, shape := range []string{"missing", "not-directory", "cycle"} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			problem := filepath.Join(root, "a-problem")
			switch shape {
			case "not-directory":
				claudeDirectoryWrite(t, problem, "not a component directory")
			case "cycle":
				if err := os.Symlink("a-problem", problem); err != nil {
					t.Fatal(err)
				}
			}
			good := filepath.Join(root, "z-good.json")
			claudeDirectoryWrite(t, good, pruneWalkGood)
			boundary := &destinationBoundary{machineScope: true, homeDir: root, roots: []string{root}}
			report, diags := pruneUncoveredManagedMergeFiles(root, nil, false, nil,
				map[string]bool{"a-problem/plugin.json": true, "z-good.json": true}, boundary)
			if shape == "cycle" {
				if len(diags) != 1 || diags[0].Severity != diag.SeverityError || diags[0].Resource != "a-problem/plugin.json" {
					t.Fatalf("inspection failure disappeared: %+v", diags)
				}
			} else if len(diags) != 0 {
				t.Fatalf("genuine absence refused: %+v", diags)
			}
			if string(claudeDirectoryRead(t, good)) != pruneWalkGoodPruned || len(report.Written) != 1 {
				t.Fatalf("inspection prevented safe sibling cleanup: %+v", report)
			}
		})
	}
}
