package harness

import (
	"log/slog"
	"os"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/testutil"
)

func TestMain(m *testing.M) {
	_, cleanup, err := testutil.IsolatedHome()
	if err != nil {
		slog.Error("initialize isolated test home", "err", err)
		os.Exit(1)
	}

	// Recompute the package-level root tables now that HOME points to a temp
	// directory, so any test reading InstallRoots/TargetInfoMap sees isolated
	// paths instead of the developer's real home.
	InstallRoots = MakeInstallRoots()
	TargetInfoMap, err = MakeTargetInfo()
	if err != nil {
		slog.Error("make target info", "err", err)
		if cleanupErr := cleanup(); cleanupErr != nil {
			slog.Error("cleanup isolated test home", "err", cleanupErr)
		}
		os.Exit(1)
	}

	code := m.Run()
	if cleanupErr := cleanup(); cleanupErr != nil {
		slog.Error("cleanup isolated test home", "err", cleanupErr)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
