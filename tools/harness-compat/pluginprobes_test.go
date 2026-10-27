package main

import (
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
)

// spec: plugin-policy-catalogs/verified-native-delivery-without-a-probe-fails-the-check
// spec: plugin-policy-catalogs/every-harness-target-and-package-format-ships-a-plugin-catalog
func TestPluginCatalogsValidateAgainstNativeProbes(t *testing.T) {
	if !slices.Contains(nativePluginProbes, "claude-native-plugin") {
		t.Fatal("Claude native plugin probe is not registered")
	}
	if err := pluginpolicy.ValidateCatalogs(nativePluginProbes); err != nil {
		t.Fatal(err)
	}
}
