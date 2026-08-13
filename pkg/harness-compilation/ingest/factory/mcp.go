package factory

import (
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadMcpServers reads a Factory mcp.json file.
func LoadMcpServers(src fs.FS, path string) ([]*ir.McpServer, error) {
	return bundle.LoadMcpServers(src, path)
}
