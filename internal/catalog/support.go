package catalog

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

func HookSupportFiles(sourcePath string) ([]string, error) {
	if sourcePath == "" {
		return nil, nil
	}
	return HookSupportFilesFS(os.DirFS(filepath.Dir(sourcePath)), filepath.Base(sourcePath))
}

func HookSupportFilesFS(source fs.FS, sourcePath string) ([]string, error) {
	if sourcePath == "" {
		return nil, nil
	}
	entries, err := fs.ReadDir(source, path.Dir(sourcePath))
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
