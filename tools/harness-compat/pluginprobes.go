package main

// nativePluginProbes is the set of native plugin probe identifiers the
// harness validation suite defines. A plugin harness catalog may declare
// native delivery verified only with a probe identifier present here
// (pluginpolicy design D3). None are defined yet: every shipped catalog
// stays unverified.
var nativePluginProbes []string
