package gemini

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestNativeSettingsPrefersNativeMCPAndRetainsLegacyInputs(t *testing.T) {
	for _, legacy := range []string{"gemini-extension.json", ".gemini/gemini-extension.json", ".gemini/mcp.json"} {
		t.Run(legacy, func(t *testing.T) {
			src := fstest.MapFS{legacy: &fstest.MapFile{Data: []byte(`{"name":"legacy","version":"1.0.0","mcpServers":{"legacy":{"command":"legacy"}}}`)}}
			bundle, err := LoadBundle(src)
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "legacy" {
				t.Fatalf("legacy resources: %v", bundle.Resources)
			}
			src[".gemini/settings.json"] = &fstest.MapFile{Data: []byte(`{"mcpServers":{"native":{"command":"native"}}}`)}
			bundle, err = LoadBundle(src)
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "native" {
				t.Fatalf("native resources: %v", bundle.Resources)
			}
		})
	}
}

func TestNativeSettingsMCPAndHooks(t *testing.T) {
	src := fstest.MapFS{".gemini/settings.json": &fstest.MapFile{Data: []byte(`{"mcpServers":{"first":{"command":"first"},"second":{"command":"second"}},"hooks":{"AfterTool":[{"matcher":".*","hooks":[{"type":"command","command":"echo audit"}]}]}}`)}}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[ir.ResourceKind]int{}
	for _, resource := range bundle.Resources {
		counts[resource.Kind()]++
	}
	if counts[ir.KindMcpServer] != 2 || counts[ir.KindHook] != 1 {
		t.Fatalf("resource kinds: %v", counts)
	}
}
