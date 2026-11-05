//go:build !e2e

package pluginpolicy

import "io/fs"

func runtimeCatalogSource() (fs.FS, []string, error) {
	return catalogFiles, []string{ClaudeNativePluginProbeID}, nil
}
