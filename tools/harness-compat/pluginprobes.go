package main

import "github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"

// nativePluginProbes is the set of native plugin probe identifiers the
// harness validation suite defines. A plugin harness catalog may declare
// native delivery verified only with a probe identifier present here
// (pluginpolicy design D3).
var nativePluginProbes = []string{pluginpolicy.ClaudeNativePluginProbeID}
