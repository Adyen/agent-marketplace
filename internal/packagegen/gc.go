package packagegen

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

var generationDirectoryName = regexp.MustCompile(`^[0-9a-f]{64}$`)

func GarbageCollect(dryRun bool) ([]string, error) {
	stateRoot, err := StateRoot()
	if err != nil {
		return nil, err
	}
	packagesDir := filepath.Join(stateRoot, "packages")
	if _, err := os.Lstat(packagesDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var removed []string
	collect := func() error {
		if err := requireDirectory(packagesDir); err != nil {
			return err
		}
		stages, err := cleanupPackageStages(packagesDir, dryRun)
		if err != nil {
			return err
		}
		removed = append(removed, stages...)
		refs, err := ownership.ReadAllPluginGenerationRefs()
		if err != nil {
			return err
		}
		pending, err := ownership.ReadPendingPluginGenerationRefs()
		if err != nil {
			return err
		}
		live := make(map[string]bool, len(refs)+len(pending))
		for _, ref := range append(refs, pending...) {
			live[ref.GenerationKey] = true
		}
		entries, err := os.ReadDir(packagesDir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !generationDirectoryName.MatchString(entry.Name()) || live[entry.Name()] {
				continue
			}
			path := filepath.Join(packagesDir, entry.Name())
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return os.ErrInvalid
			}
			removed = append(removed, path)
			if !dryRun {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if dryRun {
		err = collect()
	} else {
		err = dependencytxn.WithPathLock(context.Background(), packagesDir, collect)
	}
	if err != nil {
		return nil, err
	}
	if len(removed) > 0 {
		slog.Info("plugin package generation collection completed", "dry_run", dryRun, "removed", len(removed))
	}
	return removed, nil
}

func cleanupPackageStages(packagesDir string, dryRun bool) ([]string, error) {
	entries, err := os.ReadDir(packagesDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stages []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".packagegen-") {
			continue
		}
		path := filepath.Join(packagesDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, os.ErrInvalid
		}
		stages = append(stages, path)
		if !dryRun {
			if err := os.RemoveAll(path); err != nil {
				return nil, err
			}
		}
	}
	return stages, nil
}
