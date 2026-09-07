package pipeline

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/testutil"
)

// Isolation of the organization layer is not arranged here. An unset override
// resolves to a nonexistent path under `go test` (see organizationConfigPath),
// so no package has to remember to ask.
func TestMain(m *testing.M) {
	home, cleanup, err := testutil.IsolatedHome()
	if err != nil {
		slog.Error("initialize isolated test home", "err", err)
		os.Exit(1)
	}
	cwd, err := os.Getwd()
	if err != nil {
		slog.Error("resolve pipeline test working directory", "err", err)
		os.Exit(1)
	}
	if err := os.Chdir(home); err != nil {
		slog.Error("isolate pipeline test working directory", "err", err)
		os.Exit(1)
	}

	code := m.Run()
	if err := os.Chdir(cwd); err != nil {
		slog.Error("restore pipeline test working directory", "err", err)
		code = 1
	}
	if cleanupErr := cleanup(); cleanupErr != nil {
		slog.Error("cleanup isolated test home", "err", cleanupErr)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
