//go:build e2e

package localapi

import (
	"testing"
)

func TestE2EReleaseOverrideIsLimitedToItsLoopbackOrigin(t *testing.T) {
	const endpoint = "http://127.0.0.1:48123/repos/Adyen/agent-marketplace/releases/latest"
	t.Setenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT", endpoint)
	if got := releaseEndpoint(); got != endpoint {
		t.Fatalf("release endpoint = %q, want %q", got, endpoint)
	}
	if !allowedReleasePageURL("http://127.0.0.1:48123/Adyen/agent-marketplace/releases/tag/v1.2.3") {
		t.Fatal("same-origin loopback release page was rejected")
	}
	if !allowedReleaseAssetURL(
		"http://127.0.0.1:48123/Adyen/agent-marketplace/releases/download/v1.2.3/agent-marketplace-gui-v1.2.3-macos-universal.zip",
		"v1.2.3",
		"agent-marketplace-gui-v1.2.3-macos-universal.zip",
	) {
		t.Fatal("same-origin loopback release asset was rejected")
	}
	if allowedReleasePageURL("http://example.invalid/Adyen/agent-marketplace/releases/tag/v1.2.3") {
		t.Fatal("non-loopback release page was accepted")
	}
}
