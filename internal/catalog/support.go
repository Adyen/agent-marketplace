package catalog

import (
	"os"
	"path/filepath"
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
