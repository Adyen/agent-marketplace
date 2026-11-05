package factory

import (
	"io/fs"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	droidPluginRootToken  = "${DROID_PLUGIN_ROOT}"
	claudePluginRootToken = "${CLAUDE_PLUGIN_ROOT}"
)

// LoadMcpServers reads a Factory mcp.json file and canonicalizes the Factory
// plugin-root token in every value that may carry one.
func LoadMcpServers(src fs.FS, path string) ([]*ir.McpServer, error) {
	servers, err := bundle.LoadMcpServers(src, path)
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		server.Command = canonicalPluginRoot(server.Command)
		server.URL = canonicalPluginRoot(server.URL)
		for i, arg := range server.Args {
			server.Args[i] = canonicalPluginRoot(arg)
		}
		for _, values := range []map[string]string{server.Env, server.Headers} {
			for key, value := range values {
				values[key] = canonicalPluginRoot(value)
			}
		}
	}
	return servers, nil
}

func canonicalPluginRoot(value string) string {
	return strings.ReplaceAll(value, droidPluginRootToken, claudePluginRootToken)
}
