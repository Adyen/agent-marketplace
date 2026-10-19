package factory

import (
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadMcpServers reads a Factory mcp.json file and normalizes the
// Factory-native plugin-root token into the canonical IR spelling across
// command, args, env, URL, and headers, matching hook ingestion.
func LoadMcpServers(src fs.FS, path string) ([]*ir.McpServer, error) {
	servers, err := bundle.LoadMcpServers(src, path)
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		bundle.NormalizeMcpServerRootToken(server, droidPluginRootToken)
	}
	return servers, nil
}
