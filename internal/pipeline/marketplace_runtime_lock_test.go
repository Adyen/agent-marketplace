package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestTransientMarketplaceReadAssertionsRequireLockAbsence(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "lock.yaml")
	if err := os.WriteFile(lockPath, []byte("deployed"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := transientMarketplaceReadAssertions(config.EffectiveConfig{}, lockPath)
	if err == nil || !strings.Contains(err.Error(), "must remain absent") {
		t.Fatalf("error = %v, want deployed-lock absence failure", err)
	}
}
