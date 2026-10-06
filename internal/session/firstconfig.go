package session

import (
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
)

// NeedsFirstConfiguration reports whether the home directory exists but the
// user personal config file does not, so an interactive client should offer
// first-run target selection. The home directory itself must be present so
// test sandboxes with synthetic paths don't accidentally trigger the
// first-run flow.
func NeedsFirstConfiguration(homeDir string) bool {
	if homeDir == "" {
		return false
	}
	if info, err := os.Stat(homeDir); err != nil || !info.IsDir() {
		return false
	}
	_, err := os.Stat(filepath.Join(homeDir, ".agents", "config.yaml"))
	return os.IsNotExist(err)
}

// DetectHarnessTargets detects installed harness roots under homeDir for
// first-configuration pre-selection.
func DetectHarnessTargets(homeDir string) ([]string, error) {
	return harness.DetectInstalled(homeDir)
}

// InitializeUserTargets writes the user-personal root preconfigured with the
// selected harness targets and returns its path. Callers decide how to treat
// config.ErrAlreadyInitialized.
func InitializeUserTargets(harnesses []string) (string, error) {
	return config.InitializeUserPersonal(harnesses)
}
