package ownership

import (
	"log/slog"
	"os"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/testutil"
)

func TestMain(m *testing.M) {
	// Every test in this package resolves an XDG state root, so HOME must be
	// shadowed before anything runs: a leaked default would write ownership state
	// into the developer's real ~/.local/state.
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
