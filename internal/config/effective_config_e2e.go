//go:build e2e

package config

import "os"

func init() {
	if env := os.Getenv("AGENT_MARKETPLACE_V3_ORG_CONFIG"); env != "" {
		testV3OrganizationConfigPath = env
	}
}
