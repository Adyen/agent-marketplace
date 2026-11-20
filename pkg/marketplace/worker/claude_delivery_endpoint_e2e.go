//go:build e2e

package worker

import "os"

func claudePluginsEndpoint() string {
	return os.Getenv("AGENT_MARKETPLACE_E2E_CLAUDE_PLUGINS_ENDPOINT")
}
