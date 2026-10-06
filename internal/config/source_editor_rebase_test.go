package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: tui-source-toggle/new-whole-source-configuration-roots-use-v4
func TestWholeSourceEditorsCreateV4Roots(t *testing.T) {
	for _, exclusion := range []bool{false, true} {
		t.Run(map[bool]string{false: "selection", true: "exclusion"}[exclusion], func(t *testing.T) {
			root := canonicalDirectoryTestRoot(t)
			path := filepath.Join(root, ".agents", "config.local.yaml")
			entry := RegistryEntry{URL: "https://example.test/tools.git", Ref: "main"}
			var changed bool
			var err error
			if exclusion {
				changed, err = EditSourceExclusion(path, path, entry, SourceExcludeAll)
			} else {
				changed, err = EditSourceSelection(path, path, entry, true)
			}
			if err != nil || !changed {
				t.Fatalf("new source edit: changed=%v err=%v", changed, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if PeekAPIVersion(data) != APIVersionV4 {
				t.Fatalf("new root is not v4:\n%s", data)
			}
		})
	}
}

// spec: tui-source-toggle/a-sibling-ref-stays-unchanged
func TestWholeSourceLegacyEditsKeepOtherRefs(t *testing.T) {
	for _, exclusion := range []bool{false, true} {
		t.Run(map[bool]string{false: "selection", true: "exclusion"}[exclusion], func(t *testing.T) {
			root := canonicalDirectoryTestRoot(t)
			path := LocalConfigPath(root)
			const url = "https://example.test/tools.git"
			writeSourceExclusionFixture(t, path, "registries:\n"+
				"  - url: "+url+"\n    ref: main\n    default: true\n"+
				"  - url: "+url+"\n    ref: dev\n    skills: {include: [keep]}\n")
			entry := RegistryEntry{URL: url, Ref: "main"}
			var changed bool
			var err error
			if exclusion {
				changed, err = EditSourceExclusion(path, path, entry, SourceExcludeAll)
			} else {
				changed, err = EditSourceSelection(path, path, entry, false)
			}
			if err != nil || !changed {
				t.Fatalf("edit: changed=%v err=%v", changed, err)
			}
			local, err := LoadLocalConfigFromPath(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(local.Registries) != 2 || local.Registries[1].Ref != "dev" ||
				len(local.Registries[1].Skills.Include) != 1 || local.Registries[1].Skills.Include[0] != "keep" ||
				local.Registries[1].ExcludeAll {
				t.Fatalf("sibling ref changed: %+v", local.Registries)
			}
			if !strings.Contains(readSourceExclusionFixture(t, path), "ref: main") {
				t.Fatal("target identity disappeared")
			}
		})
	}
}
