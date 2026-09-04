package pipeline

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/testutil"
)

func TestMain(m *testing.M) {
	home, cleanup, err := testutil.IsolatedHome()
	if err != nil {
		slog.Error("initialize isolated test home", "err", err)
		os.Exit(1)
	}
	config.SetTestV3OrganizationConfigPath(nil, filepath.Join(home, "missing-org-config.yaml"))

	code := m.Run()
	if cleanupErr := cleanup(); cleanupErr != nil {
		slog.Error("cleanup isolated test home", "err", cleanupErr)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
