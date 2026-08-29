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

	// An isolated HOME is not enough. The organization layer lives at an
	// absolute path outside HOME, so without this override these tests read
	// the developer's real /etc/agent-marketplace/config.yaml and fetch the
	// registries it declares over the network. Resolution reaches this layer
	// even when a test passes an empty config, because it loads the effective
	// config from disk itself. Individual tests that need an org config still
	// set the override explicitly with config.SetTestOrganizationConfigPath.
	orgDir, err := os.MkdirTemp("", "agent-marketplace-test-org-config-*")
	if err != nil {
		slog.Error("initialize isolated org config directory", "err", err)
		if cleanupErr := cleanup(); cleanupErr != nil {
			slog.Error("cleanup isolated test home", "err", cleanupErr)
		}
		os.Exit(1)
	}
	config.SetTestOrganizationConfigPath(nil, filepath.Join(orgDir, "nonexistent-org-config-for-tests.yaml"))

	code := m.Run()
	_ = os.RemoveAll(orgDir)
	if cleanupErr := cleanup(); cleanupErr != nil {
		slog.Error("cleanup isolated test home", "err", cleanupErr)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
