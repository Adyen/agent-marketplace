//go:build e2e

package guiupdate

import "testing"

func TestE2EReleaseURLsAreLimitedToTheConfiguredLoopbackOrigin(t *testing.T) {
	t.Setenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT", "http://127.0.0.1:48123/repos/Adyen/agent-marketplace/releases/latest")
	if !validGitHubURL("http://127.0.0.1:48123/Adyen/agent-marketplace/releases/tag/v1.2.3") {
		t.Fatal("same-origin loopback release page was rejected")
	}
	if !validReleaseAssetURL(
		"http://127.0.0.1:48123/Adyen/agent-marketplace/releases/download/v1.2.3/agent-marketplace-gui-v1.2.3-macos-universal.zip",
		"v1.2.3",
		"agent-marketplace-gui-v1.2.3-macos-universal.zip",
	) {
		t.Fatal("same-origin loopback asset was rejected")
	}
	if validGitHubURL("http://example.invalid/Adyen/agent-marketplace/releases/tag/v1.2.3") {
		t.Fatal("non-loopback release page was accepted")
	}
}
