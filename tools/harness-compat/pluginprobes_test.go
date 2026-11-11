package main

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
)

// spec: plugin-policy-catalogs/verified-native-delivery-without-a-probe-fails-the-check
// spec: plugin-policy-catalogs/every-harness-target-and-package-format-ships-a-plugin-catalog
func TestPluginCatalogsValidateAgainstNativeProbes(t *testing.T) {
	if err := pluginpolicy.ValidateCatalogs(nativePluginProbes); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeNativePluginProbeIsRegistered(t *testing.T) {
	if pluginpolicy.ClaudeNativePluginProbeID != "claude-native-plugin" {
		t.Fatalf("Claude native plugin probe ID = %q, want claude-native-plugin", pluginpolicy.ClaudeNativePluginProbeID)
	}
	for _, probe := range nativePluginProbes {
		if probe == pluginpolicy.ClaudeNativePluginProbeID {
			return
		}
	}
	t.Fatalf("native plugin probes %v do not include %q", nativePluginProbes, pluginpolicy.ClaudeNativePluginProbeID)
}
