//go:build e2e

package pluginpolicy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"
)

const e2ePluginCatalogEnv = "AGENT_MARKETPLACE_E2E_PLUGIN_CATALOG"

func runtimeCatalogSource() (fs.FS, []string, error) {
	overridePath := os.Getenv(e2ePluginCatalogEnv)
	if overridePath == "" {
		return catalogFiles, nil, nil
	}
	override, err := os.ReadFile(overridePath)
	if err != nil {
		return nil, nil, err
	}
	target := strings.TrimSuffix(filepath.Base(overridePath), filepath.Ext(overridePath))
	if target == "" {
		return nil, nil, errors.New("invalid e2e plugin catalog override path")
	}
	files := fstest.MapFS{}
	if err := fs.WalkDir(catalogFiles, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, err := fs.ReadFile(catalogFiles, name)
		if err != nil {
			return err
		}
		files[name] = &fstest.MapFile{Data: data}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	files[filepath.ToSlash(filepath.Join("catalogs", target+".yaml"))] = &fstest.MapFile{Data: override}
	return files, []string{"e2e-test-probe"}, nil
}
