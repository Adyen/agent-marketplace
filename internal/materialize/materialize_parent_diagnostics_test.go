package materialize

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestExecuteTargetPlansReportsEachFailedEmptyPlanParentInspection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory read permissions are not enforced by Windows in the same way")
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	factoryRoot := filepath.Join(repoRoot, ".factory")
	if err := os.Mkdir(factoryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	outputFS, err := openConfinedFS(factoryRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outputFS.Close() }()
	defer func() { _ = os.Chmod(factoryRoot, 0o755) }()
	if err := os.Chmod(factoryRoot, 0o111); err != nil {
		t.Fatal(err)
	}

	report := executeTargetPlans(
		&Report{},
		outputKey{target: hcc.TargetFactory},
		outputFS,
		factoryRoot,
		nil,
		nil,
		nil,
		nil,
		false,
		config.MaterializationModeSymlink,
		repoRoot,
	)

	inspections := 0
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == "materialize.write.error" && strings.HasPrefix(diagnostic.Message, "inspect output directory ") {
			inspections++
		}
	}
	want := len(harness.TargetInfoMap[hcc.TargetFactory].Components)
	if inspections != want {
		t.Fatalf("parent inspection diagnostics = %d, want %d; diagnostics: %+v", inspections, want, report.Diags)
	}
}
