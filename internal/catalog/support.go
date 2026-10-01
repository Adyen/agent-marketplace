package catalog

import (
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/harness"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func HookSupportFiles(sourcePath string) ([]string, error) {
	if sourcePath == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Name() != "hooks.json" && !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// HookSupportExclusions excludes native definitions, not executable assets.
// Keep HookSupportFiles unchanged: historical acquisition locks hash its superset.
func HookSupportExclusions(sourcePath, sourceHarness string) map[string]bool {
	primary := filepath.Base(sourcePath)
	excluded := map[string]bool{primary: true}
	info, ok := harness.TargetInfoFor(hcc.Target(sourceHarness))
	if !ok {
		return excluded
	}
	files := append(append([]string(nil), info.AggregateFiles.Project...), info.AggregateFiles.User...)
	for _, source := range files {
		if filepath.Base(source) != primary {
			continue
		}
		for _, sibling := range files {
			if filepath.Dir(source) == filepath.Dir(sibling) {
				excluded[filepath.Base(sibling)] = true
			}
		}
	}
	return excluded
}

// HookMaterializationSupportFiles is shared by publication and installation.
func HookMaterializationSupportFiles(sourcePath, sourceHarness string) ([]string, error) {
	names, err := HookSupportFiles(sourcePath)
	if err != nil {
		return nil, err
	}
	excluded := HookSupportExclusions(sourcePath, sourceHarness)
	result := names[:0]
	for _, name := range names {
		if excluded[name] || (sourceHarness == string(hcc.TargetWindsurf) && filepath.Base(filepath.Dir(sourcePath)) == "hooks" && filepath.Ext(name) == ".json") {
			continue
		}
		result = append(result, name)
	}
	return result, nil
}
