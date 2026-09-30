package testutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// IsolatedHome creates a temporary directory, sets HOME to it, and removes it
// on cleanup so tests never read or write the developer's real home.
// Callers receive the temporary home path and a cleanup function that removes
// it. The cleanup function is safe to call multiple times.
func IsolatedHome() (home string, cleanup func() error, err error) {
	home, err = os.MkdirTemp("", "agent-marketplace-test-home-")
	if err != nil {
		return "", nil, fmt.Errorf("create isolated test home: %w", err)
	}

	cleanup = func() error {
		if home == "" {
			return nil
		}
		// Tests that shell out to the Go toolchain (cross-compile build
		// checks) populate a fresh module cache under the isolated HOME, and
		// module-cache directories are read-only. Restore write permission
		// only on directories so os.RemoveAll can traverse them; removing a
		// file needs only its parent directory to be writable, and chmod on
		// a symlink would modify its target outside the isolated home.
		_ = filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
		if err := os.RemoveAll(home); err != nil {
			return fmt.Errorf("remove isolated test home: %w", err)
		}
		return nil
	}

	if err := os.Setenv("HOME", home); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return "", nil, errors.Join(
				fmt.Errorf("set HOME: %w", err),
				fmt.Errorf("cleanup isolated test home: %w", cleanupErr),
			)
		}
		return "", nil, fmt.Errorf("set HOME: %w", err)
	}

	return home, cleanup, nil
}
