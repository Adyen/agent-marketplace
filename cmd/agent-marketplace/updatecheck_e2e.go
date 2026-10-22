//go:build e2e

package main

import "os"

// e2e builds can point the release check at a local stub and report a
// release version, so TUI QA can prove the upgrade notice end to end. The
// make targets build e2e binaries with a git-describe version, which the
// release check never compares, and production builds have neither hook.
func init() {
	if endpoint := os.Getenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT"); endpoint != "" {
		upgradeNoticeEndpoint = endpoint
	}
	if v := os.Getenv("AGENT_MARKETPLACE_E2E_VERSION"); v != "" {
		version = v
	}
}
