package fsboundary

import (
	"os"
	"path/filepath"
)

// ResolveExistingPrefix resolves symlinks in the longest existing ancestor of
// abs and re-appends the remaining components.
func ResolveExistingPrefix(abs string) (string, error) {
	remainder := []string{}
	current := abs
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(remainder) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, remainder[i])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(abs), nil
		}
		remainder = append(remainder, filepath.Base(current))
		current = parent
	}
}
