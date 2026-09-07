package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		injected string
		info     *debug.BuildInfo
		want     string
	}{
		{"release overrides pseudo-version", "3.0.4", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260903094142-e069a1ba1077"}}, "3.0.4"},
		{"tag prefix preserved", "v3.0.4", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "v3.0.4"},
		{"prerelease preserved", "v3.1.0-rc.1", nil, "v3.1.0-rc.1"},
		{"local stamp preserved", "v3.0.4-2-gabcdef-dirty", nil, "v3.0.4-2-gabcdef-dirty"},
		{"module install", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, "v1.2.3"},
		{"empty stamp", "", &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, "v1.2.3"},
		{"pseudo-version fallback", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260903094142-e069a1ba1077"}}, "v0.0.0-20260903094142-e069a1ba1077"},
		{"development build", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "dev"},
		{"empty metadata", "", &debug.BuildInfo{}, "dev"},
		{"missing metadata", "dev", nil, "dev"},
		{"dependency is not the CLI", "dev", &debug.BuildInfo{Deps: []*debug.Module{{Path: "github.com/Adyen/agent-marketplace", Version: "v1.2.3"}}}, "dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersion(tc.injected, tc.info); got != tc.want {
				t.Fatalf("resolveVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}
