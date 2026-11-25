//go:build e2e

package localapi

import (
	"os"

	"github.com/Adyen/agent-marketplace/internal/guiupdate"
)

func releaseEndpoint() string {
	if guiupdate.E2EReleaseEndpoint() != nil {
		return os.Getenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT")
	}
	return githubReleaseURL
}
