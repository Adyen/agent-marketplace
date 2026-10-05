//go:build !e2e

package ownership

import "testing"

func TestProductionOwnershipDoesNotInstallPause(t *testing.T) {
	t.Setenv("AGENT_MARKETPLACE_E2E_OWNERSHIP_PAUSE", t.TempDir())
	store := newFixture(t).store()
	if defaultCrasher != nil || store.crasher != nil {
		t.Fatal("production ownership installed an E2E pause hook")
	}
}
