//go:build e2e

package pluginpolicy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeCatalogSourceAcceptsFactoryNativeProbe(t *testing.T) {
	override := `schema: 1
harness: factory
evidence:
  checked: "2026-10-28"
  refs: [factory-native-e2e]
accepts: [factory-plugin, claude-plugin]
delivery:
  decompose: {status: verified}
  native: {status: verified, format: claude-plugin, probe: factory-native-plugin, refresh: restart-required}
`
	path := filepath.Join(t.TempDir(), "factory.yaml")
	if err := os.WriteFile(path, []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(e2ePluginCatalogEnv, path)
	files, probes, err := runtimeCatalogSource()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadCatalogs(files, "formats/*.yaml", "catalogs/*.yaml", probes); err != nil {
		t.Fatalf("e2e Factory catalog override rejected its CI probe: %v", err)
	}
}
