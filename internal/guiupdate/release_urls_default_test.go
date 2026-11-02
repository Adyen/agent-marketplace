//go:build !e2e

package guiupdate

import "testing"

func TestProductionUpdaterRejectsE2EReleaseEndpoint(t *testing.T) {
	t.Setenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT", "http://127.0.0.1:48123/latest")
	if validGitHubURL("http://127.0.0.1:48123/Adyen/agent-marketplace/releases/tag/v1.2.3") {
		t.Fatal("production updater accepted an E2E release URL")
	}
}
