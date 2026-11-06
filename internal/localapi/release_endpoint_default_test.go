//go:build !e2e

package localapi

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/guiupdate"
)

func TestProductionReleaseEndpointIgnoresE2EOverride(t *testing.T) {
	t.Setenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT", "http://127.0.0.1:48123/latest")
	if got := releaseEndpoint(); got != githubReleaseURL {
		t.Fatalf("production release endpoint = %q, want %q", got, githubReleaseURL)
	}
	if guiupdate.ValidReleasePageURL("http://127.0.0.1:48123/Adyen/agent-marketplace/releases/tag/v1.2.3") {
		t.Fatal("production accepted an E2E release page")
	}
}
