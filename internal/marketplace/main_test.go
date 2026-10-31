package marketplace

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

	code := m.Run()
	if cleanupErr := cleanup(); cleanupErr != nil {
		slog.Error("cleanup isolated test home", "err", cleanupErr)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
