package main

import "runtime/debug"

func init() {
	info, _ := debug.ReadBuildInfo()
	version = resolveVersion(version, info)
}

func resolveVersion(injected string, info *debug.BuildInfo) string {
	if injected != "" && injected != defaultVersion {
		return injected
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return defaultVersion
}
