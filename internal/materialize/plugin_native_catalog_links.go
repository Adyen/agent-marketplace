package materialize

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/packagegen"
)

func publishClaudePluginCatalogLinks(catalog nativePluginCatalog) (retErr error) {
	stateRoot, err := packagegen.StateRoot()
	if err != nil {
		return err
	}
	plugins, pluginsPath, err := openClaudePluginCatalogLinks(catalog.path, stateRoot, true)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, plugins.Close()) }()
	packagesRoot := filepath.Join(stateRoot, "packages")
	for _, entry := range catalog.entries {
		if entry.name == "" || filepath.Base(entry.name) != entry.name || strings.ContainsAny(entry.name, `/\`) {
			return fmt.Errorf("invalid Claude plugin catalog entry name %q", entry.name)
		}
		target, err := filepath.Abs(entry.generationPath)
		if err != nil || !catalogGenerationPath(target, packagesRoot) {
			return fmt.Errorf("claude plugin generation is outside the packages root")
		}
		info, err := os.Lstat(target)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("claude plugin generation is not a directory")
		}
		current, err := plugins.Lstat(entry.name)
		if err == nil {
			if current.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("claude plugin catalog path is not a symbolic link")
			}
			link, linkErr := plugins.Readlink(entry.name)
			if linkErr != nil || !catalogGenerationPath(resolveCatalogLink(pluginsPath, link), packagesRoot) {
				return fmt.Errorf("claude plugin catalog link is outside the packages root")
			}
			if filepath.Clean(resolveCatalogLink(pluginsPath, link)) == target {
				continue
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := replaceClaudePluginCatalogLink(plugins, entry.name, target); err != nil {
			return err
		}
	}
	return syncClaudePluginCatalogLinks(plugins)
}

func removeStaleClaudePluginCatalogLinks(catalog nativePluginCatalog) (retErr error) {
	stateRoot, err := packagegen.StateRoot()
	if err != nil {
		return err
	}
	plugins, pluginsPath, err := openClaudePluginCatalogLinks(catalog.path, stateRoot, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, plugins.Close()) }()
	wanted := make(map[string]bool, len(catalog.entries))
	for _, entry := range catalog.entries {
		wanted[entry.name] = true
	}
	packagesRoot := filepath.Join(stateRoot, "packages")
	dir, err := plugins.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	changed := false
	for _, entry := range entries {
		if wanted[entry.Name()] {
			continue
		}
		info, err := plugins.Lstat(entry.Name())
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		link, err := plugins.Readlink(entry.Name())
		if err != nil {
			return err
		}
		if !catalogGenerationPath(resolveCatalogLink(pluginsPath, link), packagesRoot) {
			continue
		}
		if err := plugins.Remove(entry.Name()); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		return syncClaudePluginCatalogLinks(plugins)
	}
	return nil
}

func openClaudePluginCatalogLinks(catalogPath, stateRoot string, create bool) (native *os.Root, pluginsPath string, retErr error) {
	root, err := fsboundary.OpenDir(stateRoot)
	if err != nil {
		return nil, "", err
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	catalogRel, err := filepath.Rel(root.Path(), catalogPath)
	if err != nil || catalogRel == "." || catalogRel == ".." || strings.HasPrefix(catalogRel, ".."+string(os.PathSeparator)) || filepath.IsAbs(catalogRel) {
		return nil, "", fmt.Errorf("%w: Claude plugin catalog escapes the state root", fsboundary.ErrUnsafePath)
	}
	rel := filepath.ToSlash(filepath.Join(catalogRel, "plugins"))
	var plugins *fsboundary.Dir
	if create {
		plugins, err = root.EnsureRoot(rel)
	} else {
		plugins, err = root.OpenRoot(rel)
	}
	if err != nil {
		return nil, "", err
	}
	defer func() { retErr = errors.Join(retErr, plugins.Close()) }()
	native, err = plugins.NativeRoot()
	if err != nil {
		return nil, "", err
	}
	return native, plugins.Path(), nil
}

func replaceClaudePluginCatalogLink(root *os.Root, name, target string) error {
	for attempts := 0; attempts < 8; attempts++ {
		var token [12]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		temp := ".am-" + hex.EncodeToString(token[:]) + ".tmp"
		if err := root.Symlink(target, temp); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return err
		}
		if err := root.Rename(temp, name); err != nil {
			_ = root.Remove(temp)
			return err
		}
		return nil
	}
	return fmt.Errorf("could not allocate a temporary Claude plugin catalog link")
}

func catalogGenerationPath(candidate, packagesRoot string) bool {
	rel, err := filepath.Rel(packagesRoot, candidate)
	return err == nil && rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func resolveCatalogLink(pluginsPath, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(pluginsPath, target))
}

func syncClaudePluginCatalogLinks(root *os.Root) (retErr error) {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, dir.Close()) }()
	return dir.Sync()
}
